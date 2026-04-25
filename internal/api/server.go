package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/validator/v10"

	"github.com/samuelstrom93/pingit/internal/auth"
	"github.com/samuelstrom93/pingit/internal/config"
	"github.com/samuelstrom93/pingit/internal/db"
	"github.com/samuelstrom93/pingit/internal/domain"
	"github.com/samuelstrom93/pingit/internal/email"
	"github.com/samuelstrom93/pingit/internal/ws"
)

type Server struct {
	cfg      config.Config
	db       *sql.DB
	logger   *slog.Logger
	validate *validator.Validate
	email    email.Sender
	hub      *ws.Hub
}

type contextKey string

const userContextKey contextKey = "user"

func NewServer(cfg config.Config, conn *sql.DB, logger *slog.Logger, sender email.Sender, hub *ws.Hub) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	return &Server{
		cfg:      cfg,
		db:       conn,
		logger:   logger,
		validate: validator.New(validator.WithRequiredStructEnabled()),
		email:    sender,
		hub:      hub,
	}
}

func (s *Server) Router(spa http.Handler) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(s.logMiddleware)
	r.Use(s.corsMiddleware)

	r.Route("/api", func(api chi.Router) {
		api.Post("/auth/magic-link", s.handleMagicLink)
		api.Get("/auth/callback", s.handleAuthCallback)

		api.Group(func(priv chi.Router) {
			priv.Use(s.authRequired)
			priv.Post("/auth/logout", s.handleLogout)
			priv.Get("/me", s.handleMe)

			priv.Route("/spaces", func(spaces chi.Router) {
				spaces.Post("/", s.handleCreateSpace)
				spaces.Get("/", s.handleListSpaces)
				spaces.Post("/join/{code}", s.handleJoinByCode)
				spaces.Get("/{spaceID}", s.handleGetSpace)
				spaces.Patch("/{spaceID}", s.handleUpdateSpace)
				spaces.Delete("/{spaceID}", s.handleDeleteSpace)
				spaces.Get("/{spaceID}/members", s.handleListMembers)
				spaces.Delete("/{spaceID}/members/{userID}", s.handleDeleteMember)
				spaces.Post("/{spaceID}/invitations", s.handleCreateInvitation)
				spaces.Get("/{spaceID}/players", s.handleListPlayers)
				spaces.Post("/{spaceID}/players", s.handleCreatePlayer)
				spaces.Post("/{spaceID}/matches", s.handleCreateMatch)
				spaces.Get("/{spaceID}/matches", s.handleListMatches)
				spaces.Post("/{spaceID}/tournaments", s.handleCreateTournament)
				spaces.Get("/{spaceID}/tournaments", s.handleListTournaments)
				spaces.Get("/{spaceID}/stats/players/{playerID}", s.handlePlayerStats)
				spaces.Get("/{spaceID}/stats/h2h", s.handleHeadToHead)
			})

			priv.Post("/invitations/{token}/accept", s.handleAcceptInvitation)
			priv.Patch("/players/{playerID}", s.handleUpdatePlayer)
			priv.Delete("/players/{playerID}", s.handleDeletePlayer)
			priv.Post("/players/{playerID}/claim", s.handleClaimPlayer)
			priv.Get("/matches/{matchID}", s.handleGetMatch)
			priv.Post("/matches/{matchID}/score", s.handleScoreMatch)
			priv.Post("/matches/{matchID}/undo", s.handleUndoMatch)
			priv.Patch("/matches/{matchID}", s.handleEditMatch)
			priv.Delete("/matches/{matchID}", s.handleDeleteMatch)
			priv.Get("/ws/matches/{matchID}", s.handleMatchWebSocket)
			priv.Post("/tournaments/{tournamentID}/start", s.handleStartTournament)
			priv.Get("/tournaments/{tournamentID}", s.handleGetTournament)
			priv.Get("/tournaments/{tournamentID}/standings", s.handleTournamentStandings)
			priv.Delete("/tournaments/{tournamentID}", s.handleDeleteTournament)
			priv.Get("/admin/spaces", s.handleAdminSpaces)
			priv.Post("/admin/wipe", s.handleAdminWipe)
		})
	})

	r.Handle("/*", spa)
	return r
}

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start))
	})
}

func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", s.cfg.BaseURL)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) authRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.currentUser(r)
		if err != nil {
			s.writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) currentUser(r *http.Request) (db.User, error) {
	ck, err := r.Cookie(auth.SessionCookieName)
	if err != nil || ck.Value == "" {
		return db.User{}, errors.New("missing session cookie")
	}

	row := s.db.QueryRowContext(r.Context(), `
SELECT u.id, u.email, u.display_name, u.avatar_url, u.is_super_admin, u.created_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.id = ? AND s.expires_at > ?`, ck.Value, nowMS())

	user, err := scanUser(row)
	if err != nil {
		return db.User{}, err
	}
	return user, nil
}

func userFromContext(ctx context.Context) db.User {
	return ctx.Value(userContextKey).(db.User)
}

type magicLinkRequest struct {
	Email string `json:"email" validate:"required,email"`
}

func (s *Server) handleMagicLink(w http.ResponseWriter, r *http.Request) {
	var req magicLinkRequest
	if !s.decodeAndValidate(w, r, &req) {
		return
	}

	token, err := auth.NewMagicToken()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	now := nowMS()
	_, err = s.db.ExecContext(r.Context(), `INSERT INTO magic_link_tokens (token, email, expires_at, created_at) VALUES (?, ?, ?, ?)`,
		token, strings.ToLower(req.Email), now+int64(auth.MagicLinkTTL/time.Millisecond), now)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to persist token")
		return
	}

	link := fmt.Sprintf("%s/api/auth/callback?token=%s", strings.TrimSuffix(s.cfg.BaseURL, "/"), url.QueryEscape(token))
	if err := s.email.SendMagicLink(r.Context(), req.Email, link); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to send email")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		s.writeError(w, http.StatusBadRequest, "missing token")
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to open transaction")
		return
	}
	defer tx.Rollback()

	var email string
	var expiresAt int64
	var usedAt sql.NullInt64
	err = tx.QueryRowContext(r.Context(), `SELECT email, expires_at, used_at FROM magic_link_tokens WHERE token = ?`, token).Scan(&email, &expiresAt, &usedAt)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid token")
		return
	}
	if usedAt.Valid || expiresAt <= nowMS() {
		s.writeError(w, http.StatusBadRequest, "expired token")
		return
	}

	user, err := s.findOrCreateUserTx(r.Context(), tx, email)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}

	sessionID := auth.NewID()
	now := nowMS()
	if _, err = tx.ExecContext(r.Context(), `UPDATE magic_link_tokens SET used_at = ? WHERE token = ?`, now, token); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to update token")
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO sessions (id, user_id, expires_at, created_at, user_agent, ip) VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, user.ID, now+int64(auth.SessionTTL/time.Millisecond), now, r.UserAgent(), clientIP(r))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.cfg.IsProd(),
		MaxAge:   int((30 * 24 * time.Hour).Seconds()),
	})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	ck, err := r.Cookie(auth.SessionCookieName)
	if err == nil && ck.Value != "" {
		_, _ = s.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE id = ?`, ck.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: auth.SessionCookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.cfg.IsProd()})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, userFromContext(r.Context()))
}

type createSpaceRequest struct {
	Name        string `json:"name" validate:"required,min=2"`
	Description string `json:"description"`
}

func (s *Server) handleCreateSpace(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	var req createSpaceRequest
	if !s.decodeAndValidate(w, r, &req) {
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()

	spaceID := auth.NewID()
	now := nowMS()
	joinCode := randomJoinCode()
	_, err = tx.ExecContext(r.Context(), `
INSERT INTO spaces (id, name, description, is_invite_only, join_code, created_by, created_at)
VALUES (?, ?, ?, 1, ?, ?, ?)`,
		spaceID, req.Name, nullableString(req.Description), joinCode, user.ID, now)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create space")
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?, ?, 'admin', ?)`, spaceID, user.ID, now)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create membership")
		return
	}
	if err := s.ensurePlayerTx(r.Context(), tx, spaceID, user); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create player")
		return
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit space")
		return
	}
	row := s.db.QueryRowContext(r.Context(), `
SELECT id, name, description, is_invite_only, join_code, created_by, created_at, deleted_at
FROM spaces WHERE id = ? AND deleted_at IS NULL`, spaceID)
	space, err := scanSpace(row)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to reload space")
		return
	}
	s.writeJSON(w, http.StatusCreated, space)
}

