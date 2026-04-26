package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/samuelstrom93/pingit/internal/auth"
	"github.com/samuelstrom93/pingit/internal/db"
	"github.com/samuelstrom93/pingit/internal/domain"
	"github.com/samuelstrom93/pingit/internal/ws"
)

type createMatchRequest struct {
	Kind           string   `json:"kind" validate:"required,oneof=singles doubles"`
	HomePlayers    []string `json:"home_players" validate:"required"`
	VisitorPlayers []string `json:"visitor_players" validate:"required"`
	BestOf         int      `json:"best_of" validate:"required,oneof=1 3 5 7"`
	PointsToWin    int      `json:"points_to_win" validate:"required,oneof=5 11"`
}

func (s *Server) handleCreateMatch(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req createMatchRequest
	if !s.decodeAndValidate(w, r, &req) {
		return
	}
	if err := validateMatchParticipants(req.Kind, req.HomePlayers, req.VisitorPlayers); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	matchID, err := s.createMatch(r.Context(), nil, spaceID, user.ID, req.Kind, req.HomePlayers, req.VisitorPlayers, req.BestOf, req.PointsToWin, "", "", nil, nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create match")
		return
	}
	s.respondMatch(w, r, matchID, http.StatusCreated)
}

func (s *Server) handleListMatches(w http.ResponseWriter, r *http.Request) {
	spaceID := chi.URLParam(r, "spaceID")
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), spaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	status := r.URL.Query().Get("status")
	limit := 20
	if value := r.URL.Query().Get("limit"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	query := `SELECT id, space_id, kind, tournament_id, tournament_phase, tournament_group_id, tournament_bracket_round, best_of, points_to_win, status, winner_side, started_at, completed_at, created_by, created_at, updated_at, deleted_at
FROM matches WHERE space_id = ? AND deleted_at IS NULL`
	args := []any{spaceID}
	if status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to list matches")
		return
	}
	defer rows.Close()
	matches, err := scanMatches(rows)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to decode matches")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"matches": matches})
}

func (s *Server) handleGetMatch(w http.ResponseWriter, r *http.Request) {
	matchID := chi.URLParam(r, "matchID")
	match, payload, err := s.loadMatchPayload(r.Context(), matchID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "match not found")
		return
	}
	user := userFromContext(r.Context())
	if err := s.requireMembership(r.Context(), match.SpaceID, user.ID); err != nil {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	s.writeJSON(w, http.StatusOK, payload)
}

type scoreRequest struct {
	Side string `json:"side" validate:"required,oneof=home visitor"`
}

