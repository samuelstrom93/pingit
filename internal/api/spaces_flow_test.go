package api

import (
	"net/http"
	"testing"
)

func TestJoinRequestAcceptFlow(t *testing.T) {
	h := newHarness(t)
	combo, _ := setupSpaceWithPlayers(t, h, "admin@example.com")
	adminCookie := combo[:26]
	spaceID := combo[27:]

	var joinCode string
	if err := h.conn.QueryRow(`SELECT join_code FROM spaces WHERE id = ?`, spaceID).Scan(&joinCode); err != nil {
		t.Fatalf("join code: %v", err)
	}
	playerCookie := h.login(t, "player@example.com")

	resp, body := h.request(http.MethodPost, "/api/spaces/join/"+joinCode, playerCookie, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("direct join should be forbidden for invite-only space, got %d (%s)", resp.StatusCode, body)
	}

	resp, body = h.request(http.MethodPost, "/api/spaces/join/"+joinCode+"/request", playerCookie, map[string]string{"message": "let me in"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("request join: %d (%s)", resp.StatusCode, body)
	}

	resp, body = h.request(http.MethodGet, "/api/spaces/"+spaceID+"/join-requests?status=pending", adminCookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list requests: %d (%s)", resp.StatusCode, body)
	}
	listed := decode[map[string]any](t, body)
	requests, _ := listed["join_requests"].([]any)
	if len(requests) != 1 {
		t.Fatalf("expected one pending request, got %v", listed)
	}
	requestID := requests[0].(map[string]any)["id"].(string)

	resp, body = h.request(http.MethodPatch, "/api/spaces/"+spaceID+"/join-requests/"+requestID, adminCookie, map[string]string{"status": "accepted"})
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("accept request: %d (%s)", resp.StatusCode, body)
	}

	resp, body = h.request(http.MethodGet, "/api/spaces/"+spaceID, playerCookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("accepted player should access space, got %d (%s)", resp.StatusCode, body)
	}

	var memberCount, playerCount int
	if err := h.conn.QueryRow(`SELECT COUNT(*) FROM space_members WHERE space_id = ? AND user_id = (SELECT id FROM users WHERE email = 'player@example.com')`, spaceID).Scan(&memberCount); err != nil {
		t.Fatalf("member count: %v", err)
	}
	if err := h.conn.QueryRow(`SELECT COUNT(*) FROM players WHERE space_id = ? AND user_id = (SELECT id FROM users WHERE email = 'player@example.com')`, spaceID).Scan(&playerCount); err != nil {
		t.Fatalf("player count: %v", err)
	}
	if memberCount != 1 || playerCount != 1 {
		t.Fatalf("expected member+player rows, got members=%d players=%d", memberCount, playerCount)
	}
}

func TestSpaceAdminDashboard(t *testing.T) {
	h := newHarness(t)
	combo, _ := setupSpaceWithPlayers(t, h, "admin@example.com")
	adminCookie := combo[:26]
	spaceID := combo[27:]

	resp, body := h.request(http.MethodGet, "/api/spaces/"+spaceID+"/admin/dashboard", adminCookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard: %d (%s)", resp.StatusCode, body)
	}
	dashboard := decode[map[string]any](t, body)
	if dashboard["counts"] == nil || dashboard["recent_matches"] == nil || dashboard["pending_join_requests"] == nil {
		t.Fatalf("dashboard missing expected sections: %v", dashboard)
	}
}

func TestDoublesMatchRequiresTwoDistinctPlayersPerSide(t *testing.T) {
	h := newHarness(t)
	combo, players := setupSpaceWithPlayers(t, h, "admin@example.com")
	cookie := combo[:26]
	spaceID := combo[27:]

	resp, body := h.request(http.MethodPost, "/api/spaces/"+spaceID+"/matches", cookie, map[string]any{
		"kind":            "doubles",
		"home_players":    []string{players[0], players[1]},
		"visitor_players": []string{players[2], players[3]},
		"best_of":         1,
		"points_to_win":   5,
	})
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		t.Fatalf("create doubles: %d (%s)", resp.StatusCode, body)
	}
	created := decode[map[string]any](t, body)
	matchID := pickID(created)
	resp, body = h.request(http.MethodGet, "/api/matches/"+matchID, cookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get doubles: %d (%s)", resp.StatusCode, body)
	}
	detail := decode[map[string]any](t, body)
	participants, _ := detail["participants"].([]any)
	if len(participants) != 4 {
		t.Fatalf("expected 4 doubles participants, got %v", detail["participants"])
	}

	resp, _ = h.request(http.MethodPost, "/api/spaces/"+spaceID+"/matches", cookie, map[string]any{
		"kind":            "doubles",
		"home_players":    []string{players[0], players[0]},
		"visitor_players": []string{players[2], players[3]},
		"best_of":         1,
		"points_to_win":   5,
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("duplicate doubles player should be rejected, got %d", resp.StatusCode)
	}
}