func (s *Server) handleListSpaces(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	rows, err := s.db.QueryContext(r.Context(), `
SELECT s.id, s.name, s.description, s.is_invite_only, s.join_code, s.created_by, s.created_at, s.deleted_at
FROM spaces s
JOIN space_members sm ON sm.space_id = s.id
WHERE sm.user_id = ? AND s.deleted_at IS NULL
ORDER BY s.created_at DESC`, user.ID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list spaces")
		return
	}
	defer rows.Close()

	var spaces []db.Space
	for rows.Next() {
		space, err := scanSpace(rows)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to decode spaces")
			return
		}
		spaces = append(spaces, space)
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"spaces": spaces})
}

func (s *Server) handleGetSpace(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	row := s.db.QueryRowContext(r.Context(), `
SELECT id, name, description, is_invite_only, join_code, created_by, created_at, deleted_at
FROM spaces WHERE id = ? AND deleted_at IS NULL`, spaceID)
	space, err := scanUserlessSpace(row)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "space not found")
		return
	}
	s.writeJSON(w, http.StatusOK, space)
}

type updateSpaceRequest struct {
	IsInviteOnly   *bool `json:"is_invite_only"`
	RotateJoinCode bool  `json:"rotate_join_code"`
}

func (s *Server) handleUpdateSpace(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireAdmin(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req updateSpaceRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	parts := []string{}
	args := []any{}
	if req.IsInviteOnly != nil {
		parts = append(parts, "is_invite_only = ?")
		args = append(args, boolToInt(*req.IsInviteOnly))
	}
	if req.RotateJoinCode {
		parts = append(parts, "join_code = ?")
		args = append(args, randomJoinCode())
	}
	if len(parts) == 0 {
		s.writeError(w, http.StatusBadRequest, "no changes requested")
		return
	}
	args = append(args, spaceID)
	_, err := s.db.ExecContext(r.Context(), "UPDATE spaces SET "+strings.Join(parts, ", ")+" WHERE id = ?", args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to update space")
		return
	}
	s.handleGetSpace(w, r)
}

func (s *Server) handleDeleteSpace(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireAdmin(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	_, err := s.db.ExecContext(r.Context(), `UPDATE spaces SET deleted_at = ? WHERE id = ?`, nowMS(), spaceID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to delete space")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type invitationRequest struct {
	Email string `json:"email" validate:"required,email"`
}

func (s *Server) handleCreateInvitation(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireAdmin(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req invitationRequest
	if !s.decodeAndValidate(w, r, &req) {
		return
	}
	token, err := auth.NewMagicToken()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	id := auth.NewID()
	now := nowMS()
	_, err = s.db.ExecContext(r.Context(), `
INSERT INTO space_invitations (id, space_id, email, token, invited_by, status, created_at)
VALUES (?, ?, ?, ?, ?, 'pending', ?)`, id, spaceID, strings.ToLower(req.Email), token, user.ID, now)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create invitation")
		return
	}
	link := fmt.Sprintf("%s/api/invitations/%s/accept", strings.TrimSuffix(s.cfg.BaseURL, "/"), token)
	if err := s.email.SendInvitation(r.Context(), req.Email, link); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to send invitation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListMembers(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
SELECT u.id, u.email, u.display_name, u.avatar_url, u.is_super_admin, u.created_at, sm.role, sm.joined_at
FROM space_members sm
JOIN users u ON u.id = sm.user_id
WHERE sm.space_id = ?
ORDER BY sm.joined_at ASC`, spaceID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list members")
		return
	}
	defer rows.Close()
	type member struct {
		db.User
		Role     string `json:"role"`
		JoinedAt int64  `json:"joined_at"`
	}
	var members []member
	for rows.Next() {
		var user db.User
		var avatar sql.NullString
		var isSuperAdmin int
		var role string
		var joinedAt int64
		if err := rows.Scan(&user.ID, &user.Email, &user.DisplayName, &avatar, &isSuperAdmin, &user.CreatedAt, &role, &joinedAt); err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to decode members")
			return
		}
		if avatar.Valid {
			user.AvatarURL = &avatar.String
		}
		user.IsSuperAdmin = isSuperAdmin == 1
		members = append(members, member{User: user, Role: role, JoinedAt: joinedAt})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"members": members})
}

func (s *Server) handleDeleteMember(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	targetUserID := chi.URLParam(r, "userID")
	user := userFromContext(r.Context())
	if err := s.requireAdmin(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if user.ID == targetUserID {
		admins, err := s.countAdmins(r.Context(), spaceID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to inspect admins")
			return
		}
		if admins <= 1 {
			s.writeError(w, http.StatusBadRequest, "last admin cannot leave")
			return
		}
	}
	_, err := s.db.ExecContext(r.Context(), `DELETE FROM space_members WHERE space_id = ? AND user_id = ?`, spaceID, targetUserID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to remove member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleJoinByCode(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	user := userFromContext(r.Context())
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()

	var spaceID string
	var inviteOnly int
	if err := tx.QueryRowContext(r.Context(), `SELECT id, is_invite_only FROM spaces WHERE join_code = ? AND deleted_at IS NULL`, code).Scan(&spaceID, &inviteOnly); err != nil {
		s.writeError(w, http.StatusNotFound, "join code not found")
		return
	}
	if inviteOnly == 1 {
		s.writeError(w, http.StatusForbidden, "space is invite only")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT OR IGNORE INTO space_members (space_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`, spaceID, user.ID, nowMS()); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to join space")
		return
	}
	if err := s.ensurePlayerTx(r.Context(), tx, spaceID, user); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create player")
		return
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit join")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"space_id": spaceID})
}

func (s *Server) handleAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	user := userFromContext(r.Context())
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()

	var id, spaceID, emailAddr, status string
	if err := tx.QueryRowContext(r.Context(), `SELECT id, space_id, email, status FROM space_invitations WHERE token = ?`, token).Scan(&id, &spaceID, &emailAddr, &status); err != nil {
		s.writeError(w, http.StatusNotFound, "invitation not found")
		return
	}
	if strings.ToLower(emailAddr) != strings.ToLower(user.Email) || status != "pending" {
		s.writeError(w, http.StatusForbidden, "invitation not valid")
		return
	}
	now := nowMS()
	if _, err = tx.ExecContext(r.Context(), `UPDATE space_invitations SET status = 'accepted', accepted_at = ? WHERE id = ?`, now, id); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to update invitation")
		return
	}
	_, err = tx.ExecContext(r.Context(), `INSERT OR IGNORE INTO space_members (space_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`, spaceID, user.ID, now)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to accept invitation")
		return
	}
	if err := s.ensurePlayerTx(r.Context(), tx, spaceID, user); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create player")
		return
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit invitation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type createPlayerRequest struct {
	DisplayName string `json:"display_name" validate:"required,min=1"`
}

func (s *Server) handleListPlayers(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `
SELECT id, space_id, display_name, user_id, avatar_url, created_at, deleted_at
FROM players WHERE space_id = ? AND deleted_at IS NULL
ORDER BY display_name ASC`, spaceID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list players")
		return
	}
	defer rows.Close()
	players, err := scanPlayers(rows)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to decode players")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"players": players})
}

func (s *Server) handleCreatePlayer(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req createPlayerRequest
	if !s.decodeAndValidate(w, r, &req) {
		return
	}
	player := db.Player{ID: auth.NewID(), SpaceID: spaceID, DisplayName: req.DisplayName, CreatedAt: nowMS()}
	_, err := s.db.ExecContext(r.Context(), `INSERT INTO players (id, space_id, display_name, created_at) VALUES (?, ?, ?, ?)`,
		player.ID, player.SpaceID, player.DisplayName, player.CreatedAt)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create player")
		return
	}
	s.writeJSON(w, http.StatusCreated, player)
}