func (s *Server) handleScoreMatch(w http.ResponseWriter, r *http.Request) {
	matchID := chi.URLParam(r, "matchID")
	user := userFromContext(r.Context())
	match, _, err := s.loadMatchPayload(r.Context(), matchID)
	if err != nil || s.requireMembership(r.Context(), match.SpaceID, user.ID) != nil || match.Status != "in_progress" {
		s.writeError(w, http.StatusBadRequest, "match is not scoreable")
		return
	}
	var req scoreRequest
	if !s.decodeAndValidate(w, r, &req) {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()
	currentGame, err := s.currentGameTx(r.Context(), tx, matchID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to load current game")
		return
	}
	sequence := s.nextSequenceTx(r.Context(), tx, currentGame.ID)
	result := domain.ApplyScore(match.PointsToWin, currentGame.HomeScore, currentGame.VisitorScore, req.Side)
	event := db.ScoreEvent{
		ID:                auth.NewID(),
		GameID:            currentGame.ID,
		Sequence:          sequence,
		ScorerSide:        req.Side,
		HomeScoreAfter:    result.GameState.Home,
		VisitorScoreAfter: result.GameState.Visitor,
		OccurredAt:        nowMS(),
	}
	if _, err := tx.ExecContext(r.Context(), `
INSERT INTO score_events (id, game_id, sequence, scorer_side, home_score_after, visitor_score_after, occurred_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`, event.ID, event.GameID, event.Sequence, event.ScorerSide, event.HomeScoreAfter, event.VisitorScoreAfter, event.OccurredAt); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to write score")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE games SET home_score = ?, visitor_score = ? WHERE id = ?`, event.HomeScoreAfter, event.VisitorScoreAfter, currentGame.ID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to update game")
		return
	}

	s.hub.Broadcast(matchID, ws.Message{Type: "score_event", Data: map[string]any{
		"id":                  event.ID,
		"game_id":             event.GameID,
		"sequence":            event.Sequence,
		"scorer_side":         event.ScorerSide,
		"home_score_after":    event.HomeScoreAfter,
		"visitor_score_after": event.VisitorScoreAfter,
		"occurred_at":         event.OccurredAt,
		"game_state":          result.GameState,
	}})

	if result.GameCompleted {
		now := nowMS()
		if _, err := tx.ExecContext(r.Context(), `UPDATE games SET status = 'completed', completed_at = ?, home_score = ?, visitor_score = ? WHERE id = ?`,
			now, event.HomeScoreAfter, event.VisitorScoreAfter, currentGame.ID); err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to complete game")
			return
		}
		completedGames, err := s.completedGameStatesTx(r.Context(), tx, matchID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to load completed games")
			return
		}
		if winner, done := domain.MatchWinner(match.BestOf, completedGames); done {
			if _, err := tx.ExecContext(r.Context(), `UPDATE matches SET status = 'completed', winner_side = ?, completed_at = ?, updated_at = ? WHERE id = ?`,
				winner, now, now, matchID); err != nil {
				s.writeError(w, http.StatusInternalServerError, "failed to complete match")
				return
			}
			if err := s.advanceTournamentMatchTx(r.Context(), tx, matchID); err != nil {
				s.writeError(w, http.StatusInternalServerError, "failed to advance tournament")
				return
			}
			s.hub.Broadcast(matchID, ws.Message{Type: "match_completed", Data: map[string]any{"winner_side": winner, "final_games": completedGames}})
		} else {
			nextGameID := auth.NewID()
			if _, err := tx.ExecContext(r.Context(), `
INSERT INTO games (id, match_id, game_number, home_score, visitor_score, status, started_at)
VALUES (?, ?, ?, 0, 0, 'in_progress', ?)`, nextGameID, matchID, len(completedGames)+1, now); err != nil {
				s.writeError(w, http.StatusInternalServerError, "failed to create next game")
				return
			}
			s.hub.Broadcast(matchID, ws.Message{Type: "game_completed", Data: map[string]any{
				"game_id":       currentGame.ID,
				"home_score":    event.HomeScoreAfter,
				"visitor_score": event.VisitorScoreAfter,
				"next_game_id":  nextGameID,
			}})
		}
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE matches SET updated_at = ? WHERE id = ?`, nowMS(), matchID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to bump match")
		return
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit score")
		return
	}
	s.respondMatch(w, r, matchID, http.StatusOK)
}

func (s *Server) handleUndoMatch(w http.ResponseWriter, r *http.Request) {
	matchID := chi.URLParam(r, "matchID")
	user := userFromContext(r.Context())
	match, _, err := s.loadMatchPayload(r.Context(), matchID)
	if err != nil || s.requireMembership(r.Context(), match.SpaceID, user.ID) != nil || match.Status != "in_progress" {
		s.writeError(w, http.StatusBadRequest, "match cannot be undone")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()

	game, err := s.currentOrLatestCompletedGameTx(r.Context(), tx, matchID)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "no score to undo")
		return
	}
	event, err := s.lastScoreEventTx(r.Context(), tx, game.ID)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "no score to undo")
		return
	}
	prevHome, prevVisitor := 0, 0
	if event.Sequence > 1 {
		prev, err := s.scoreEventBySequenceTx(r.Context(), tx, game.ID, event.Sequence-1)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to load previous event")
			return
		}
		prevHome = prev.HomeScoreAfter
		prevVisitor = prev.VisitorScoreAfter
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM score_events WHERE id = ?`, event.ID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to remove score")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE games SET home_score = ?, visitor_score = ?, status = 'in_progress', completed_at = NULL WHERE id = ?`, prevHome, prevVisitor, game.ID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to rewind game")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `DELETE FROM games WHERE match_id = ? AND status = 'in_progress' AND game_number > ?`, matchID, game.GameNumber); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to clean later game")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `UPDATE matches SET updated_at = ?, status = 'in_progress', winner_side = NULL, completed_at = NULL WHERE id = ?`, nowMS(), matchID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to rewind match")
		return
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit undo")
		return
	}
	s.respondMatch(w, r, matchID, http.StatusOK)
}

type editMatchRequest struct {
	Games []struct {
		GameNumber   int `json:"game_number"`
		HomeScore    int `json:"home_score"`
		VisitorScore int `json:"visitor_score"`
	} `json:"games"`
}

