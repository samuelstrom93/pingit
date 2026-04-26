package api

import (
	"context"
	"database/sql"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/samuelstrom93/pingit/internal/auth"
	"github.com/samuelstrom93/pingit/internal/db"
)

type createJoinRequestRequest struct {
	Message string `json:"message"`
}

type reviewJoinRequestRequest struct {
	Status string `json:"status" validate:"required,oneof=accepted rejected"`
}

type joinRequestPayload struct {
	db.SpaceJoinRequest
	User db.User `json:"user"`
}

func (s *Server) handleCreateJoinRequest(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(chi.URLParam(r, "code"))
	user := userFromContext(r.Context())
	var req createJoinRequestRequest
	if r.Body != nil && r.ContentLength != 0 {
		if !s.decodeJSON(w, r, &req) {
			return
		}
	}

	var spaceID string
	if err := s.db.QueryRowContext(r.Context(), `SELECT id FROM spaces WHERE join_code = ? AND deleted_at IS NULL`, code).Scan(&spaceID); err != nil {
		s.writeError(w, http.StatusNotFound, "join code not found")
		return
	}
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err == nil {
		s.writeError(w, http.StatusConflict, "already a member")
		return
	}

	now := nowMS()
	id := auth.NewID()
	_, err := s.db.ExecContext(r.Context(), `
INSERT INTO space_join_requests (id, space_id, user_id, join_code, status, message, created_at)
VALUES (?, ?, ?, ?, 'pending', ?, ?)
ON CONFLICT(space_id, user_id) DO UPDATE SET
  join_code = excluded.join_code,
  status = 'pending',
  message = excluded.message,
  created_at = excluded.created_at,
  reviewed_at = NULL,
  reviewed_by = NULL`, id, spaceID, user.ID, code, nullableString(req.Message), now)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create join request")
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{"space_id": spaceID, "status": "pending"})
}

func (s *Server) handleListJoinRequests(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireAdmin(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	requests, err := s.listJoinRequests(r.Context(), spaceID, r.URL.Query().Get("status"))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list join requests")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"join_requests": requests})
}

func (s *Server) handleReviewJoinRequest(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	requestID := chi.URLParam(r, "requestID")
	user := userFromContext(r.Context())
	if err := s.requireAdmin(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req reviewJoinRequestRequest
	if !s.decodeAndValidate(w, r, &req) {
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()

	var targetUserID, status string
	if err := tx.QueryRowContext(r.Context(), `SELECT user_id, status FROM space_join_requests WHERE id = ? AND space_id = ?`, requestID, spaceID).Scan(&targetUserID, &status); err != nil {
		s.writeError(w, http.StatusNotFound, "join request not found")
		return
	}
	if status != "pending" {
		s.writeError(w, http.StatusBadRequest, "join request already reviewed")
		return
	}
	now := nowMS()
	if _, err := tx.ExecContext(r.Context(), `UPDATE space_join_requests SET status = ?, reviewed_at = ?, reviewed_by = ? WHERE id = ?`, req.Status, now, user.ID, requestID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to review join request")
		return
	}
	if req.Status == "accepted" {
		if _, err := tx.ExecContext(r.Context(), `INSERT OR IGNORE INTO space_members (space_id, user_id, role, joined_at) VALUES (?, ?, 'member', ?)`, spaceID, targetUserID, now); err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to add member")
			return
		}
		target, err := s.userByIDTx(r.Context(), tx, targetUserID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to load user")
			return
		}
		if err := s.ensurePlayerTx(r.Context(), tx, spaceID, target); err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to create player")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit join request")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListInvitations(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireAdmin(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	invitations, err := s.listInvitations(r.Context(), spaceID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list invitations")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"invitations": invitations})
}

func (s *Server) userByIDTx(ctx context.Context, tx *sql.Tx, userID string) (db.User, error) {
	// kept separate so accept-flow can create a linked player inside the same transaction
	row := tx.QueryRowContext(ctx, `SELECT id, email, display_name, avatar_url, is_super_admin, created_at FROM users WHERE id = ?`, userID)
	return scanUser(row)
}
