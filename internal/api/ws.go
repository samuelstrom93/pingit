package api

import (
	"net/http"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"
)

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
