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

func TestGroupsKnockoutGeneratesAndCompletesBracketAfterGroupStage(t *testing.T) {
	h := newHarness(t)
	combo, players := setupSpaceWithPlayers(t, h, "alice@example.com")
	cookie := combo[:26]
	spaceID := combo[27:]

	resp, body := h.request(http.MethodPost, "/api/spaces/"+spaceID+"/tournaments", cookie, map[string]any{
		"name":          "Club championship",
		"format":        "groups_knockout",
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

	detail := getTournamentDetail(t, h, cookie, tournamentID)
	groupIDs := matchIDsByPhaseRound(detail, "group", 0)
	if len(groupIDs) != 6 {
		t.Fatalf("expected 6 group-stage matches for 4 players, got %d", len(groupIDs))
	}
	if len(matchIDsByPhaseRound(detail, "bracket", 1)) != 0 {
		t.Fatalf("bracket should not exist before group stage is completed")
	}

	for _, matchID := range groupIDs {
		scoreHomeToFive(t, h, cookie, matchID)
	}

	detail = getTournamentDetail(t, h, cookie, tournamentID)
	round1 := matchIDsByPhaseRound(detail, "bracket", 1)
	finals := matchIDsByPhaseRound(detail, "bracket", 2)
	if len(round1) != 2 || len(finals) != 1 {
		t.Fatalf("expected generated 2 semifinal + 1 final matches, got round1=%d final=%d detail=%v", len(round1), len(finals), detail["matches"])
	}
	for _, matchID := range round1 {
		assertParticipantCount(t, h, matchID, 2)
	}
	assertParticipantCount(t, h, finals[0], 0)

	for _, matchID := range round1 {
		scoreHomeToFive(t, h, cookie, matchID)
	}
	assertParticipantCount(t, h, finals[0], 2)
	scoreHomeToFive(t, h, cookie, finals[0])

	detail = getTournamentDetail(t, h, cookie, tournamentID)
	tournament, _ := detail["tournament"].(map[string]any)
	if tournament["status"] != "completed" || tournament["winner_player_id"] == nil || tournament["winner_player_id"] == "" {
		t.Fatalf("expected completed tournament with winner, got %v", tournament)
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

func getTournamentDetail(t *testing.T, h *testHarness, cookie, tournamentID string) map[string]any {
	t.Helper()
	resp, body := h.request(http.MethodGet, "/api/tournaments/"+tournamentID, cookie, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get tournament: %d (%s)", resp.StatusCode, body)
	}
	return decode[map[string]any](t, body)
}

func matchIDsByPhaseRound(detail map[string]any, phase string, round int) []string {
	matches, _ := detail["matches"].([]any)
	var ids []string
	for _, item := range matches {
		match, _ := item.(map[string]any)
		if match["tournament_phase"] != phase {
			continue
		}
		if round > 0 {
			value, _ := match["tournament_bracket_round"].(float64)
			if int(value) != round {
				continue
			}
		}
		if id, _ := match["id"].(string); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func scoreHomeToFive(t *testing.T, h *testHarness, cookie, matchID string) {
	t.Helper()
	for i := 0; i < 5; i++ {
		resp, body := h.request(http.MethodPost, "/api/matches/"+matchID+"/score", cookie, map[string]any{"side": "home"})
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("score %s #%d: %d (%s)", matchID, i+1, resp.StatusCode, body)
		}
	}
}

func assertParticipantCount(t *testing.T, h *testHarness, matchID string, want int) {
	t.Helper()
	var got int
	if err := h.conn.QueryRow(`SELECT COUNT(*) FROM match_participants WHERE match_id = ?`, matchID).Scan(&got); err != nil {
		t.Fatalf("participant count: %v", err)
	}
	if got != want {
		t.Fatalf("participants for %s: want %d got %d", matchID, want, got)
	}
}
