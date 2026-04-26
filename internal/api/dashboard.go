package api

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/samuelstrom93/pingit/internal/db"
)

type invitationPayload struct {
	db.SpaceInvitation
	InviterEmail string `json:"inviter_email"`
}

type adminDashboardPayload struct {
	PendingJoinRequests []joinRequestPayload `json:"pending_join_requests"`
	PendingInvitations  []invitationPayload  `json:"pending_invitations"`
	RecentMatches       []db.Match           `json:"recent_matches"`
	RecentTournaments   []db.Tournament      `json:"recent_tournaments"`
	Counts              map[string]int       `json:"counts"`
}

func (s *Server) handleSpaceAdminDashboard(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireAdmin(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	requests, err := s.listJoinRequests(r.Context(), spaceID, "pending")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list join requests")
		return
	}
	invitations, err := s.listInvitations(r.Context(), spaceID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list invitations")
		return
	}
	matches, err := s.recentMatches(r.Context(), spaceID, 10)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list recent matches")
		return
	}
	tournaments, err := s.recentTournaments(r.Context(), spaceID, 10)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list recent tournaments")
		return
	}
	counts, err := s.spaceCounts(r.Context(), spaceID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to compute dashboard counts")
		return
	}
	if requests == nil {
		requests = []joinRequestPayload{}
	}
	if invitations == nil {
		invitations = []invitationPayload{}
	}
	if matches == nil {
		matches = []db.Match{}
	}
	if tournaments == nil {
		tournaments = []db.Tournament{}
	}
	s.writeJSON(w, http.StatusOK, adminDashboardPayload{
		PendingJoinRequests: requests,
		PendingInvitations:  invitations,
		RecentMatches:       matches,
		RecentTournaments:   tournaments,
		Counts:              counts,
	})
}

