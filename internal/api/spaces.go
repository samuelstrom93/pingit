package api

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/samuelstrom93/pingit/internal/auth"
	"github.com/samuelstrom93/pingit/internal/db"
)

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