type updatePlayerRequest struct {
	DisplayName *string `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
}

func (s *Server) handleUpdatePlayer(w http.ResponseWriter, r *http.Request) {
	playerID := chi.URLParam(r, "playerID")
	user := userFromContext(r.Context())
	player, err := s.findPlayer(r.Context(), playerID)
	if err != nil || s.requireAdmin(r.Context(), player.SpaceID, user.ID) != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req updatePlayerRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	parts := []string{}
	args := []any{}
	if req.DisplayName != nil {
		parts = append(parts, "display_name = ?")
		args = append(args, *req.DisplayName)
	}
	if req.AvatarURL != nil {
		parts = append(parts, "avatar_url = ?")
		args = append(args, nullableString(*req.AvatarURL))
	}
	if len(parts) == 0 {
		s.writeError(w, http.StatusBadRequest, "no changes requested")
		return
	}
	args = append(args, playerID)
	_, err = s.db.ExecContext(r.Context(), "UPDATE players SET "+strings.Join(parts, ", ")+" WHERE id = ?", args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to update player")
		return
	}
	updated, _ := s.findPlayer(r.Context(), playerID)
	s.writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleDeletePlayer(w http.ResponseWriter, r *http.Request) {
	playerID := chi.URLParam(r, "playerID")
	user := userFromContext(r.Context())
	player, err := s.findPlayer(r.Context(), playerID)
	if err != nil || s.requireAdmin(r.Context(), player.SpaceID, user.ID) != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	_, err = s.db.ExecContext(r.Context(), `UPDATE players SET deleted_at = ? WHERE id = ?`, nowMS(), playerID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to delete player")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type claimPlayerRequest struct {
	UserID string `json:"user_id" validate:"required"`
}

func (s *Server) handleClaimPlayer(w http.ResponseWriter, r *http.Request) {
	playerID := chi.URLParam(r, "playerID")
	user := userFromContext(r.Context())
	player, err := s.findPlayer(r.Context(), playerID)
	if err != nil || s.requireAdmin(r.Context(), player.SpaceID, user.ID) != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req claimPlayerRequest
	if !s.decodeAndValidate(w, r, &req) {
		return
	}
	if err := s.requireMembership(r.Context(), player.SpaceID, req.UserID); err != nil {
		s.writeError(w, http.StatusBadRequest, "user is not in space")
		return
	}
	_, err = s.db.ExecContext(r.Context(), `UPDATE players SET user_id = ? WHERE id = ?`, req.UserID, playerID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to claim player")
		return
	}
	updated, _ := s.findPlayer(r.Context(), playerID)
	s.writeJSON(w, http.StatusOK, updated)
}

type createMatchRequest struct {
	Kind           string   `json:"kind" validate:"required,oneof=singles doubles"`
	HomePlayers    []string `json:"home_players" validate:"required"`
	VisitorPlayers []string `json:"visitor_players" validate:"required"`
	BestOf         int      `json:"best_of" validate:"required,oneof=1 3 5 7"`
	PointsToWin    int      `json:"points_to_win" validate:"required,oneof=5 11"`
}

func (s *Server) handleCreateMatch(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req createMatchRequest
	if !s.decodeAndValidate(w, r, &req) {
		return
	}
	if err := validateMatchParticipants(req.Kind, req.HomePlayers, req.VisitorPlayers); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	matchID, err := s.createMatch(r.Context(), nil, spaceID, user.ID, req.Kind, req.HomePlayers, req.VisitorPlayers, req.BestOf, req.PointsToWin, "", "", nil, nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create match")
		return
	}
	s.respondMatch(w, r, matchID, http.StatusCreated)
}

func (s *Server) handleListMatches(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	status := r.URL.Query().Get("status")
	limit := 20
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	query := `SELECT id, space_id, kind, tournament_id, tournament_phase, tournament_group_id, tournament_bracket_round, best_of, points_to_win, status, winner_side, started_at, completed_at, created_by, created_at, updated_at, deleted_at
FROM matches WHERE space_id = ? AND deleted_at IS NULL`
	args := []any{spaceID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list matches")
		return
	}
	defer rows.Close()
	matches, err := scanMatches(rows)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to decode matches")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
}

func (s *Server) handleListTournaments(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	statusFilter := r.URL.Query().Get("status")
	query := `SELECT id, space_id, name, format, best_of, points_to_win, status, winner_player_id, created_by, created_at, updated_at, deleted_at
FROM tournaments WHERE space_id = ? AND deleted_at IS NULL`
	args := []any{spaceID}
	if statusFilter != "" {
		query += ` AND status = ?`
		args = append(args, statusFilter)
	}
	query += ` ORDER BY updated_at DESC`
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list tournaments")
		return
	}
	defer rows.Close()
	var tournaments []db.Tournament
	for rows.Next() {
		var t db.Tournament
		var winner sql.NullString
		var deleted sql.NullInt64
		if err := rows.Scan(&t.ID, &t.SpaceID, &t.Name, &t.Format, &t.BestOf, &t.PointsToWin, &t.Status, &winner, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &deleted); err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to decode tournaments")
			return
		}
		if winner.Valid {
			t.WinnerPlayerID = &winner.String
		}
		if deleted.Valid {
			t.DeletedAt = &deleted.Int64
		}
		tournaments = append(tournaments, t)
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"tournaments": tournaments})
}

func (s *Server) handleGetMatch(w http.ResponseWriter, r *http.Request) {
	matchID := chi.URLParam(r, "matchID")
	match, payload, err := s.loadMatchPayload(r.Context(), matchID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "match not found")
		return
	}
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), match.SpaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	s.writeJSON(w, http.StatusOK, payload)
}

type scoreRequest struct {
	Side string `json:"side" validate:"required,oneof=home visitor"`
}

func (s *Server) handleScoreMatch(w http.ResponseWriter, r *http.Request) {
	matchID := chi.URLParam(r, "matchID")
	user := userFromContext(r.Context())
	match, _, err := s.loadMatchPayload(r.Context(), matchID)
	if err != nil || s.requireMembership(r.Context(), match.SpaceID, user.ID) != nil || match.Status != "in_progress" {
		s.writeError(w, http.StatusBadRequest, "match is not scoreable")
		return
	}
	var req scoreRequest
	if !s.decodeAndValidate(w, r, &req) {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()
	currentGame, err := s.currentGameTx(r.Context(), tx, matchID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to load current game")
		return
	}
	sequence := s.nextSequenceTx(r.Context(), tx, currentGame.ID)
	result := domain.ApplyScore(match.PointsToWin, currentGame.HomeScore, currentGame.VisitorScore, req.Side)
	event := db.ScoreEvent{
		ID:                auth.NewID(),
		GameID:            currentGame.ID,
		Sequence:          sequence,
		ScorerSide:        req.Side,
		HomeScoreAfter:    result.GameState.Home,
		VisitorScoreAfter: result.GameState.Visitor,
		OccurredAt:        nowMS(),
	}
	if _, err := tx.ExecContext(r.Context(), `
INSERT INTO score_events (id, game_id, sequence, scorer_side, home_score_after, visitor_score_after, occurred_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, event.ID, event.GameID, event.Sequence, event.ScorerSide, event.HomeScoreAfter, event.VisitorScoreAfter, event.OccurredAt); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to write score")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE games SET home_score = ?, visitor_score = ? WHERE id = ?`, event.HomeScoreAfter, event.VisitorScoreAfter, currentGame.ID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to update game")
		return
	}

	s.hub.Broadcast(matchID, ws.Message{Type: "score_event", Data: map[string]any{
		"id":                  event.ID,
		"game_id":             event.GameID,
		"sequence":            event.Sequence,
		"scorer_side":         event.ScorerSide,
		"home_score_after":    event.HomeScoreAfter,
		"visitor_score_after": event.VisitorScoreAfter,
		"occurred_at":         event.OccurredAt,
		"game_state":          result.GameState,
	}})

	if result.GameCompleted {
		now := nowMS()
		if _, err := tx.ExecContext(r.Context(), `UPDATE games SET status = 'completed', completed_at = ?, home_score = ?, visitor_score = ? WHERE id = ?`,
			now, event.HomeScoreAfter, event.VisitorScoreAfter, currentGame.ID); err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to complete game")
			return
		}
		completedGames, err := s.completedGameStatesTx(r.Context(), tx, matchID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to load completed games")
			return
		}
		if winner, done := domain.MatchWinner(match.BestOf, completedGames); done {
			if _, err := tx.ExecContext(r.Context(), `UPDATE matches SET status = 'completed', winner_side = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
				winner, now, now, matchID); err != nil {
				s.writeError(w, http.StatusInternalServerError, "failed to complete match")
				return
			}
			if err := s.advanceTournamentMatchTx(r.Context(), tx, matchID); err != nil {
				s.writeError(w, http.StatusInternalServerError, "failed to advance tournament")
				return
			}
			s.hub.Broadcast(matchID, ws.Message{Type: "match_completed", Data: map[string]any{"winner_side": winner, "final_games": completedGames}})
		} else {
			nextGameID := auth.NewID()
			if _, err := tx.ExecContext(r.Context(), `
INSERT INTO games (id, match_id, game_number, home_score, visitor_score, status, started_at)
VALUES (?, ?, ?, 0, 0, 'in_progress', ?)`, nextGameID, matchID, len(completedGames)+1, now); err != nil {
				s.writeError(w, http.StatusInternalServerError, "failed to create next game")
				return
			}
			s.hub.Broadcast(matchID, ws.Message{Type: "game_completed", Data: map[string]any{
				"game_id":       currentGame.ID,
				"home_score":    event.HomeScoreAfter,
				"visitor_score": event.VisitorScoreAfter,
				"next_game_id":  nextGameID,
			}})
		}
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE matches SET updated_at = ? WHERE id = ?`, nowMS(), matchID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to bump match")
		return
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit score")
		return
	}
	s.respondMatch(w, r, matchID, http.StatusOK)
}

