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
