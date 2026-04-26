package api

import (
	"context"
	"database/sql"
	"math"
	"net/http"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/samuelstrom93/pingit/internal/auth"
	"github.com/samuelstrom93/pingit/internal/db"
	"github.com/samuelstrom93/pingit/internal/domain"
)

type createTournamentRequest struct {
	Name        string   `json:"name" validate:"required,min=2"`
	Format      string   `json:"format" validate:"required,oneof=round_robin bracket groups_knockout"`
	BestOf      int      `json:"best_of" validate:"required,oneof=1 3 5 7"`
	PointsToWin int      `json:"points_to_win" validate:"required,oneof=5 11"`
	PlayerIDs   []string `json:"player_ids" validate:"required"`
	Seeds       []int    `json:"seeds"`
}

func (s *Server) handleCreateTournament(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req createTournamentRequest
	if !s.decodeAndValidate(w, r, &req) {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()
	tournamentID := auth.NewID()
	now := nowMS()
	_, err = tx.ExecContext(r.Context(), `
INSERT INTO tournaments (id, space_id, name, format, best_of, points_to_win, status, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, 'setup', ?, ?, ?)`,
		tournamentID, spaceID, req.Name, req.Format, req.BestOf, req.PointsToWin, user.ID, now, now)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create tournament")
		return
	}
	for i, playerID := range req.PlayerIDs {
		var seed any
		if i < len(req.Seeds) {
			seed = req.Seeds[i]
		}
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO tournament_players (tournament_id, player_id, seed) VALUES (?, ?, ?)`, tournamentID, playerID, seed); err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to attach players")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit tournament")
		return
	}
	tournament, err := s.findTournament(r.Context(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to reload tournament")
		return
	}
	s.writeJSON(w, http.StatusCreated, tournament)
}

func (s *Server) handleListTournaments(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	statusFilter := r.URL.Query().Get("status")
	query := `SELECT id, space_id, name, format, best_of, points_to_win, status, winner_player_id, created_by, created_at, updated_at, deleted_at
FROM tournaments WHERE space_id = ? AND deleted_at IS NULL`
	args := []any{spaceID}
	if statusFilter != "" {
		query += ` AND status = ?`
		args = append(args, statusFilter)
	}
	query += ` ORDER BY updated_at DESC`
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list tournaments")
		return
	}
	defer rows.Close()
	var tournaments []db.Tournament
	for rows.Next() {
		var t db.Tournament
		var winner sql.NullString
		var deleted sql.NullInt64
		if err := rows.Scan(&t.ID, &t.SpaceID, &t.Name, &t.Format, &t.BestOf, &t.PointsToWin, &t.Status, &winner, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &deleted); err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to decode tournaments")
			return
		}
		if winner.Valid {
			t.WinnerPlayerID = &winner.String
		}
		if deleted.Valid {
			t.DeletedAt = &deleted.Int64
		}
		tournaments = append(tournaments, t)
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"tournaments": tournaments})
}

func (s *Server) handleStartTournament(w http.ResponseWriter, r *http.Request) {
	tournamentID := chi.URLParam(r, "tournamentID")
	user := userFromContext(r.Context())
	tournament, err := s.findTournament(r.Context(), tournamentID)
	if err != nil || s.requireMembership(r.Context(), tournament.SpaceID, user.ID) != nil {
		s.writeError(w, http.StatusNotFound, "tournament not found")
		return
	}
	playerIDs, err := s.tournamentPlayerIDs(r.Context(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to load players")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()

	if tournament.Format == "round_robin" {
		for _, pair := range domain.RoundRobinPairs(playerIDs) {
			if _, err := s.createMatch(r.Context(), tx, tournament.SpaceID, user.ID, "singles", []string{pair.Home}, []string{pair.Away}, tournament.BestOf, tournament.PointsToWin, tournamentID, "round_robin", nil, nil); err != nil {
				s.writeError(w, http.StatusInternalServerError, "failed to create round robin matches")
				return
			}
		}
	} else if tournament.Format == "groups_knockout" {
		groups := tournamentGroups(playerIDs)
		for groupID, ids := range groups {
			gid := groupID
			for _, pair := range domain.RoundRobinPairs(ids) {
				if _, err := s.createMatch(r.Context(), tx, tournament.SpaceID, user.ID, "singles", []string{pair.Home}, []string{pair.Away}, tournament.BestOf, tournament.PointsToWin, tournamentID, "group", &gid, nil); err != nil {
					s.writeError(w, http.StatusInternalServerError, "failed to create group stage matches")
					return
				}
			}
		}
	} else {
		for _, match := range domain.GenerateBracket(playerIDs) {
			var homePlayers, visitorPlayers []string
			if match.Home != nil && *match.Home != "" {
				homePlayers = []string{*match.Home}
			}
			if match.Visitor != nil && *match.Visitor != "" {
				visitorPlayers = []string{*match.Visitor}
			}
			matchID, err := s.createMatch(r.Context(), tx, tournament.SpaceID, user.ID, "singles", homePlayers, visitorPlayers, tournament.BestOf, tournament.PointsToWin, tournamentID, "bracket", nil, &match.Round)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, "failed to create bracket matches")
				return
			}
			if match.HasBye {
				if err := s.completeByeMatchTx(r.Context(), tx, matchID); err != nil {
					s.writeError(w, http.StatusInternalServerError, "failed to auto-advance bye")
					return
				}
			}
		}
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE tournaments SET status = 'in_progress', updated_at = ? WHERE id = ?`, nowMS(), tournamentID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to update tournament")
		return
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit tournament start")
		return
	}
	s.handleGetTournament(w, r)
}