func (s *Server) handleUndoMatch(w http.ResponseWriter, r *http.Request) {
	matchID := chi.URLParam(r, "matchID")
	user := userFromContext(r.Context())
	match, _, err := s.loadMatchPayload(r.Context(), matchID)
	if err != nil || s.requireMembership(r.Context(), match.SpaceID, user.ID) != nil || match.Status != "in_progress" {
		s.writeError(w, http.StatusBadRequest, "match cannot be undone")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()

	game, err := s.currentOrLatestCompletedGameTx(r.Context(), tx, matchID)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "no score to undo")
		return
	}
	event, err := s.lastScoreEventTx(r.Context(), tx, game.ID)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "no score to undo")
		return
	}
	prevHome, prevVisitor := 0, 0
	if event.Sequence > 1 {
		prev, err := s.scoreEventBySequenceTx(r.Context(), tx, game.ID, event.Sequence-1)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to load previous event")
			return
		}
		prevHome = prev.HomeScoreAfter
		prevVisitor = prev.VisitorScoreAfter
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM score_events WHERE id = ?`, event.ID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to remove score")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE games SET home_score = ?, visitor_score = ?, status = 'in_progress', completed_at = NULL WHERE id = ?`, prevHome, prevVisitor, game.ID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to rewind game")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM games WHERE match_id = ? AND status = 'in_progress' AND game_number > ?`, matchID, game.GameNumber); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to clean later game")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE matches SET updated_at = ?, status = 'in_progress', winner_side = NULL, completed_at = NULL WHERE id = ?`, nowMS(), matchID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to rewind match")
		return
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit undo")
		return
	}
	s.respondMatch(w, r, matchID, http.StatusOK)
}

type editMatchRequest struct {
	Games []struct {
		GameNumber   int `json:"game_number"`
		HomeScore    int `json:"home_score"`
		VisitorScore int `json:"visitor_score"`
	} `json:"games"`
}

func (s *Server) handleEditMatch(w http.ResponseWriter, r *http.Request) {
	matchID := chi.URLParam(r, "matchID")
	user := userFromContext(r.Context())
	match, payload, err := s.loadMatchPayload(r.Context(), matchID)
	if err != nil || match.Status != "completed" {
		s.writeError(w, http.StatusBadRequest, "match is not editable")
		return
	}
	if err := s.requireAdmin(r.Context(), match.SpaceID, user.ID); err != nil && user.ID != match.CreatedBy {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req editMatchRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(req.Games) == 0 {
		s.writeError(w, http.StatusBadRequest, "games are required")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()

	var gameStates []domain.GameState
	for _, game := range req.Games {
		if _, err := tx.ExecContext(r.Context(), `UPDATE games SET home_score = ?, visitor_score = ?, status = 'completed' WHERE match_id = ? AND game_number = ?`,
			game.HomeScore, game.VisitorScore, matchID, game.GameNumber); err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to update games")
			return
		}
		gameStates = append(gameStates, domain.GameState{Home: game.HomeScore, Visitor: game.VisitorScore})
	}
	winner, _ := domain.MatchWinner(match.BestOf, gameStates)
	diff := map[string]any{"before": payload, "after": req}
	diffJSON, _ := json.Marshal(diff)
	now := nowMS()
	if _, err := tx.ExecContext(r.Context(), `UPDATE matches SET winner_side = ?, updated_at = ? WHERE id = ?`, winner, now, matchID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to update match winner")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO match_edits (id, match_id, edited_by, edited_at, changes_json) VALUES (?, ?, ?, ?, ?)`,
		auth.NewID(), matchID, user.ID, now, string(diffJSON)); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to store audit log")
		return
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit edit")
		return
	}
	s.hub.Broadcast(matchID, ws.Message{Type: "match_updated", Data: map[string]any{"match": matchID}})
	s.respondMatch(w, r, matchID, http.StatusOK)
}

func (s *Server) handleDeleteMatch(w http.ResponseWriter, r *http.Request) {
	matchID := chi.URLParam(r, "matchID")
	user := userFromContext(r.Context())
	match, _, err := s.loadMatchPayload(r.Context(), matchID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if err := s.requireAdmin(r.Context(), match.SpaceID, user.ID); err != nil && user.ID != match.CreatedBy {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	_, err = s.db.ExecContext(r.Context(), `UPDATE matches SET deleted_at = ?, updated_at = ? WHERE id = ?`, nowMS(), nowMS(), matchID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to delete match")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMatchWebSocket(w http.ResponseWriter, r *http.Request) {
	matchID := chi.URLParam(r, "matchID")
	user := userFromContext(r.Context())
	match, _, err := s.loadMatchPayload(r.Context(), matchID)
	if err != nil || s.requireMembership(r.Context(), match.SpaceID, user.ID) != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	client := s.hub.Add(matchID, conn)
	defer s.hub.Remove(matchID, client)
	client.Run(r.Context())
}

type createTournamentRequest struct {
	Name        string   `json:"name" validate:"required,min=2"`
	Format      string   `json:"format" validate:"required,oneof=round_robin bracket"`
	BestOf      int      `json:"best_of" validate:"required,oneof=1 3 5 7"`
	PointsToWin int      `json:"points_to_win" validate:"required,oneof=5 11"`
	PlayerIDs   []string `json:"player_ids" validate:"required"`
	Seeds       []int    `json:"seeds"`
}

func (s *Server) handleCreateTournament(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req createTournamentRequest
	if !s.decodeAndValidate(w, r, &req) {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()
	tournamentID := auth.NewID()
	now := nowMS()
	_, err = tx.ExecContext(r.Context(), `
INSERT INTO tournaments (id, space_id, name, format, best_of, points_to_win, status, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, 'setup', ?, ?, ?)`,
		tournamentID, spaceID, req.Name, req.Format, req.BestOf, req.PointsToWin, user.ID, now, now)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create tournament")
		return
	}
	for i, playerID := range req.PlayerIDs {
		var seed any
		if i < len(req.Seeds) {
			seed = req.Seeds[i]
		}
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO tournament_players (tournament_id, player_id, seed) VALUES (?, ?, ?)`, tournamentID, playerID, seed); err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to attach players")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit tournament")
		return
	}
	tournament, err := s.findTournament(r.Context(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to reload tournament")
		return
	}
	s.writeJSON(w, http.StatusCreated, tournament)
}

func (s *Server) handleStartTournament(w http.ResponseWriter, r *http.Request) {
	tournamentID := chi.URLParam(r, "tournamentID")
	user := userFromContext(r.Context())
	tournament, err := s.findTournament(r.Context(), tournamentID)
	if err != nil || s.requireMembership(r.Context(), tournament.SpaceID, user.ID) != nil {
		s.writeError(w, http.StatusNotFound, "tournament not found")
		return
	}
	playerIDs, err := s.tournamentPlayerIDs(r.Context(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to load players")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()

	if tournament.Format == "round_robin" {
		for _, pair := range domain.RoundRobinPairs(playerIDs) {
			if _, err := s.createMatch(r.Context(), tx, tournament.SpaceID, user.ID, "singles", []string{pair.Home}, []string{pair.Away}, tournament.BestOf, tournament.PointsToWin, tournamentID, "round_robin", nil, nil); err != nil {
				s.writeError(w, http.StatusInternalServerError, "failed to create round robin matches")
				return
			}
		}
	} else {
		for _, match := range domain.GenerateBracket(playerIDs) {
			var homePlayers, visitorPlayers []string
			if match.Home != nil && *match.Home != "" {
				homePlayers = []string{*match.Home}
			}
			if match.Visitor != nil && *match.Visitor != "" {
				visitorPlayers = []string{*match.Visitor}
			}
			matchID, err := s.createMatch(r.Context(), tx, tournament.SpaceID, user.ID, "singles", homePlayers, visitorPlayers, tournament.BestOf, tournament.PointsToWin, tournamentID, "bracket", nil, &match.Round)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, "failed to create bracket matches")
				return
			}
			if match.HasBye {
				if err := s.completeByeMatchTx(r.Context(), tx, matchID); err != nil {
					s.writeError(w, http.StatusInternalServerError, "failed to auto-advance bye")
					return
				}
			}
		}
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE tournaments SET status = 'in_progress', updated_at = ? WHERE id = ?`, nowMS(), tournamentID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to update tournament")
		return
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit tournament start")
		return
	}
	s.handleGetTournament(w, r)
}

func (s *Server) handleGetTournament(w http.ResponseWriter, r *http.Request) {
	tournamentID := chi.URLParam(r, "tournamentID")
	tournament, err := s.findTournament(r.Context(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "tournament not found")
		return
	}
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), tournament.SpaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	playerIDs, _ := s.tournamentPlayerIDs(r.Context(), tournamentID)
	matches := s.matchesByTournament(r.Context(), tournamentID)
	standings, _ := s.computeStandings(r.Context(), tournamentID)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"tournament": tournament,
		"players":    playerIDs,
		"matches":    matches,
		"standings":  standings,
	})
}

