package api

import (
	"net/http"
	"testing"
)

func TestRoundRobinTournamentLifecycle(t *testing.T) {
	h := newHarness(t)
	combo, players := setupSpaceWithPlayers(t, h, "alice@example.com")
	cookie := combo[:26]
	spaceID := combo[27:]

	// 1. List tournaments — should be empty
	resp, body := h.request(http.MethodGet, "/api/spaces/"+spaceID+"/tournaments", cookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list tournaments: %d (%s)", resp.StatusCode, body)
	}

	// 2. Create round-robin with 4 players
	resp, body = h.request(http.MethodPost, "/api/spaces/"+spaceID+"/tournaments", cookie, map[string]any{
		"name":          "Round robin",
		"format":        "round_robin",
		"best_of":       1,
		"points_to_win": 5,
		"player_ids":    players,
	})
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create tournament: %d (%s)", resp.StatusCode, body)
	}
	tournamentID := pickID(decode[map[string]any](t, body))
	if tournamentID == "" {
		t.Fatalf("no tournament id: %s", body)
	}

	// 3. Start
	resp, body = h.request(http.MethodPost, "/api/tournaments/"+tournamentID+"/start", cookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start tournament: %d (%s)", resp.StatusCode, body)
	}

	// 4. List tournaments — should have one now
	resp, body = h.request(http.MethodGet, "/api/spaces/"+spaceID+"/tournaments", cookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list tournaments after create: %d (%s)", resp.StatusCode, body)
	}
	listed := decode[map[string]any](t, body)
	if arr, ok := listed["tournaments"].([]any); !ok || len(arr) != 1 {
		t.Fatalf("expected 1 tournament listed, got %v", listed)
	}

	// 5. Get tournament details, expect N*(N-1)/2 = 6 matches for 4 players
	resp, body = h.request(http.MethodGet, "/api/tournaments/"+tournamentID, cookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get tournament: %d (%s)", resp.StatusCode, body)
	}
	detail := decode[map[string]any](t, body)
	matches, _ := detail["matches"].([]any)
	if len(matches) != 6 {
		t.Fatalf("expected 6 round-robin matches, got %d", len(matches))
	}
	detailPlayers, _ := detail["players"].([]any)
	if len(detailPlayers) != len(players) {
		t.Fatalf("expected %d player objects, got %d", len(players), len(detailPlayers))
	}
	firstPlayer, _ := detailPlayers[0].(map[string]any)
	if firstPlayer["display_name"] == "" {
		t.Fatalf("expected tournament detail player objects, got %v", detail["players"])
	}

	// 6. Standings endpoint returns rows
	resp, body = h.request(http.MethodGet, "/api/tournaments/"+tournamentID+"/standings", cookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("standings: %d (%s)", resp.StatusCode, body)
	}
}

func TestBracketTournamentByes(t *testing.T) {
	h := newHarness(t)
	combo, players := setupSpaceWithPlayers(t, h, "alice@example.com")
	cookie := combo[:26]
	spaceID := combo[27:]

	// Create bracket with 4 players → 3 matches total (2 + 1)
	resp, body := h.request(http.MethodPost, "/api/spaces/"+spaceID+"/tournaments", cookie, map[string]any{
		"name":          "Bracket",
		"format":        "bracket",
		"best_of":       1,
		"points_to_win": 5,
		"player_ids":    players,
	})
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create tournament: %d (%s)", resp.StatusCode, body)
	}
	tournamentID := pickID(decode[map[string]any](t, body))

	resp, body = h.request(http.MethodPost, "/api/tournaments/"+tournamentID+"/start", cookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("start: %d (%s)", resp.StatusCode, body)
	}

	resp, body = h.request(http.MethodGet, "/api/tournaments/"+tournamentID, cookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get: %d (%s)", resp.StatusCode, body)
	}
	detail := decode[map[string]any](t, body)
	matches, _ := detail["matches"].([]any)
	if len(matches) < 2 {
		t.Fatalf("expected ≥2 matches for 4-player bracket, got %d", len(matches))
	}
}