func (s *Server) handleEditMatch(w http.ResponseWriter, r *http.Request) {
	matchID := chi.URLParam(r, "matchID")
	user := userFromContext(r.Context())
	match, payload, err := s.loadMatchPayload(r.Context(), matchID)
	if err != nil || match.Status != "completed" {
		s.writeError(w, http.StatusBadRequest, "match is not editable")
		return
	}
	if err := s.requireAdmin(r.Context(), match.SpaceID, user.ID); err != nil && user.ID != match.CreatedBy {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req editMatchRequest
	if !s.decodeJSON(w, r, &req) {
		return
	}
	if len(req.Games) == 0 {
		s.writeError(w, http.StatusBadRequest, "games are required")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback()

	var gameStates []domain.GameState
	for _, game := range req.Games {
		if _, err := tx.ExecContext(r.Context(), `UPDATE games SET home_score = ?, visitor_score = ?, status = 'completed' WHERE match_id = ? AND game_number = ?`,
			game.HomeScore, game.VisitorScore, matchID, game.GameNumber); err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to update games")
			return
		}
		gameStates = append(gameStates, domain.GameState{Home: game.HomeScore, Visitor: game.VisitorScore})
	}
	winner, _ := domain.MatchWinner(match.BestOf, gameStates)
	diff := map[string]any{"before": payload, "after": req}
	diffJSON, _ := json.Marshal(diff)
	now := nowMS()
	if _, err := tx.ExecContext(r.Context(), `UPDATE matches SET winner_side = ?, updated_at = ? WHERE id = ?`, winner, now, matchID); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to update match winner")
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO match_edits (id, match_id, edited_by, edited_at, changes_json) VALUES (?, ?, ?, ?, ?)`,
		auth.NewID(), matchID, user.ID, now, string(diffJSON)); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to store audit log")
		return
	}
	if err := tx.Commit(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to commit edit")
		return
	}
	s.hub.Broadcast(matchID, ws.Message{Type: "match_updated", Data: map[string]any{"match": matchID}})
	s.respondMatch(w, r, matchID, http.StatusOK)
}

func (s *Server) handleDeleteMatch(w http.ResponseWriter, r *http.Request) {
	matchID := chi.URLParam(r, "matchID")
	user := userFromContext(r.Context())
	match, _, err := s.loadMatchPayload(r.Context(), matchID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "match not found")
		return
	}
	if err := s.requireAdmin(r.Context(), match.SpaceID, user.ID); err != nil && user.ID != match.CreatedBy {
		s.writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	_, err = s.db.ExecContext(r.Context(), `UPDATE matches SET deleted_at = ?, updated_at = ? WHERE id = ?`, nowMS(), nowMS(), matchID)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to delete match")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func validateMatchParticipants(kind string, home, visitor []string) error {
	want := 1
	if kind == "doubles" {
		want = 2
	}
	if len(home) != want || len(visitor) != want {
		return fmt.Errorf("%s requires %d players per side", kind, want)
	}
	seen := map[string]bool{}
	for _, id := range append(append([]string{}, home...), visitor...) {
		if id == "" {
			return fmt.Errorf("%s requires player ids for every slot", kind)
		}
		if seen[id] {
			return fmt.Errorf("player %s cannot be selected more than once", id)
		}
		seen[id] = true
	}
	return nil
}

func (s *Server) createMatch(ctx context.Context, tx *sql.Tx, spaceID, createdBy, kind string, homePlayers, visitorPlayers []string, bestOf, pointsToWin int, tournamentID, phase string, groupID *string, bracketRound *int) (string, error) {
	exec := sqlExecutor{s.db, tx}
	matchID := auth.NewID()
	now := nowMS()
	var tournament any
	if tournamentID != "" {
		tournament = tournamentID
	}
	_, err := exec.ExecContext(ctx, `
INSERT INTO matches (id, space_id, kind, tournament_id, tournament_phase, tournament_group_id, tournament_bracket_round, best_of, points_to_win, status, started_at, created_by, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'in_progress', ?, ?, ?, ?)`,
		matchID, spaceID, kind, tournament, nullableString(phase), nullableStringPtr(groupID), nullableIntPtr(bracketRound), bestOf, pointsToWin, now, createdBy, now, now)
	if err != nil {
		return "", err
	}
	for i, playerID := range homePlayers {
		if _, err := exec.ExecContext(ctx, `INSERT INTO match_participants (match_id, player_id, side, slot) VALUES (?, ?, 'home', ?)`, matchID, playerID, i+1); err != nil {
			return "", err
		}
	}
	for i, playerID := range visitorPlayers {
		if _, err := exec.ExecContext(ctx, `INSERT INTO match_participants (match_id, player_id, side, slot) VALUES (?, ?, 'visitor', ?)`, matchID, playerID, i+1); err != nil {
			return "", err
		}
	}
	if _, err := exec.ExecContext(ctx, `INSERT INTO games (id, match_id, game_number, status, started_at) VALUES (?, ?, 1, 'in_progress', ?)`, auth.NewID(), matchID, now); err != nil {
		return "", err
	}
	return matchID, nil
}

func (s *Server) respondMatch(w http.ResponseWriter, r *http.Request, matchID string, status int) {
	_, payload, err := s.loadMatchPayload(r.Context(), matchID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "match not found")
		return
	}
	s.writeJSON(w, status, payload)
}

func (s *Server) loadMatchPayload(ctx context.Context, matchID string) (db.Match, map[string]any, error) {
	matchRow := s.db.QueryRowContext(ctx, `SELECT id, space_id, kind, tournament_id, tournament_phase, tournament_group_id, tournament_bracket_round, best_of, points_to_win, status, winner_side, started_at, completed_at, created_by, created_at, updated_at, deleted_at FROM matches WHERE id = ? AND deleted_at IS NULL`, matchID)
	match, err := scanMatch(matchRow)
	if err != nil {
		return db.Match{}, nil, err
	}
	games, _ := s.gamesForMatch(ctx, matchID)
	participants, _ := s.participantsForMatch(ctx, matchID)
	events, _ := s.scoreEventsForMatch(ctx, matchID)
	payload := map[string]any{
		"match":        match,
		"games":        games,
		"participants": participants,
		"score_events": events,
	}
	return match, payload, nil
}

func (s *Server) gamesForMatch(ctx context.Context, matchID string) ([]db.Game, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, match_id, game_number, home_score, visitor_score, status, started_at, completed_at FROM games WHERE match_id = ? ORDER BY game_number ASC`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var games []db.Game
	for rows.Next() {
		game, err := scanGame(rows)
		if err != nil {
			return nil, err
		}
		games = append(games, game)
	}
	return games, nil
}

func (s *Server) participantsForMatch(ctx context.Context, matchID string) ([]map[string]any, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT mp.side, mp.slot, p.id, p.display_name, p.user_id, p.avatar_url
FROM match_participants mp
JOIN players p ON p.id = mp.player_id
WHERE mp.match_id = ?
ORDER BY mp.side ASC, mp.slot ASC`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var participants []map[string]any
	for rows.Next() {
		var side string
		var slot int
		var player db.Player
		var userID, avatar sql.NullString
		if err := rows.Scan(&side, &slot, &player.ID, &player.DisplayName, &userID, &avatar); err != nil {
			return nil, err
		}
		if userID.Valid {
			player.UserID = &userID.String
		}
		if avatar.Valid {
			player.AvatarURL = &avatar.String
		}
		participants = append(participants, map[string]any{"side": side, "slot": slot, "player": player})
	}
	return participants, nil
}

func (s *Server) scoreEventsForMatch(ctx context.Context, matchID string) ([]db.ScoreEvent, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT se.id, se.game_id, se.sequence, se.scorer_side, se.home_score_after, se.visitor_score_after, se.occurred_at
FROM score_events se
JOIN games g ON g.id = se.game_id
WHERE g.match_id = ?
ORDER BY g.game_number ASC, se.sequence ASC`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []db.ScoreEvent
	for rows.Next() {
		var event db.ScoreEvent
		if err := rows.Scan(&event.ID, &event.GameID, &event.Sequence, &event.ScorerSide, &event.HomeScoreAfter, &event.VisitorScoreAfter, &event.OccurredAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *Server) currentGameTx(ctx context.Context, tx *sql.Tx, matchID string) (db.Game, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, match_id, game_number, home_score, visitor_score, status, started_at, completed_at FROM games WHERE match_id = ? AND status = 'in_progress' ORDER BY game_number DESC LIMIT 1`, matchID)
	return scanGame(row)
}

func (s *Server) currentOrLatestCompletedGameTx(ctx context.Context, tx *sql.Tx, matchID string) (db.Game, error) {
	row := tx.QueryRowContext(ctx, `
SELECT id, match_id, game_number, home_score, visitor_score, status, started_at, completed_at
FROM games WHERE match_id = ?
ORDER BY CASE WHEN status = 'in_progress' THEN 0 ELSE 1 END ASC, game_number DESC LIMIT 1`, matchID)
	return scanGame(row)
}

func (s *Server) nextSequenceTx(ctx context.Context, tx *sql.Tx, gameID string) int {
	var sequence int
	_ = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM score_events WHERE game_id = ?`, gameID).Scan(&sequence)
	if sequence == 0 {
		return 1
	}
	return sequence
}

func (s *Server) completedGameStatesTx(ctx context.Context, tx *sql.Tx, matchID string) ([]domain.GameState, error) {
	rows, err := tx.QueryContext(ctx, `SELECT home_score, visitor_score FROM games WHERE match_id = ? AND status = 'completed' ORDER BY game_number ASC`, matchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var states []domain.GameState
	for rows.Next() {
		var state domain.GameState
		if err := rows.Scan(&state.Home, &state.Visitor); err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, nil
}

func (s *Server) lastScoreEventTx(ctx context.Context, tx *sql.Tx, gameID string) (db.ScoreEvent, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, game_id, sequence, scorer_side, home_score_after, visitor_score_after, occurred_at FROM score_events WHERE game_id = ? ORDER BY sequence DESC LIMIT 1`, gameID)
	var event db.ScoreEvent
	err := row.Scan(&event.ID, &event.GameID, &event.Sequence, &event.ScorerSide, &event.HomeScoreAfter, &event.VisitorScoreAfter, &event.OccurredAt)
	return event, err
}

func (s *Server) scoreEventBySequenceTx(ctx context.Context, tx *sql.Tx, gameID string, sequence int) (db.ScoreEvent, error) {
	row := tx.QueryRowContext(ctx, `SELECT id, game_id, sequence, scorer_side, home_score_after, visitor_score_after, occurred_at FROM score_events WHERE game_id = ? AND sequence = ?`, gameID, sequence)
	var event db.ScoreEvent
	err := row.Scan(&event.ID, &event.GameID, &event.Sequence, &event.ScorerSide, &event.HomeScoreAfter, &event.VisitorScoreAfter, &event.OccurredAt)
	return event, err
}

func scanMatch(row interface{ Scan(dest ...any) error }) (db.Match, error) {
	var match db.Match
	var tournamentID, phase, groupID, winner sql.NullString
	var bracketRound sql.NullInt64
	var completedAt, deleted sql.NullInt64
	err := row.Scan(&match.ID, &match.SpaceID, &match.Kind, &tournamentID, &phase, &groupID, &bracketRound, &match.BestOf, &match.PointsToWin, &match.Status, &winner, &match.StartedAt, &completedAt, &match.CreatedBy, &match.CreatedAt, &match.UpdatedAt, &deleted)
	if err != nil {
		return match, err
	}
	if tournamentID.Valid {
		match.TournamentID = &tournamentID.String
	}
	if phase.Valid {
		match.TournamentPhase = &phase.String
	}
	if groupID.Valid {
		match.TournamentGroupID = &groupID.String
	}
	if bracketRound.Valid {
		value := int(bracketRound.Int64)
		match.TournamentBracketRound = &value
	}
	if winner.Valid {
		match.WinnerSide = &winner.String
	}
	if completedAt.Valid {
		match.CompletedAt = &completedAt.Int64
	}
	if deleted.Valid {
		match.DeletedAt = &deleted.Int64
	}
	return match, nil
}

func scanMatches(rows *sql.Rows) ([]db.Match, error) {
	var matches []db.Match
	for rows.Next() {
		match, err := scanMatch(rows)
		if err != nil {
			return nil, err
		}
		matches = append(matches, match)
	}
	return matches, nil
}

func scanGame(row interface{ Scan(dest ...any) error }) (db.Game, error) {
	var game db.Game
	var completed sql.NullInt64
	err := row.Scan(&game.ID, &game.MatchID, &game.GameNumber, &game.HomeScore, &game.VisitorScore, &game.Status, &game.StartedAt, &completed)
	if err != nil {
		return game, err
	}
	if completed.Valid {
		game.CompletedAt = &completed.Int64
	}
	return game, nil
}