func (s *Server) handleTournamentStandings(w http.ResponseWriter, r *http.Request) {
	tournamentID := chi.URLParam(r, "tournamentID")
	tournament, err := s.findTournament(r.Context(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "tournament not found")
		return
	}
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), tournament.SpaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	standings, err := s.computeStandings(r.Context(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to compute standings")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"standings": standings})
}

func (s *Server) handleDeleteTournament(w http.ResponseWriter, r *http.Request) {
	tournamentID := chi.URLParam(r, "tournamentID")
	tournament, err := s.findTournament(r.Context(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "tournament not found")
		return
	}
	user := userFromContext(r.Context())
	if err := s.requireAdmin(r.Context(), tournament.SpaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	_, err = s.db.ExecContext(r.Context(), `UPDATE tournaments SET deleted_at = ?, updated_at = ? WHERE id = ?`, nowMS(), nowMS(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to delete tournament")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePlayerStats(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	playerID := chi.URLParam(r, "playerID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	stats, err := s.computePlayerStats(r.Context(), spaceID, playerID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to compute stats")
		return
	}
	s.writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleHeadToHead(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	a := r.URL.Query().Get("a")
	b := r.URL.Query().Get("b")
	stats, err := s.computeHeadToHead(r.Context(), spaceID, a, b)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to compute h2h")
		return
	}
	s.writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleAdminSpaces(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if !user.IsSuperAdmin {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT id, name, description, is_invite_only, join_code, created_by, created_at, deleted_at FROM spaces ORDER BY created_at DESC`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list spaces")
		return
	}
	defer rows.Close()
	spaces, err := scanSpaces(rows)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to decode spaces")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"spaces": spaces})
}

func (s *Server) handleAdminWipe(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if !user.IsSuperAdmin || s.cfg.Env != "dev" || r.URL.Query().Get("confirm") != "yes" {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	tables := []string{"match_edits", "score_events", "games", "match_participants", "matches", "tournament_players", "tournaments", "space_invitations", "players", "space_members", "spaces", "magic_link_tokens", "sessions"}
	for _, table := range tables {
		if _, err := s.db.ExecContext(r.Context(), "DELETE FROM "+table); err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to wipe data")
			return
		}
	}
	if _, err := s.db.ExecContext(r.Context(), `DELETE FROM users WHERE is_super_admin = 0`); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to wipe users")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) decodeAndValidate(w http.ResponseWriter, r *http.Request, dst any) bool {
	if !s.decodeJSON(w, r, dst) {
		return false
	}
	if err := s.validate.Struct(dst); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func (s *Server) decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid json")
		return false
	}
	return true
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"error": message})
}

func (s *Server) findOrCreateUserTx(ctx context.Context, tx *sql.Tx, emailAddr string) (db.User, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, email, display_name, avatar_url, is_super_admin, created_at FROM users WHERE email = ?`, strings.ToLower(emailAddr))
	user, err := scanUser(row)
	if err == nil {
		if s.cfg.SuperAdminEmail != "" && strings.EqualFold(s.cfg.SuperAdminEmail, user.Email) && !user.IsSuperAdmin {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET is_super_admin = 1 WHERE id = ?`, user.ID); err == nil {
				user.IsSuperAdmin = true
			}
		}
		return user, nil
	}
	now := nowMS()
	user = db.User{
		ID:           auth.NewID(),
		Email:        strings.ToLower(emailAddr),
		DisplayName:  auth.DisplayNameFromEmail(emailAddr),
		IsSuperAdmin: s.cfg.SuperAdminEmail != "" && strings.EqualFold(s.cfg.SuperAdminEmail, emailAddr),
		CreatedAt:    now,
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO users (id, email, display_name, is_super_admin, created_at) VALUES (?, ?, ?, ?, ?)`,
		user.ID, user.Email, user.DisplayName, boolToInt(user.IsSuperAdmin), user.CreatedAt)
	return user, err
}

func (s *Server) requireMembership(ctx context.Context, spaceID, userID string) error {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM space_members WHERE space_id = ? AND user_id = ?`, spaceID, userID).Scan(&count)
	if err != nil || count == 0 {
		return errors.New("missing membership")
	}
	return nil
}

func (s *Server) requireAdmin(ctx context.Context, spaceID, userID string) error {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM space_members WHERE space_id = ? AND user_id = ? AND role = 'admin'`, spaceID, userID).Scan(&count)
	if err != nil || count == 0 {
		return errors.New("missing admin")
	}
	return nil
}

func (s *Server) countAdmins(ctx context.Context, spaceID string) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM space_members WHERE space_id = ? AND role = 'admin'`, spaceID).Scan(&count)
	return count, err
}

func (s *Server) ensurePlayerTx(ctx context.Context, tx *sql.Tx, spaceID string, user db.User) error {
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM players WHERE space_id = ? AND user_id = ? AND deleted_at IS NULL`, spaceID, user.ID).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO players (id, space_id, display_name, user_id, created_at) VALUES (?, ?, ?, ?, ?)`,
		auth.NewID(), spaceID, user.DisplayName, user.ID, nowMS())
	return err
}

func (s *Server) findPlayer(ctx context.Context, playerID string) (db.Player, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, space_id, display_name, user_id, avatar_url, created_at, deleted_at FROM players WHERE id = ?`, playerID)
	return scanPlayer(row)
}

func validateMatchParticipants(kind string, home, visitor []string) error {
	want := 1
	if kind == "doubles" {
		want = 2
	}
	if len(home) != want || len(visitor) != want {
		return fmt.Errorf("%s requires %d players per side", kind, want)
	}
	return nil
}

func (s *Server) createMatch(ctx context.Context, tx *sql.Tx, spaceID, createdBy, kind string, homePlayers, visitorPlayers []string, bestOf, pointsToWin int, tournamentID, phase string, groupID *string, bracketRound *int) (string, error) {
	exec := sqlExecutor{s.db, tx}
	matchID := auth.NewID()
	now := nowMS()
	var tournament any
	if tournamentID != "" {
		tournament = tournamentID
	}
	_, err := exec.ExecContext(ctx, `
INSERT INTO matches (id, space_id, kind, tournament_id, tournament_phase, tournament_group_id, tournament_bracket_round, best_of, points_to_win, status, started_at, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'in_progress', ?, ?, ?, ?)`,
		matchID, spaceID, kind, tournament, nullableString(phase), nullableStringPtr(groupID), nullableIntPtr(bracketRound), bestOf, pointsToWin, now, createdBy, now, now)
	if err != nil {
		return "", err
	}
	for i, playerID := range homePlayers {
		if _, err := exec.ExecContext(ctx, `INSERT INTO match_participants (match_id, player_id, side, slot) VALUES (?, ?, 'home', ?)`, matchID, playerID, i+1); err != nil {
			return "", err
		}
	}
	for i, playerID := range visitorPlayers {
		if _, err := exec.ExecContext(ctx, `INSERT INTO match_participants (match_id, player_id, side, slot) VALUES (?, ?, 'visitor', ?)`, matchID, playerID, i+1); err != nil {
			return "", err
		}
	}
	if _, err := exec.ExecContext(ctx, `INSERT INTO games (id, match_id, game_number, status, started_at) VALUES (?, ?, 1, 'in_progress', ?)`, auth.NewID(), matchID, now); err != nil {
		return "", err
	}
	return matchID, nil
}

func (s *Server) respondMatch(w http.ResponseWriter, r *http.Request, matchID string, status int) {
	_, payload, err := s.loadMatchPayload(r.Context(), matchID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "match not found")
		return
	}
	s.writeJSON(w, status, payload)
}

func (s *Server) loadMatchPayload(ctx context.Context, matchID string) (db.Match, map[string]any, error) {
	matchRow := s.db.QueryRowContext(ctx, `SELECT id, space_id, kind, tournament_id, tournament_phase, tournament_group_id, tournament_bracket_round, best_of, points_to_win, status, winner_side, started_at, completed_at, created_by, created_at, updated_at, deleted_at FROM matches WHERE id = ? AND deleted_at IS NULL`, matchID)
	match, err := scanMatch(matchRow)
	if err != nil {
		return db.Match{}, nil, err
	}
	games, _ := s.gamesForMatch(ctx, matchID)
	participants, _ := s.participantsForMatch(ctx, matchID)
	events, _ := s.scoreEventsForMatch(ctx, matchID)
	payload := map[string]any{
		"match":        match,
		"games":        games,
		"participants": participants,
		"score_events": events,
	}
	return match, payload, nil
}

func (s *Server) gamesForMatch(ctx context.Context, matchID string) ([]db.Game, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, match_id, game_number, home_score, visitor_score, status, started_at, completed_at FROM games WHERE match_id = ? ORDER BY game_number ASC`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var games []db.Game
	for rows.Next() {
		game, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, game)
	}
	return games, nil
}

func (s *Server) participantsForMatch(ctx context.Context, matchID string) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT mp.side, mp.slot, p.id, p.display_name, p.user_id, p.avatar_url
FROM match_participants mp
JOIN players p ON p.id = mp.player_id
WHERE mp.match_id = ?
ORDER BY mp.side ASC, mp.slot ASC`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var participants []map[string]any
	for rows.Next() {
		var side string
		var slot int
		var player db.Player
		var userID, avatar sql.NullString
		if err := rows.Scan(&side, &slot, &player.ID, &player.DisplayName, &userID, &avatar); err != nil {
			return nil, err
		}
		if userID.Valid {
			player.UserID = &userID.String
		}
		if avatar.Valid {
			player.AvatarURL = &avatar.String
		}
		participants = append(participants, map[string]any{"side": side, "slot": slot, "player": player})
	}
	return participants, nil
}

func (s *Server) scoreEventsForMatch(ctx context.Context, matchID string) ([]db.ScoreEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT se.id, se.game_id, se.sequence, se.scorer_side, se.home_score_after, se.visitor_score_after, se.occurred_at
FROM score_events se
JOIN games g ON g.id = se.game_id
WHERE g.match_id = ?
ORDER BY g.game_number ASC, se.sequence ASC`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []db.ScoreEvent
	for rows.Next() {
		var event db.ScoreEvent
		if err := rows.Scan(&event.ID, &event.GameID, &event.Sequence, &event.ScorerSide, &event.HomeScoreAfter, &event.VisitorScoreAfter, &event.OccurredAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *Server) currentGameTx(ctx context.Context, tx *sql.Tx, matchID string) (db.Game, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, match_id, game_number, home_score, visitor_score, status, started_at, completed_at FROM games WHERE match_id = ? AND status = 'in_progress' ORDER BY game_number DESC LIMIT 1`, matchID)
	return scanGame(row)
}