func (s *Server) handleGetTournament(w http.ResponseWriter, r *http.Request) {
	tournamentID := chi.URLParam(r, "tournamentID")
	tournament, err := s.findTournament(r.Context(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "tournament not found")
		return
	}
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), tournament.SpaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	players, _ := s.tournamentPlayers(r.Context(), tournamentID)
	matches := s.matchesByTournament(r.Context(), tournamentID)
	standings, _ := s.computeStandings(r.Context(), tournamentID)
	s.writeJSON(w, http.StatusOK, map[string]any{
		"tournament": tournament,
		"players":    players,
		"matches":    matches,
		"standings":  standings,
	})
}

func (s *Server) handleTournamentStandings(w http.ResponseWriter, r *http.Request) {
	tournamentID := chi.URLParam(r, "tournamentID")
	tournament, err := s.findTournament(r.Context(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "tournament not found")
		return
	}
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), tournament.SpaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	standings, err := s.computeStandings(r.Context(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to compute standings")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"standings": standings})
}

func (s *Server) handleDeleteTournament(w http.ResponseWriter, r *http.Request) {
	tournamentID := chi.URLParam(r, "tournamentID")
	tournament, err := s.findTournament(r.Context(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "tournament not found")
		return
	}
	user := userFromContext(r.Context())
	if err := s.requireAdmin(r.Context(), tournament.SpaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	_, err = s.db.ExecContext(r.Context(), `UPDATE tournaments SET deleted_at = ?, updated_at = ? WHERE id = ?`, nowMS(), nowMS(), tournamentID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to delete tournament")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) findTournament(ctx context.Context, tournamentID string) (db.Tournament, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, space_id, name, format, best_of, points_to_win, status, winner_player_id, created_by, created_at, updated_at, deleted_at FROM tournaments WHERE id = ? AND deleted_at IS NULL`, tournamentID)
	var tournament db.Tournament
	var winner sql.NullString
	var deleted sql.NullInt64
	if err := row.Scan(&tournament.ID, &tournament.SpaceID, &tournament.Name, &tournament.Format, &tournament.BestOf, &tournament.PointsToWin, &tournament.Status, &winner, &tournament.CreatedBy, &tournament.CreatedAt, &tournament.UpdatedAt, &deleted); err != nil {
		return tournament, err
	}
	if winner.Valid {
		tournament.WinnerPlayerID = &winner.String
	}
	if deleted.Valid {
		tournament.DeletedAt = &deleted.Int64
	}
	return tournament, nil
}

func (s *Server) tournamentPlayerIDs(ctx context.Context, tournamentID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT player_id FROM tournament_players WHERE tournament_id = ? ORDER BY COALESCE(seed, 999999), player_id`, tournamentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *Server) tournamentPlayers(ctx context.Context, tournamentID string) ([]db.Player, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT p.id, p.space_id, p.display_name, p.user_id, p.avatar_url, p.created_at, p.deleted_at
FROM tournament_players tp
JOIN players p ON p.id = tp.player_id
WHERE tp.tournament_id = ? AND p.deleted_at IS NULL
ORDER BY COALESCE(tp.seed, 999999), p.display_name, p.id`, tournamentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanPlayers(rows)
}

func (s *Server) matchesByTournament(ctx context.Context, tournamentID string) []db.Match {
	rows, err := s.db.QueryContext(ctx, `SELECT id, space_id, kind, tournament_id, tournament_phase, tournament_group_id, tournament_bracket_round, best_of, points_to_win, status, winner_side, started_at, completed_at, created_by, created_at, updated_at, deleted_at FROM matches WHERE tournament_id = ? AND deleted_at IS NULL ORDER BY COALESCE(tournament_bracket_round, 0), created_at`, tournamentID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	matches, _ := scanMatches(rows)
	return matches
}

func (s *Server) computeStandings(ctx context.Context, tournamentID string) ([]map[string]any, error) {
	playerIDs, err := s.tournamentPlayerIDs(ctx, tournamentID)
	if err != nil {
		return nil, err
	}
	type standing struct {
		PlayerID    string
		Wins        int
		Losses      int
		SetsWon     int
		SetsLost    int
		PointsWon   int
		PointsLost  int
		PointsRatio float64
		HeadToHead  map[string]int
	}
	stats := map[string]*standing{}
	for _, id := range playerIDs {
		stats[id] = &standing{PlayerID: id, HeadToHead: map[string]int{}}
	}
	matches := s.matchesByTournament(ctx, tournamentID)
	for _, match := range matches {
		if match.Status != "completed" || match.WinnerSide == nil {
			continue
		}
		participants, _ := s.participantsForMatch(ctx, match.ID)
		if len(participants) < 2 {
			continue
		}
		home := participants[0]["player"].(db.Player).ID
		visitor := participants[len(participants)-1]["player"].(db.Player).ID
		games, _ := s.gamesForMatch(ctx, match.ID)
		homeSets, visitorSets, homePoints, visitorPoints := 0, 0, 0, 0
		for _, game := range games {
			homePoints += game.HomeScore
			visitorPoints += game.VisitorScore
			if game.HomeScore > game.VisitorScore {
				homeSets++
			}
			if game.VisitorScore > game.HomeScore {
				visitorSets++
			}
		}
		stats[home].SetsWon += homeSets
		stats[home].SetsLost += visitorSets
		stats[home].PointsWon += homePoints
		stats[home].PointsLost += visitorPoints
		stats[visitor].SetsWon += visitorSets
		stats[visitor].SetsLost += homeSets
		stats[visitor].PointsWon += visitorPoints
		stats[visitor].PointsLost += homePoints
		if *match.WinnerSide == "home" {
			stats[home].Wins++
			stats[visitor].Losses++
			stats[home].HeadToHead[visitor]++
		} else {
			stats[visitor].Wins++
			stats[home].Losses++
			stats[visitor].HeadToHead[home]++
		}
	}
	var standings []map[string]any
	for _, id := range playerIDs {
		item := stats[id]
		if item.PointsLost == 0 {
			item.PointsRatio = float64(item.PointsWon)
		} else {
			item.PointsRatio = float64(item.PointsWon) / float64(item.PointsLost)
		}
		standings = append(standings, map[string]any{
			"player_id":    id,
			"wins":         item.Wins,
			"losses":       item.Losses,
			"sets_won":     item.SetsWon,
			"sets_lost":    item.SetsLost,
			"points_won":   item.PointsWon,
			"points_lost":  item.PointsLost,
			"points_ratio": math.Round(item.PointsRatio*1000) / 1000,
		})
	}
	slices.SortFunc(standings, func(a, b map[string]any) int {
		aw, bw := a["wins"].(int), b["wins"].(int)
		if aw != bw {
			return bw - aw
		}
		ap, bp := a["player_id"].(string), b["player_id"].(string)
		if stats[ap].HeadToHead[bp] != stats[bp].HeadToHead[ap] {
			return stats[bp].HeadToHead[ap] - stats[ap].HeadToHead[bp]
		}
		ar, br := a["points_ratio"].(float64), b["points_ratio"].(float64)
		switch {
		case ar > br:
			return -1
		case ar < br:
			return 1
		default:
			return strings.Compare(ap, bp)
		}
	})
	for i := range standings {
		standings[i]["rank"] = i + 1
	}
	return standings, nil
}

func (s *Server) completeByeMatchTx(ctx context.Context, tx *sql.Tx, matchID string) error {
	participantsRows, err := tx.QueryContext(ctx, `SELECT side, player_id FROM match_participants WHERE match_id = ?`, matchID)
	if err != nil {
		return err
	}
	defer participantsRows.Close()
	winner := "home"
	count := 0
	for participantsRows.Next() {
		var side, playerID string
		if err := participantsRows.Scan(&side, &playerID); err != nil {
			return err
		}
		count++
		if side == "visitor" {
			winner = "visitor"
		}
	}
	if count != 1 {
		return nil
	}
	now := nowMS()
	if _, err := tx.ExecContext(ctx, `UPDATE games SET home_score = 1, visitor_score = 0, status = 'completed', completed_at = ? WHERE match_id = ?`, now, matchID); err != nil {
		return err
	}
	if winner == "visitor" {
		if _, err := tx.ExecContext(ctx, `UPDATE games SET home_score = 0, visitor_score = 1 WHERE match_id = ?`, matchID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE matches SET status = 'completed', winner_side = ?, completed_at = ?, updated_at = ? WHERE id = ?`, winner, now, now, matchID); err != nil {
		return err
	}
	return s.advanceTournamentMatchTx(ctx, tx, matchID)
}

func (s *Server) advanceTournamentMatchTx(ctx context.Context, tx *sql.Tx, matchID string) error {
	var tournamentID, phase sql.NullString
	var round sql.NullInt64
	var winnerSide sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT tournament_id, tournament_phase, tournament_bracket_round, winner_side FROM matches WHERE id = ?`, matchID).Scan(&tournamentID, &phase, &round, &winnerSide)
	if err != nil || !tournamentID.Valid || !winnerSide.Valid {
		return err
	}
	if phase.String == "round_robin" {
		var remaining int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM matches WHERE tournament_id = ? AND status != 'completed' AND deleted_at IS NULL`, tournamentID.String).Scan(&remaining); err != nil {
			return err
		}
		if remaining == 0 {
			standings, err := s.computeStandings(ctx, tournamentID.String)
			if err != nil {
				return err
			}
			var winner any
			if len(standings) > 0 {
				winner = standings[0]["player_id"]
			}
			_, err = tx.ExecContext(ctx, `UPDATE tournaments SET status = 'completed', winner_player_id = ?, updated_at = ? WHERE id = ?`, winner, nowMS(), tournamentID.String)
			return err
		}
		return nil
	}
	if phase.String == "group" {
		return s.maybeCreateKnockoutFromGroupsTx(ctx, tx, tournamentID.String)
	}

	participantsRows, err := tx.QueryContext(ctx, `SELECT side, player_id FROM match_participants WHERE match_id = ? ORDER BY slot`, matchID)
	if err != nil {
		return err
	}
	defer participantsRows.Close()
	sideToPlayer := map[string]string{}
	for participantsRows.Next() {
		var side, playerID string
		if err := participantsRows.Scan(&side, &playerID); err != nil {
			return err
		}
		sideToPlayer[side] = playerID
	}
	playerID := sideToPlayer[winnerSide.String]
	if playerID == "" || !round.Valid {
		return nil
	}
	nextRound := int(round.Int64) + 1
	rows, err := tx.QueryContext(ctx, `SELECT id FROM matches WHERE tournament_id = ? AND tournament_phase = 'bracket' AND tournament_bracket_round = ? ORDER BY created_at`, tournamentID.String, nextRound)
	if err != nil {
		return err
	}
	defer rows.Close()
	var nextMatches []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		nextMatches = append(nextMatches, id)
	}
	if len(nextMatches) == 0 {
		_, err := tx.ExecContext(ctx, `UPDATE tournaments SET status = 'completed', winner_player_id = ?, updated_at = ? WHERE id = ?`, playerID, nowMS(), tournamentID.String)
		return err
	}
	currentRoundRows, err := tx.QueryContext(ctx, `SELECT id FROM matches WHERE tournament_id = ? AND tournament_phase = 'bracket' AND tournament_bracket_round = ? ORDER BY created_at`, tournamentID.String, round.Int64)
	if err != nil {
		return err
	}
	defer currentRoundRows.Close()
	var currentRound []string
	for currentRoundRows.Next() {
		var id string
		if err := currentRoundRows.Scan(&id); err != nil {
			return err
		}
		currentRound = append(currentRound, id)
	}
	index := slices.Index(currentRound, matchID)
	if index < 0 {
		return nil
	}
	target := nextMatches[index/2]
	side := "home"
	slot := 1
	if index%2 == 1 {
		side = "visitor"
	}
	if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO match_participants (match_id, player_id, side, slot) VALUES (?, ?, ?, ?)`, target, playerID, side, slot); err != nil {
		return err
	}
	return nil
}

type tournamentGroupStanding struct {
	PlayerID    string
	GroupID     string
	Wins        int
	Losses      int
	SetsWon     int
	SetsLost    int
	PointsWon   int
	PointsLost  int
	PointsRatio float64
	HeadToHead  map[string]int
}

func (s *Server) maybeCreateKnockoutFromGroupsTx(ctx context.Context, tx *sql.Tx, tournamentID string) error {
	var tournament db.Tournament
	var winner sql.NullString
	var deleted sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT id, space_id, name, format, best_of, points_to_win, status, winner_player_id, created_by, created_at, updated_at, deleted_at FROM tournaments WHERE id = ? AND deleted_at IS NULL`, tournamentID).Scan(&tournament.ID, &tournament.SpaceID, &tournament.Name, &tournament.Format, &tournament.BestOf, &tournament.PointsToWin, &tournament.Status, &winner, &tournament.CreatedBy, &tournament.CreatedAt, &tournament.UpdatedAt, &deleted); err != nil {
		return err
	}
	if tournament.Format != "groups_knockout" {
		return nil
	}
	var remaining int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM matches WHERE tournament_id = ? AND tournament_phase = 'group' AND status != 'completed' AND deleted_at IS NULL`, tournamentID).Scan(&remaining); err != nil {
		return err
	}
	if remaining > 0 {
		return nil
	}
	var existingBracket int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM matches WHERE tournament_id = ? AND tournament_phase = 'bracket' AND deleted_at IS NULL`, tournamentID).Scan(&existingBracket); err != nil {
		return err
	}
	if existingBracket > 0 {
		return nil
	}

	groups, overall, err := s.groupStandingsTx(ctx, tx, tournamentID)
	if err != nil {
		return err
	}
	seeds := knockoutSeeds(groups, overall)
	if len(seeds) == 0 {
		return nil
	}
	if len(seeds) == 1 {
		_, err := tx.ExecContext(ctx, `UPDATE tournaments SET status = 'completed', winner_player_id = ?, updated_at = ? WHERE id = ?`, seeds[0], nowMS(), tournamentID)
		return err
	}
	for _, match := range domain.GenerateBracket(seeds) {
		var homePlayers, visitorPlayers []string
		if match.Home != nil && *match.Home != "" {
			homePlayers = []string{*match.Home}
		}
		if match.Visitor != nil && *match.Visitor != "" {
			visitorPlayers = []string{*match.Visitor}
		}
		matchID, err := s.createMatch(ctx, tx, tournament.SpaceID, tournament.CreatedBy, "singles", homePlayers, visitorPlayers, tournament.BestOf, tournament.PointsToWin, tournamentID, "bracket", nil, &match.Round)
		if err != nil {
			return err
		}
		if match.HasBye {
			if err := s.completeByeMatchTx(ctx, tx, matchID); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE tournaments SET updated_at = ? WHERE id = ?`, nowMS(), tournamentID)
	return err
}

func (s *Server) groupStandingsTx(ctx context.Context, tx *sql.Tx, tournamentID string) (map[string][]tournamentGroupStanding, []tournamentGroupStanding, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id, tournament_group_id, winner_side FROM matches WHERE tournament_id = ? AND tournament_phase = 'group' AND status = 'completed' AND winner_side IS NOT NULL AND deleted_at IS NULL ORDER BY tournament_group_id, created_at`, tournamentID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	stats := map[string]map[string]*tournamentGroupStanding{}
	for rows.Next() {
		var matchID, winnerSide string
		var groupID sql.NullString
		if err := rows.Scan(&matchID, &groupID, &winnerSide); err != nil {
			return nil, nil, err
		}
		gid := groupID.String
		if gid == "" {
			gid = "A"
		}
		participants, err := groupMatchParticipantsTx(ctx, tx, matchID)
		if err != nil {
			return nil, nil, err
		}
		home := participants["home"]
		visitor := participants["visitor"]
		if home == "" || visitor == "" {
			continue
		}
		if stats[gid] == nil {
			stats[gid] = map[string]*tournamentGroupStanding{}
		}
		for _, playerID := range []string{home, visitor} {
			if stats[gid][playerID] == nil {
				stats[gid][playerID] = &tournamentGroupStanding{PlayerID: playerID, GroupID: gid, HeadToHead: map[string]int{}}
			}
		}
		homeSets, visitorSets, homePoints, visitorPoints, err := matchTotalsTx(ctx, tx, matchID)
		if err != nil {
			return nil, nil, err
		}
		stats[gid][home].SetsWon += homeSets
		stats[gid][home].SetsLost += visitorSets
		stats[gid][home].PointsWon += homePoints
		stats[gid][home].PointsLost += visitorPoints
		stats[gid][visitor].SetsWon += visitorSets
		stats[gid][visitor].SetsLost += homeSets
		stats[gid][visitor].PointsWon += visitorPoints
		stats[gid][visitor].PointsLost += homePoints
		if winnerSide == "home" {
			stats[gid][home].Wins++
			stats[gid][visitor].Losses++
			stats[gid][home].HeadToHead[visitor]++
		} else {
			stats[gid][visitor].Wins++
			stats[gid][home].Losses++
			stats[gid][visitor].HeadToHead[home]++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	groups := map[string][]tournamentGroupStanding{}
	var overall []tournamentGroupStanding
	for groupID, groupStats := range stats {
		for _, standing := range groupStats {
			if standing.PointsLost == 0 {
				standing.PointsRatio = float64(standing.PointsWon)
			} else {
				standing.PointsRatio = float64(standing.PointsWon) / float64(standing.PointsLost)
			}
			groups[groupID] = append(groups[groupID], *standing)
			overall = append(overall, *standing)
		}
		SortTournamentStandings(groups[groupID])
	}
	SortTournamentStandings(overall)
	return groups, overall, nil
}

func groupMatchParticipantsTx(ctx context.Context, tx *sql.Tx, matchID string) (map[string]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT side, player_id FROM match_participants WHERE match_id = ?`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	participants := map[string]string{}
	for rows.Next() {
		var side, playerID string
		if err := rows.Scan(&side, &playerID); err != nil {
			return nil, err
		}
		participants[side] = playerID
	}
	return participants, rows.Err()
}

func matchTotalsTx(ctx context.Context, tx *sql.Tx, matchID string) (homeSets, visitorSets, homePoints, visitorPoints int, err error) {
	rows, err := tx.QueryContext(ctx, `SELECT home_score, visitor_score FROM games WHERE match_id = ? AND status = 'completed'`, matchID)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var homeScore, visitorScore int
		if err := rows.Scan(&homeScore, &visitorScore); err != nil {
			return 0, 0, 0, 0, err
		}
		homePoints += homeScore
		visitorPoints += visitorScore
		if homeScore > visitorScore {
			homeSets++
		} else if visitorScore > homeScore {
			visitorSets++
		}
	}
	return homeSets, visitorSets, homePoints, visitorPoints, rows.Err()
}

func SortTournamentStandings(standings []tournamentGroupStanding) {
	slices.SortFunc(standings, func(a, b tournamentGroupStanding) int {
		if a.Wins != b.Wins {
			return b.Wins - a.Wins
		}
		if a.HeadToHead[b.PlayerID] != b.HeadToHead[a.PlayerID] {
			return b.HeadToHead[a.PlayerID] - a.HeadToHead[b.PlayerID]
		}
		setDiffA := a.SetsWon - a.SetsLost
		setDiffB := b.SetsWon - b.SetsLost
		if setDiffA != setDiffB {
			return setDiffB - setDiffA
		}
		pointDiffA := a.PointsWon - a.PointsLost
		pointDiffB := b.PointsWon - b.PointsLost
		if pointDiffA != pointDiffB {
			return pointDiffB - pointDiffA
		}
		switch {
		case a.PointsRatio > b.PointsRatio:
			return -1
		case a.PointsRatio < b.PointsRatio:
			return 1
		default:
			return strings.Compare(a.PlayerID, b.PlayerID)
		}
	})
}

func knockoutSeeds(groups map[string][]tournamentGroupStanding, overall []tournamentGroupStanding) []string {
	if len(groups) == 2 {
		groupIDs := make([]string, 0, len(groups))
		for groupID := range groups {
			groupIDs = append(groupIDs, groupID)
		}
		slices.Sort(groupIDs)
		first := groups[groupIDs[0]]
		second := groups[groupIDs[1]]
		var seeds []string
		if len(first) > 0 {
			seeds = append(seeds, first[0].PlayerID)
		}
		if len(second) > 1 {
			seeds = append(seeds, second[1].PlayerID)
		}
		if len(second) > 0 {
			seeds = append(seeds, second[0].PlayerID)
		}
		if len(first) > 1 {
			seeds = append(seeds, first[1].PlayerID)
		}
		return uniqueSeeds(seeds)
	}
	limit := 4
	if len(overall) < limit {
		limit = len(overall)
	}
	seeds := make([]string, 0, limit)
	for _, standing := range overall[:limit] {
		seeds = append(seeds, standing.PlayerID)
	}
	return uniqueSeeds(seeds)
}

func uniqueSeeds(seeds []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		if seed == "" || seen[seed] {
			continue
		}
		seen[seed] = true
		out = append(out, seed)
	}
	return out
}

func tournamentGroups(playerIDs []string) map[string][]string {
	groups := map[string][]string{}
	if len(playerIDs) == 0 {
		return groups
	}
	groupCount := 1
	if len(playerIDs) > 4 {
		groupCount = 2
	}
	for i, id := range playerIDs {
		groupID := string(rune('A' + (i % groupCount)))
		groups[groupID] = append(groups[groupID], id)
	}
	return groups
}
