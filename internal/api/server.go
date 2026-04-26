package api

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/validator/v10"

	"github.com/samuelstrom93/pingit/internal/auth"
	"github.com/samuelstrom93/pingit/internal/config"
	"github.com/samuelstrom93/pingit/internal/db"
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
				spaces.Post("/join/{code}/request", s.handleCreateJoinRequest)
				spaces.Get("/{spaceID}", s.handleGetSpace)
				spaces.Patch("/{spaceID}", s.handleUpdateSpace)
				spaces.Delete("/{spaceID}", s.handleDeleteSpace)
				spaces.Get("/{spaceID}/members", s.handleListMembers)
				spaces.Delete("/{spaceID}/members/{userID}", s.handleDeleteMember)
				spaces.Post("/{spaceID}/invitations", s.handleCreateInvitation)
				spaces.Get("/{spaceID}/invitations", s.handleListInvitations)
				spaces.Get("/{spaceID}/join-requests", s.handleListJoinRequests)
				spaces.Patch("/{spaceID}/join-requests/{requestID}", s.handleReviewJoinRequest)
				spaces.Get("/{spaceID}/admin/dashboard", s.handleSpaceAdminDashboard)
				spaces.Get("/{spaceID}/players", s.handleListPlayers)
				spaces.Post("/{spaceID}/players", s.handleCreatePlayer)
				spaces.Post("/{spaceID}/matches", s.handleCreateMatch)
				spaces.Get("/{spaceID}/matches", s.handleListMatches)
				spaces.Post("/{spaceID}/tournaments", s.handleCreateTournament)
				spaces.Get("/{spaceID}/tournaments", s.handleListTournaments)
				spaces.Get("/{spaceID}/stats", s.handleSpaceStats)
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
			priv.Get("/admin/overview", s.handleAdminOverview)
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