func (s *Server) currentOrLatestCompletedGameTx(ctx context.Context, tx *sql.Tx, matchID string) (db.Game, error) {
	row := tx.QueryRowContext(ctx, `
SELECT id, match_id, game_number, home_score, visitor_score, status, started_at, completed_at
FROM games WHERE match_id = ?
ORDER BY CASE WHEN status = 'in_progress' THEN 0 ELSE 1 END ASC, game_number DESC LIMIT 1`, matchID)
	return scanGame(row)
}

func (s *Server) nextSequenceTx(ctx context.Context, tx *sql.Tx, gameID string) int {
	var sequence int
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM score_events WHERE game_id = ?`, gameID).Scan(&sequence)
	if sequence == 0 {
		return 1
	}
	return sequence
}

func (s *Server) completedGameStatesTx(ctx context.Context, tx *sql.Tx, matchID string) ([]domain.GameState, error) {
	rows, err := tx.QueryContext(ctx, `SELECT home_score, visitor_score FROM games WHERE match_id = ? AND status = 'completed' ORDER BY game_number ASC`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var states []domain.GameState
	for rows.Next() {
		var state domain.GameState
		if err := rows.Scan(&state.Home, &state.Visitor); err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, nil
}

func (s *Server) lastScoreEventTx(ctx context.Context, tx *sql.Tx, gameID string) (db.ScoreEvent, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, game_id, sequence, scorer_side, home_score_after, visitor_score_after, occurred_at FROM score_events WHERE game_id = ? ORDER BY sequence DESC LIMIT 1`, gameID)
	var event db.ScoreEvent
	err := row.Scan(&event.ID, &event.GameID, &event.Sequence, &event.ScorerSide, &event.HomeScoreAfter, &event.VisitorScoreAfter, &event.OccurredAt)
	return event, err
}

func (s *Server) scoreEventBySequenceTx(ctx context.Context, tx *sql.Tx, gameID string, sequence int) (db.ScoreEvent, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, game_id, sequence, scorer_side, home_score_after, visitor_score_after, occurred_at FROM score_events WHERE game_id = ? AND sequence = ?`, gameID, sequence)
	var event db.ScoreEvent
	err := row.Scan(&event.ID, &event.GameID, &event.Sequence, &event.ScorerSide, &event.HomeScoreAfter, &event.VisitorScoreAfter, &event.OccurredAt)
	return event, err
}

func (s *Server) findTournament(ctx context.Context, tournamentID string) (db.Tournament, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, space_id, name, format, best_of, points_to_win, status, winner_player_id, created_by, created_at, updated_at, deleted_at FROM tournaments WHERE id = ? AND deleted_at IS NULL`, tournamentID)
	var tournament db.Tournament
	var winner sql.NullString
	var deleted sql.NullInt64
	if err := row.Scan(&tournament.ID, &tournament.SpaceID, &tournament.Name, &tournament.Format, &tournament.BestOf, &tournament.PointsToWin, &tournament.Status, &winner, &tournament.CreatedBy, &tournament.CreatedAt, &tournament.UpdatedAt, &deleted); err != nil {
		return tournament, err
	}
	if winner.Valid {
		tournament.WinnerPlayerID = &winner.String
	}
	if deleted.Valid {
		tournament.DeletedAt = &deleted.Int64
	}
	return tournament, nil
}

