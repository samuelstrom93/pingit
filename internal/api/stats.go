package api

import (
	"context"
	"math"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/samuelstrom93/pingit/internal/domain"
)

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
