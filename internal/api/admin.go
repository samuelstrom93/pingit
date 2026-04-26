package api

import (
	"net/http"
)

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