func (s *Server) tournamentPlayerIDs(ctx context.Context, tournamentID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT player_id FROM tournament_players WHERE tournament_id = ? ORDER BY COALESCE(seed, 999999), player_id`, tournamentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Server) matchesByTournament(ctx context.Context, tournamentID string) []db.Match {
	rows, err := s.db.QueryContext(ctx, `SELECT id, space_id, kind, tournament_id, tournament_phase, tournament_group_id, tournament_bracket_round, best_of, points_to_win, status, winner_side, started_at, completed_at, created_by, created_at, updated_at, deleted_at FROM matches WHERE tournament_id = ? AND deleted_at IS NULL ORDER BY COALESCE(tournament_bracket_round, 0), created_at`, tournamentID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	matches, _ := scanMatches(rows)
	return matches
}

func (s *Server) computeStandings(ctx context.Context, tournamentID string) ([]map[string]any, error) {
	playerIDs, err := s.tournamentPlayerIDs(ctx, tournamentID)
	if err != nil {
		return nil, err
	}
	type standing struct {
		PlayerID    string
		Wins        int
		Losses      int
		SetsWon     int
		SetsLost    int
		PointsWon   int
		PointsLost  int
		PointsRatio float64
		HeadToHead  map[string]int
	}
	stats := map[string]*standing{}
	for _, id := range playerIDs {
		stats[id] = &standing{PlayerID: id, HeadToHead: map[string]int{}}
	}
	matches := s.matchesByTournament(ctx, tournamentID)
	for _, match := range matches {
		if match.Status != "completed" || match.WinnerSide == nil {
			continue
		}
		participants, _ := s.participantsForMatch(ctx, match.ID)
		if len(participants) < 2 {
			continue
		}
		home := participants[0]["player"].(db.Player).ID
		visitor := participants[len(participants)-1]["player"].(db.Player).ID
		games, _ := s.gamesForMatch(ctx, match.ID)
		homeSets, visitorSets, homePoints, visitorPoints := 0, 0, 0, 0
		for _, game := range games {
			homePoints += game.HomeScore
			visitorPoints += game.VisitorScore
			if game.HomeScore > game.VisitorScore {
				homeSets++
			}
			if game.VisitorScore > game.HomeScore {
				visitorSets++
			}
		}
		stats[home].SetsWon += homeSets
		stats[home].SetsLost += visitorSets
		stats[home].PointsWon += homePoints
		stats[home].PointsLost += visitorPoints
		stats[visitor].SetsWon += visitorSets
		stats[visitor].SetsLost += homeSets
		stats[visitor].PointsWon += visitorPoints
		stats[visitor].PointsLost += homePoints
		if *match.WinnerSide == "home" {
			stats[home].Wins++
			stats[visitor].Losses++
			stats[home].HeadToHead[visitor]++
		} else {
			stats[visitor].Wins++
			stats[home].Losses++
			stats[visitor].HeadToHead[home]++
		}
	}
	var standings []map[string]any
	for _, id := range playerIDs {
		item := stats[id]
		if item.PointsLost == 0 {
			item.PointsRatio = float64(item.PointsWon)
		} else {
			item.PointsRatio = float64(item.PointsWon) / float64(item.PointsLost)
		}
		standings = append(standings, map[string]any{
			"player_id":    id,
			"wins":         item.Wins,
			"losses":       item.Losses,
			"sets_won":     item.SetsWon,
			"sets_lost":    item.SetsLost,
			"points_won":   item.PointsWon,
			"points_lost":  item.PointsLost,
			"points_ratio": math.Round(item.PointsRatio*1000) / 1000,
		})
	}
	slices.SortFunc(standings, func(a, b map[string]any) int {
		aw, bw := a["wins"].(int), b["wins"].(int)
		if aw != bw {
			return bw - aw
		}
		ap, bp := a["player_id"].(string), b["player_id"].(string)
		if stats[ap].HeadToHead[bp] != stats[bp].HeadToHead[ap] {
			return stats[bp].HeadToHead[ap] - stats[ap].HeadToHead[bp]
		}
		ar, br := a["points_ratio"].(float64), b["points_ratio"].(float64)
		switch {
		case ar > br:
			return -1
		case ar < br:
			return 1
		default:
			return strings.Compare(ap, bp)
		}
	})
	for i := range standings {
		standings[i]["rank"] = i + 1
	}
	return standings, nil
}

func (s *Server) completeByeMatchTx(ctx context.Context, tx *sql.Tx, matchID string) error {
	participantsRows, err := tx.QueryContext(ctx, `SELECT side, player_id FROM match_participants WHERE match_id = ?`, matchID)
	if err != nil {
		return err
	}
	defer participantsRows.Close()
	winner := "home"
	count := 0
	for participantsRows.Next() {
		var side, playerID string
		if err := participantsRows.Scan(&side, &playerID); err != nil {
			return err
		}
		count++
		if side == "visitor" {
			winner = "visitor"
		}
	}
	if count != 1 {
		return nil
	}
	now := nowMS()
	if _, err := tx.ExecContext(ctx, `UPDATE games SET home_score = 1, visitor_score = 0, status = 'completed', completed_at = ? WHERE match_id = ?`, now, matchID); err != nil {
		return err
	}
	if winner == "visitor" {
		if _, err := tx.ExecContext(ctx, `UPDATE games SET home_score = 0, visitor_score = 1 WHERE match_id = ?`, matchID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE matches SET status = 'completed', winner_side = ?, completed_at = ?, updated_at = ? WHERE id = ?`, winner, now, now, matchID); err != nil {
		return err
	}
	return s.advanceTournamentMatchTx(ctx, tx, matchID)
}

func (s *Server) advanceTournamentMatchTx(ctx context.Context, tx *sql.Tx, matchID string) error {
	var tournamentID, phase sql.NullString
	var round sql.NullInt64
	var winnerSide sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT tournament_id, tournament_phase, tournament_bracket_round, winner_side FROM matches WHERE id = ?`, matchID).Scan(&tournamentID, &phase, &round, &winnerSide)
	if err != nil || !tournamentID.Valid || !winnerSide.Valid {
		return err
	}
	if phase.String == "round_robin" {
		var remaining int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM matches WHERE tournament_id = ? AND status != 'completed' AND deleted_at IS NULL`, tournamentID.String).Scan(&remaining); err != nil {
			return err
		}
		if remaining == 0 {
			standings, err := s.computeStandings(ctx, tournamentID.String)
			if err != nil {
				return err
			}
			var winner any
			if len(standings) > 0 {
				winner = standings[0]["player_id"]
			}
			_, err = tx.ExecContext(ctx, `UPDATE tournaments SET status = 'completed', winner_player_id = ?, updated_at = ? WHERE id = ?`, winner, nowMS(), tournamentID.String)
			return err
		}
		return nil
	}

	participantsRows, err := tx.QueryContext(ctx, `SELECT side, player_id FROM match_participants WHERE match_id = ? ORDER BY slot`, matchID)
	if err != nil {
		return err
	}
	defer participantsRows.Close()
	sideToPlayer := map[string]string{}
	for participantsRows.Next() {
		var side, playerID string
		if err := participantsRows.Scan(&side, &playerID); err != nil {
			return err
		}
		sideToPlayer[side] = playerID
	}
	playerID := sideToPlayer[winnerSide.String]
	if playerID == "" || !round.Valid {
		return nil
	}
	nextRound := int(round.Int64) + 1
	rows, err := tx.QueryContext(ctx, `SELECT id FROM matches WHERE tournament_id = ? AND tournament_phase = 'bracket' AND tournament_bracket_round = ? ORDER BY created_at`, tournamentID.String, nextRound)
	if err != nil {
		return err
	}
	defer rows.Close()
	var nextMatches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		nextMatches = append(nextMatches, id)
	}
	if len(nextMatches) == 0 {
		_, err := tx.ExecContext(ctx, `UPDATE tournaments SET status = 'completed', winner_player_id = ?, updated_at = ? WHERE id = ?`, playerID, nowMS(), tournamentID.String)
		return err
	}
	currentRoundRows, err := tx.QueryContext(ctx, `SELECT id FROM matches WHERE tournament_id = ? AND tournament_phase = 'bracket' AND tournament_bracket_round = ? ORDER BY created_at`, tournamentID.String, round.Int64)
	if err != nil {
		return err
	}
	defer currentRoundRows.Close()
	var currentRound []string
	for currentRoundRows.Next() {
		var id string
		if err := currentRoundRows.Scan(&id); err != nil {
			return err
		}
		currentRound = append(currentRound, id)
	}
	index := slices.Index(currentRound, matchID)
	if index < 0 {
		return nil
	}
	target := nextMatches[index/2]
	side := "home"
	slot := 1
	if index%2 == 1 {
		side = "visitor"
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO match_participants (match_id, player_id, side, slot) VALUES (?, ?, ?, ?)`, target, playerID, side, slot); err != nil {
		return err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM match_participants WHERE match_id = ?`, target).Scan(&count); err != nil {
		return err
	}
	if count == 1 {
		return s.completeByeMatchTx(ctx, tx, target)
	}
	return nil
}

