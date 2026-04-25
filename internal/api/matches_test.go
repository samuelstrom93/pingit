package api

import (
	"net/http"
	"testing"
)

func setupSpaceWithPlayers(t *testing.T, h *testHarness, addr string) (string, []string) {
	t.Helper()
	cookie := h.login(t, addr)

	resp, body := h.request(http.MethodPost, "/api/spaces", cookie, map[string]any{"name": "Testspace"})
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create space want 200/201, got %d (body=%s)", resp.StatusCode, body)
	}
	space := decode[map[string]any](t, body)
	spaceID, _ := space["id"].(string)
	if spaceID == "" {
		t.Fatalf("no space id: %v", space)
	}

	playerIDs := []string{}
	for _, name := range []string{"Anna", "Bertil", "Cecilia", "David"} {
		resp, body = h.request(http.MethodPost, "/api/spaces/"+spaceID+"/players", cookie, map[string]any{"display_name": name})
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
			t.Fatalf("create player %s: %d (%s)", name, resp.StatusCode, body)
		}
		p := decode[map[string]any](t, body)
		id, _ := p["id"].(string)
		playerIDs = append(playerIDs, id)
	}
	return cookie + "|" + spaceID, playerIDs
}

func TestSinglesMatchScoringFlow(t *testing.T) {
	h := newHarness(t)
	combo, players := setupSpaceWithPlayers(t, h, "alice@example.com")
	cookie := combo[:26]
	spaceID := combo[27:]

	// Create singles match: best_of=1, points=5
	resp, body := h.request(http.MethodPost, "/api/spaces/"+spaceID+"/matches", cookie, map[string]any{
		"kind":            "singles",
		"home_players":    []string{players[0]},
		"visitor_players": []string{players[1]},
		"best_of":         1,
		"points_to_win":   5,
	})
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create match: %d (%s)", resp.StatusCode, body)
	}
	created := decode[map[string]any](t, body)
	matchID := pickID(created)
	if matchID == "" {
		t.Fatalf("no match id: %v", created)
	}

	// Score 5 home points → match completes
	for i := 0; i < 5; i++ {
		resp, body = h.request(http.MethodPost, "/api/matches/"+matchID+"/score", cookie, map[string]any{"side": "home"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("score #%d: %d (%s)", i+1, resp.StatusCode, body)
		}
	}

	// Verify match completed
	resp, body = h.request(http.MethodGet, "/api/matches/"+matchID, cookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get match: %d", resp.StatusCode)
	}
	final := decode[map[string]any](t, body)
	m, _ := final["match"].(map[string]any)
	if m == nil {
		m = final
	}
	if m["status"] != "completed" {
		t.Fatalf("want completed, got %v", m["status"])
	}
	if m["winner_side"] != "home" {
		t.Fatalf("want winner home, got %v", m["winner_side"])
	}

	participants, ok := final["participants"].([]any)
	if !ok || len(participants) != 2 {
		t.Fatalf("expected 2 participants, got %v", final["participants"])
	}
	first, _ := participants[0].(map[string]any)
	player, _ := first["player"].(map[string]any)
	if player["display_name"] == "" {
		t.Fatalf("expected embedded player payload, got %v", first)
	}
}

func TestEditAfterCompleteWritesAuditRow(t *testing.T) {
	h := newHarness(t)
	combo, players := setupSpaceWithPlayers(t, h, "alice@example.com")
	cookie := combo[:26]
	spaceID := combo[27:]

	resp, body := h.request(http.MethodPost, "/api/spaces/"+spaceID+"/matches", cookie, map[string]any{
		"kind":            "singles",
		"home_players":    []string{players[0]},
		"visitor_players": []string{players[1]},
		"best_of":         1,
		"points_to_win":   5,
	})
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create match: %d (%s)", resp.StatusCode, body)
	}
	matchID := pickID(decode[map[string]any](t, body))
	for i := 0; i < 5; i++ {
		h.request(http.MethodPost, "/api/matches/"+matchID+"/score", cookie, map[string]any{"side": "home"})
	}

	// Edit the completed match
	resp, body = h.request(http.MethodPatch, "/api/matches/"+matchID, cookie, map[string]any{
		"games": []map[string]any{{"game_number": 1, "home_score": 5, "visitor_score": 3}},
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("edit match: %d (%s)", resp.StatusCode, body)
	}

	// Verify match_edits row exists
	var count int
	if err := h.conn.QueryRow("SELECT COUNT(*) FROM match_edits WHERE match_id = ?", matchID).Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count < 1 {
		t.Fatalf("expected match_edits row, got %d", count)
	}
}

func TestUnauthenticatedScoreIsForbidden(t *testing.T) {
	h := newHarness(t)
	resp, _ := h.request(http.MethodPost, "/api/matches/any/score", "", map[string]string{"side": "home"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
}

func pickID(m map[string]any) string {
	if inner, ok := m["match"].(map[string]any); ok {
		if id, ok := inner["id"].(string); ok {
			return id
		}
	}
	if id, ok := m["id"].(string); ok {
		return id
	}
	return ""
}