func (s *Server) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	user := userFromContext(r.Context())
	if !user.IsSuperAdmin {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	spacesRows, err := s.db.QueryContext(r.Context(), `SELECT id, name, description, is_invite_only, join_code, created_by, created_at, deleted_at FROM spaces ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list spaces")
		return
	}
	defer spacesRows.Close()
	spaces, err := scanSpaces(spacesRows)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to decode spaces")
		return
	}
	matches, err := s.recentMatches(r.Context(), "", 25)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list matches")
		return
	}
	tournaments, err := s.recentTournaments(r.Context(), "", 25)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list tournaments")
		return
	}
	counts, err := s.globalCounts(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to compute counts")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"spaces":      spaces,
		"matches":     matches,
		"tournaments": tournaments,
		"counts":      counts,
	})
}

func (s *Server) listJoinRequests(ctx context.Context, spaceID, status string) ([]joinRequestPayload, error) {
	query := `
SELECT r.id, r.space_id, r.user_id, r.join_code, r.status, r.message, r.created_at, r.reviewed_at, r.reviewed_by,
       u.id, u.email, u.display_name, u.avatar_url, u.is_super_admin, u.created_at
FROM space_join_requests r
JOIN users u ON u.id = r.user_id
WHERE r.space_id = ?`
	args := []any{spaceID}
	if status != "" {
		query += ` AND r.status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY r.created_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var requests []joinRequestPayload
	for rows.Next() {
		var req db.SpaceJoinRequest
		var msg sql.NullString
		var reviewedAt sql.NullInt64
		var reviewedBy sql.NullString
		var u db.User
		var avatar sql.NullString
		var isSuperAdmin int
		if err := rows.Scan(&req.ID, &req.SpaceID, &req.UserID, &req.JoinCode, &req.Status, &msg, &req.CreatedAt, &reviewedAt, &reviewedBy, &u.ID, &u.Email, &u.DisplayName, &avatar, &isSuperAdmin, &u.CreatedAt); err != nil {
			return nil, err
		}
		if msg.Valid {
			req.Message = &msg.String
		}
		if reviewedAt.Valid {
			req.ReviewedAt = &reviewedAt.Int64
		}
		if reviewedBy.Valid {
			req.ReviewedBy = &reviewedBy.String
		}
		if avatar.Valid {
			u.AvatarURL = &avatar.String
		}
		u.IsSuperAdmin = isSuperAdmin == 1
		requests = append(requests, joinRequestPayload{SpaceJoinRequest: req, User: u})
	}
	return requests, rows.Err()
}

func (s *Server) listInvitations(ctx context.Context, spaceID string) ([]invitationPayload, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT i.id, i.space_id, i.email, i.token, i.invited_by, i.status, i.created_at, i.accepted_at, u.email
FROM space_invitations i
JOIN users u ON u.id = i.invited_by
WHERE i.space_id = ?
ORDER BY i.created_at DESC`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var invitations []invitationPayload
	for rows.Next() {
		var inv db.SpaceInvitation
		var accepted sql.NullInt64
		var inviterEmail string
		if err := rows.Scan(&inv.ID, &inv.SpaceID, &inv.Email, &inv.Token, &inv.InvitedBy, &inv.Status, &inv.CreatedAt, &accepted, &inviterEmail); err != nil {
			return nil, err
		}
		if accepted.Valid {
			inv.AcceptedAt = &accepted.Int64
		}
		invitations = append(invitations, invitationPayload{SpaceInvitation: inv, InviterEmail: inviterEmail})
	}
	return invitations, rows.Err()
}

func (s *Server) recentMatches(ctx context.Context, spaceID string, limit int) ([]db.Match, error) {
	query := `SELECT id, space_id, kind, tournament_id, tournament_phase, tournament_group_id, tournament_bracket_round, best_of, points_to_win, status, winner_side, started_at, completed_at, created_by, created_at, updated_at, deleted_at FROM matches WHERE deleted_at IS NULL`
	args := []any{}
	if spaceID != "" {
		query += ` AND space_id = ?`
		args = append(args, spaceID)
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMatches(rows)
}

func (s *Server) recentTournaments(ctx context.Context, spaceID string, limit int) ([]db.Tournament, error) {
	query := `SELECT id, space_id, name, format, best_of, points_to_win, status, winner_player_id, created_by, created_at, updated_at, deleted_at FROM tournaments WHERE deleted_at IS NULL`
	args := []any{}
	if spaceID != "" {
		query += ` AND space_id = ?`
		args = append(args, spaceID)
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tournaments []db.Tournament
	for rows.Next() {
		var t db.Tournament
		var winner sql.NullString
		var deleted sql.NullInt64
		if err := rows.Scan(&t.ID, &t.SpaceID, &t.Name, &t.Format, &t.BestOf, &t.PointsToWin, &t.Status, &winner, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &deleted); err != nil {
			return nil, err
		}
		if winner.Valid {
			t.WinnerPlayerID = &winner.String
		}
		if deleted.Valid {
			t.DeletedAt = &deleted.Int64
		}
		tournaments = append(tournaments, t)
	}
	return tournaments, rows.Err()
}

func (s *Server) spaceCounts(ctx context.Context, spaceID string) (map[string]int, error) {
	keys := map[string]string{
		"members":               "SELECT COUNT(*) FROM space_members WHERE space_id = ?",
		"players":               "SELECT COUNT(*) FROM players WHERE space_id = ? AND deleted_at IS NULL",
		"matches":               "SELECT COUNT(*) FROM matches WHERE space_id = ? AND deleted_at IS NULL",
		"tournaments":           "SELECT COUNT(*) FROM tournaments WHERE space_id = ? AND deleted_at IS NULL",
		"pending_join_requests": "SELECT COUNT(*) FROM space_join_requests WHERE space_id = ? AND status = 'pending'",
		"pending_invitations":   "SELECT COUNT(*) FROM space_invitations WHERE space_id = ? AND status = 'pending'",
	}
	out := map[string]int{}
	for key, query := range keys {
		var count int
		if err := s.db.QueryRowContext(ctx, query, spaceID).Scan(&count); err != nil {
			return nil, err
		}
		out[key] = count
	}
	return out, nil
}

func (s *Server) globalCounts(ctx context.Context) (map[string]int, error) {
	keys := map[string]string{
		"spaces":                "SELECT COUNT(*) FROM spaces WHERE deleted_at IS NULL",
		"users":                 "SELECT COUNT(*) FROM users",
		"matches":               "SELECT COUNT(*) FROM matches WHERE deleted_at IS NULL",
		"tournaments":           "SELECT COUNT(*) FROM tournaments WHERE deleted_at IS NULL",
		"pending_join_requests": "SELECT COUNT(*) FROM space_join_requests WHERE status = 'pending'",
	}
	out := map[string]int{}
	for key, query := range keys {
		var count int
		if err := s.db.QueryRowContext(ctx, query).Scan(&count); err != nil {
			return nil, err
		}
		out[key] = count
	}
	return out, nil
}