func (s *Server) computePlayerStats(ctx context.Context, spaceID, playerID string) (map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT m.id, m.winner_side, mp.side
FROM matches m
JOIN match_participants mp ON mp.match_id = m.id
WHERE m.space_id = ? AND m.status = 'completed' AND mp.player_id = ? AND m.deleted_at IS NULL
ORDER BY m.completed_at ASC`, spaceID, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var total, wins, losses int
	var outcomes []domain.MatchOutcome
	for rows.Next() {
		var matchID string
		var winnerSide, side string
		if err := rows.Scan(&matchID, &winnerSide, &side); err != nil {
			return nil, err
		}
		total++
		won := winnerSide == side
		if won {
			wins++
		} else {
			losses++
		}
		outcomes = append(outcomes, domain.MatchOutcome{Won: won})
	}
	h2hRows, err := s.db.QueryContext(ctx, `
SELECT opp.player_id,
SUM(CASE WHEN m.winner_side = own.side THEN 1 ELSE 0 END) AS wins,
SUM(CASE WHEN m.winner_side != own.side THEN 1 ELSE 0 END) AS losses
FROM matches m
JOIN match_participants own ON own.match_id = m.id AND own.player_id = ?
JOIN match_participants opp ON opp.match_id = m.id AND opp.player_id != own.player_id
WHERE m.space_id = ? AND m.status = 'completed' AND m.deleted_at IS NULL
GROUP BY opp.player_id`, playerID, spaceID)
	if err != nil {
		return nil, err
	}
	defer h2hRows.Close()
	bestOpponent := ""
	worstOpponent := ""
	bestDiff := math.MinInt
	worstDiff := math.MaxInt
	for h2hRows.Next() {
		var opponent string
		var opponentWins, opponentLosses int
		if err := h2hRows.Scan(&opponent, &opponentWins, &opponentLosses); err != nil {
			return nil, err
		}
		diff := opponentWins - opponentLosses
		if diff > bestDiff {
			bestDiff = diff
			bestOpponent = opponent
		}
		if diff < worstDiff {
			worstDiff = diff
			worstOpponent = opponent
		}
	}
	return map[string]any{
		"total_matches":  total,
		"wins":           wins,
		"losses":         losses,
		"win_rate":       domain.WinRate(wins, total),
		"recent_form":    domain.RecentForm(outcomes, 10),
		"current_streak": domain.CurrentStreak(outcomes),
		"best_opponent":  bestOpponent,
		"worst_opponent": worstOpponent,
	}, nil
}

func (s *Server) computeHeadToHead(ctx context.Context, spaceID, a, b string) (map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT m.winner_side, pa.side, pb.side
FROM matches m
JOIN match_participants pa ON pa.match_id = m.id AND pa.player_id = ?
JOIN match_participants pb ON pb.match_id = m.id AND pb.player_id = ?
WHERE m.space_id = ? AND m.status = 'completed' AND m.deleted_at IS NULL
ORDER BY m.completed_at ASC`, a, b, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	total, aWins, bWins := 0, 0, 0
	var recent []string
	currentWinner := ""
	currentStreak := 0
	longestStreak := 0
	for rows.Next() {
		var winnerSide, aSide, bSide string
		if err := rows.Scan(&winnerSide, &aSide, &bSide); err != nil {
			return nil, err
		}
		total++
		winner := "b"
		if winnerSide == aSide {
			aWins++
			winner = "a"
		} else if winnerSide == bSide {
			bWins++
		}
		recent = append(recent, strings.ToUpper(winner))
		if winner == currentWinner {
			currentStreak++
		} else {
			currentWinner = winner
			currentStreak = 1
		}
		if currentStreak > longestStreak {
			longestStreak = currentStreak
		}
	}
	if len(recent) > 5 {
		recent = recent[len(recent)-5:]
	}
	return map[string]any{
		"total":          total,
		"a_wins":         aWins,
		"b_wins":         bWins,
		"recent":         recent,
		"longest_streak": longestStreak,
	}, nil
}

type sqlExecutor struct {
	db *sql.DB
	tx *sql.Tx
}

func (e sqlExecutor) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if e.tx != nil {
		return e.tx.ExecContext(ctx, query, args...)
	}
	return e.db.ExecContext(ctx, query, args...)
}

func scanUser(row interface{ Scan(dest ...any) error }) (db.User, error) {
	var user db.User
	var avatar sql.NullString
	var isSuperAdmin int
	err := row.Scan(&user.ID, &user.Email, &user.DisplayName, &avatar, &isSuperAdmin, &user.CreatedAt)
	if err != nil {
		return user, err
	}
	if avatar.Valid {
		user.AvatarURL = &avatar.String
	}
	user.IsSuperAdmin = isSuperAdmin == 1
	return user, nil
}

func scanUserlessSpace(row interface{ Scan(dest ...any) error }) (db.Space, error) {
	return scanSpace(row)
}

func scanSpace(row interface{ Scan(dest ...any) error }) (db.Space, error) {
	var space db.Space
	var desc, joinCode sql.NullString
	var inviteOnly int
	var deleted sql.NullInt64
	err := row.Scan(&space.ID, &space.Name, &desc, &inviteOnly, &joinCode, &space.CreatedBy, &space.CreatedAt, &deleted)
	if err != nil {
		return space, err
	}
	space.IsInviteOnly = inviteOnly == 1
	if desc.Valid {
		space.Description = &desc.String
	}
	if joinCode.Valid {
		space.JoinCode = &joinCode.String
	}
	if deleted.Valid {
		space.DeletedAt = &deleted.Int64
	}
	return space, nil
}

func scanSpaces(rows *sql.Rows) ([]db.Space, error) {
	var spaces []db.Space
	for rows.Next() {
		space, err := scanSpace(rows)
		if err != nil {
			return nil, err
		}
		spaces = append(spaces, space)
	}
	return spaces, nil
}

func scanPlayers(rows *sql.Rows) ([]db.Player, error) {
	var players []db.Player
	for rows.Next() {
		player, err := scanPlayer(rows)
		if err != nil {
			return nil, err
		}
		players = append(players, player)
	}
	return players, nil
}

func scanPlayer(row interface{ Scan(dest ...any) error }) (db.Player, error) {
	var player db.Player
	var userID, avatar sql.NullString
	var deleted sql.NullInt64
	err := row.Scan(&player.ID, &player.SpaceID, &player.DisplayName, &userID, &avatar, &player.CreatedAt, &deleted)
	if err != nil {
		return player, err
	}
	if userID.Valid {
		player.UserID = &userID.String
	}
	if avatar.Valid {
		player.AvatarURL = &avatar.String
	}
	if deleted.Valid {
		player.DeletedAt = &deleted.Int64
	}
	return player, nil
}

func scanMatch(row interface{ Scan(dest ...any) error }) (db.Match, error) {
	var match db.Match
	var tournamentID, phase, groupID, winner sql.NullString
	var bracketRound sql.NullInt64
	var completedAt, deleted sql.NullInt64
	err := row.Scan(&match.ID, &match.SpaceID, &match.Kind, &tournamentID, &phase, &groupID, &bracketRound, &match.BestOf, &match.PointsToWin, &match.Status, &winner, &match.StartedAt, &completedAt, &match.CreatedBy, &match.CreatedAt, &match.UpdatedAt, &deleted)
	if err != nil {
		return match, err
	}
	if tournamentID.Valid {
		match.TournamentID = &tournamentID.String
	}
	if phase.Valid {
		match.TournamentPhase = &phase.String
	}
	if groupID.Valid {
		match.TournamentGroupID = &groupID.String
	}
	if bracketRound.Valid {
		value := int(bracketRound.Int64)
		match.TournamentBracketRound = &value
	}
	if winner.Valid {
		match.WinnerSide = &winner.String
	}
	if completedAt.Valid {
		match.CompletedAt = &completedAt.Int64
	}
	if deleted.Valid {
		match.DeletedAt = &deleted.Int64
	}
	return match, nil
}

func scanMatches(rows *sql.Rows) ([]db.Match, error) {
	var matches []db.Match
	for rows.Next() {
		match, err := scanMatch(rows)
		if err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	return matches, nil
}

func scanGame(row interface{ Scan(dest ...any) error }) (db.Game, error) {
	var game db.Game
	var completed sql.NullInt64
	err := row.Scan(&game.ID, &game.MatchID, &game.GameNumber, &game.HomeScore, &game.VisitorScore, &game.Status, &game.StartedAt, &completed)
	if err != nil {
		return game, err
	}
	if completed.Valid {
		game.CompletedAt = &completed.Int64
	}
	return game, nil
}

func nowMS() int64 {
	return time.Now().UnixMilli()
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func nullableStringPtr(value *string) any {
	if value == nil {
		return nil
	}
	return nullableString(*value)
}

func nullableIntPtr(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func randomJoinCode() string {
	id := auth.NewID()
	return strings.ToLower(id[len(id)-8:])
}

func clientIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
