package api

import (
	"context"
	"math"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/samuelstrom93/pingit/internal/domain"
)

func (s *Server) handleSpaceStats(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	stats, err := s.computeSpaceStats(r.Context(), spaceID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to compute stats")
		return
	}
	s.writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handlePlayerStats(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	playerID := chi.URLParam(r, "playerID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	stats, err := s.computePlayerStats(r.Context(), spaceID, playerID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to compute stats")
		return
	}
	s.writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleHeadToHead(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	a := r.URL.Query().Get("a")
	b := r.URL.Query().Get("b")
	stats, err := s.computeHeadToHead(r.Context(), spaceID, a, b)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to compute h2h")
		return
	}
	s.writeJSON(w, http.StatusOK, stats)
}

func (s *Server) computePlayerStats(ctx context.Context, spaceID, playerID string) (map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT m.id, m.winner_side, mp.side
FROM matches m
JOIN match_participants mp ON mp.match_id = m.id
WHERE m.space_id = ? AND m.status = 'completed' AND mp.player_id = ? AND m.deleted_at IS NULL
ORDER BY m.completed_at ASC`, spaceID, playerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var total, wins, losses int
	var outcomes []domain.MatchOutcome
	for rows.Next() {
		var matchID string
		var winnerSide, side string
		if err := rows.Scan(&matchID, &winnerSide, &side); err != nil {
			return nil, err
		}
		total++
		won := winnerSide == side
		if won {
			wins++
		} else {
			losses++
		}
		outcomes = append(outcomes, domain.MatchOutcome{Won: won})
	}
	h2hRows, err := s.db.QueryContext(ctx, `
SELECT opp.player_id,
SUM(CASE WHEN m.winner_side = own.side THEN 1 ELSE 0 END) AS wins,
SUM(CASE WHEN m.winner_side != own.side THEN 1 ELSE 0 END) AS losses
FROM matches m
JOIN match_participants own ON own.match_id = m.id AND own.player_id = ?
JOIN match_participants opp ON opp.match_id = m.id AND opp.player_id != own.player_id
WHERE m.space_id = ? AND m.status = 'completed' AND m.deleted_at IS NULL
GROUP BY opp.player_id`, playerID, spaceID)
	if err != nil {
		return nil, err
	}
	defer h2hRows.Close()
	bestOpponent := ""
	worstOpponent := ""
	bestDiff := math.MinInt
	worstDiff := math.MaxInt
	for h2hRows.Next() {
		var opponent string
		var opponentWins, opponentLosses int
		if err := h2hRows.Scan(&opponent, &opponentWins, &opponentLosses); err != nil {
			return nil, err
		}
		diff := opponentWins - opponentLosses
		if diff > bestDiff {
			bestDiff = diff
			bestOpponent = opponent
		}
		if diff < worstDiff {
			worstDiff = diff
			worstOpponent = opponent
		}
	}
	return map[string]any{
		"total_matches":  total,
		"wins":           wins,
		"losses":         losses,
		"win_rate":       domain.WinRate(wins, total),
		"recent_form":    domain.RecentForm(outcomes, 10),
		"current_streak": domain.CurrentStreak(outcomes),
		"best_opponent":  bestOpponent,
		"worst_opponent": worstOpponent,
	}, nil
}

type spacePlayerStats struct {
	PlayerID        string         `json:"player_id"`
	DisplayName     string         `json:"display_name"`
	Matches         int            `json:"matches"`
	Wins            int            `json:"wins"`
	Losses          int            `json:"losses"`
	WinRate         float64        `json:"win_rate"`
	PointsFor       int            `json:"points_for"`
	PointsAgainst   int            `json:"points_against"`
	AveragePoints   float64        `json:"average_points"`
	BreakdownByKind map[string]any `json:"breakdown_by_kind"`
	BreakdownBySet  map[string]any `json:"breakdown_by_set"`
}

type statsBucket struct {
	Matches       int `json:"matches"`
	Wins          int `json:"wins"`
	Losses        int `json:"losses"`
	PointsFor     int `json:"points_for"`
	PointsAgainst int `json:"points_against"`
}

func (s *Server) computeSpaceStats(ctx context.Context, spaceID string) (map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT p.id, p.display_name, m.kind, m.points_to_win, m.winner_side, mp.side,
       COALESCE(SUM(CASE WHEN mp.side = 'home' THEN g.home_score ELSE g.visitor_score END), 0) AS points_for,
       COALESCE(SUM(CASE WHEN mp.side = 'home' THEN g.visitor_score ELSE g.home_score END), 0) AS points_against
FROM players p
JOIN match_participants mp ON mp.player_id = p.id
JOIN matches m ON m.id = mp.match_id
LEFT JOIN games g ON g.match_id = m.id AND g.status = 'completed'
WHERE p.space_id = ? AND p.deleted_at IS NULL AND m.status = 'completed' AND m.deleted_at IS NULL
GROUP BY p.id, p.display_name, m.id, m.kind, m.points_to_win, m.winner_side, mp.side
ORDER BY p.display_name`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	players := map[string]*spacePlayerStats{}
	order := []string{}
	kindTotals := map[string]*statsBucket{}
	setTotals := map[string]*statsBucket{}
	for rows.Next() {
		var playerID, displayName, kind, winnerSide, side string
		var pointsToWin, pointsFor, pointsAgainst int
		if err := rows.Scan(&playerID, &displayName, &kind, &pointsToWin, &winnerSide, &side, &pointsFor, &pointsAgainst); err != nil {
			return nil, err
		}
		p := players[playerID]
		if p == nil {
			p = &spacePlayerStats{PlayerID: playerID, DisplayName: displayName, BreakdownByKind: map[string]any{}, BreakdownBySet: map[string]any{}}
			players[playerID] = p
			order = append(order, playerID)
		}
		won := winnerSide == side
		p.Matches++
		if won {
			p.Wins++
		} else {
			p.Losses++
		}
		p.PointsFor += pointsFor
		p.PointsAgainst += pointsAgainst

		addStatsBucket(playerBucket(p.BreakdownByKind, kind), won, pointsFor, pointsAgainst)
		setKey := setType(pointsToWin)
		addStatsBucket(playerBucket(p.BreakdownBySet, setKey), won, pointsFor, pointsAgainst)
		addStatsBucket(globalStatsBucket(kindTotals, kind), won, pointsFor, pointsAgainst)
		addStatsBucket(globalStatsBucket(setTotals, setKey), won, pointsFor, pointsAgainst)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := make([]spacePlayerStats, 0, len(order))
	for _, id := range order {
		p := players[id]
		p.WinRate = domain.WinRate(p.Wins, p.Matches)
		if p.Matches > 0 {
			p.AveragePoints = float64(p.PointsFor) / float64(p.Matches)
		}
		out = append(out, *p)
	}
	return map[string]any{
		"players":           out,
		"breakdown_by_kind": statsBucketMap(kindTotals),
		"breakdown_by_set":  statsBucketMap(setTotals),
	}, nil
}

func setType(pointsToWin int) string {
	if pointsToWin == 5 {
		return "short_5"
	}
	return "standard_11"
}

func playerBucket(target map[string]any, key string) *statsBucket {
	if existing, ok := target[key].(*statsBucket); ok {
		return existing
	}
	b := &statsBucket{}
	target[key] = b
	return b
}

func globalStatsBucket(target map[string]*statsBucket, key string) *statsBucket {
	if target[key] == nil {
		target[key] = &statsBucket{}
	}
	return target[key]
}

func addStatsBucket(b *statsBucket, won bool, pointsFor, pointsAgainst int) {
	b.Matches++
	if won {
		b.Wins++
	} else {
		b.Losses++
	}
	b.PointsFor += pointsFor
	b.PointsAgainst += pointsAgainst
}

func statsBucketMap(input map[string]*statsBucket) map[string]any {
	out := map[string]any{}
	for key, b := range input {
		avg := 0.0
		if b.Matches > 0 {
			avg = float64(b.PointsFor) / float64(b.Matches)
		}
		out[key] = map[string]any{"matches": b.Matches, "wins": b.Wins, "losses": b.Losses, "points_for": b.PointsFor, "points_against": b.PointsAgainst, "average_points": avg}
	}
	return out
}

func (s *Server) computeHeadToHead(ctx context.Context, spaceID, a, b string) (map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT m.winner_side, pa.side, pb.side
FROM matches m
JOIN match_participants pa ON pa.match_id = m.id AND pa.player_id = ?
JOIN match_participants pb ON pb.match_id = m.id AND pb.player_id = ?
WHERE m.space_id = ? AND m.status = 'completed' AND m.deleted_at IS NULL
ORDER BY m.completed_at ASC`, a, b, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	total, aWins, bWins := 0, 0, 0
	var recent []string
	currentWinner := ""
	currentStreak := 0
	longestStreak := 0
	for rows.Next() {
		var winnerSide, aSide, bSide string
		if err := rows.Scan(&winnerSide, &aSide, &bSide); err != nil {
			return nil, err
		}
		total++
		winner := "b"
		if winnerSide == aSide {
			aWins++
			winner = "a"
		} else if winnerSide == bSide {
			bWins++
		}
		recent = append(recent, strings.ToUpper(winner))
		if winner == currentWinner {
			currentStreak++
		} else {
			currentWinner = winner
			currentStreak = 1
		}
		if currentStreak > longestStreak {
			longestStreak = currentStreak
		}
	}
	if len(recent) > 5 {
		recent = recent[len(recent)-5:]
	}
	return map[string]any{
		"total":          total,
		"a_wins":         aWins,
		"b_wins":         bWins,
		"recent":         recent,
		"longest_streak": longestStreak,
	}, nil
}
