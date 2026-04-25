package main

import (
	"context"
	"database/sql"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/samuelstrom93/pingit"
	"github.com/samuelstrom93/pingit/internal/api"
	"github.com/samuelstrom93/pingit/internal/auth"
	"github.com/samuelstrom93/pingit/internal/config"
	"github.com/samuelstrom93/pingit/internal/db"
	"github.com/samuelstrom93/pingit/internal/email"
	"github.com/samuelstrom93/pingit/internal/ws"
)

func main() {
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		panic(err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	conn, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	if err := db.Migrate(conn, pingit.MigrationsFS); err != nil {
		panic(err)
	}

	if len(os.Args) > 1 && os.Args[1] == "seed" {
		if err := seed(ctx, conn); err != nil {
			panic(err)
		}
		logger.Info("seed complete", "db", cfg.DBPath)
		return
	}

	var sender email.Sender = email.ConsoleStub{Logger: logger}
	if cfg.ResendAPIKey != "" {
		sender = email.ResendAdapter{APIKey: cfg.ResendAPIKey, From: cfg.EmailFrom}
	}
	appFS, err := fs.Sub(pingit.WebFS, "web/build")
	if err != nil {
		panic(err)
	}
	server := api.NewServer(cfg, conn, logger, sender, ws.NewHub())
	httpServer := &http.Server{
		Addr:    cfg.Addr,
		Handler: server.Router(api.SPAHandler(appFS)),
	}
	logger.Info("starting server", "addr", cfg.Addr, "env", cfg.Env)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		panic(err)
	}
}

func seed(ctx context.Context, conn *sql.DB) error {
	tables := []string{"match_edits", "score_events", "games", "match_participants", "matches", "tournament_players", "tournaments", "space_invitations", "players", "space_members", "spaces", "magic_link_tokens", "sessions", "users"}
	for _, table := range tables {
		if _, err := conn.ExecContext(ctx, "DELETE FROM "+table); err != nil {
			return err
		}
	}

	now := nowMS()
	aliceID := auth.NewID()
	bobID := auth.NewID()
	if _, err := conn.ExecContext(ctx, `INSERT INTO users (id, email, display_name, is_super_admin, created_at) VALUES (?, ?, ?, 0, ?), (?, ?, ?, 0, ?)`,
		aliceID, "alice@example.com", "Alice", now,
		bobID, "bob@example.com", "Bob", now); err != nil {
		return err
	}
	spaceID := auth.NewID()
	if _, err := conn.ExecContext(ctx, `INSERT INTO spaces (id, name, is_invite_only, join_code, created_by, created_at) VALUES (?, ?, 0, ?, ?, ?)`,
		spaceID, "Bläckfiskligan", randomJoinCode(), aliceID, now); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?, ?, 'admin', ?), (?, ?, 'member', ?)`,
		spaceID, aliceID, now, spaceID, bobID, now); err != nil {
		return err
	}
	alicePlayer := auth.NewID()
	bobPlayer := auth.NewID()
	pappaPlayer := auth.NewID()
	ingridPlayer := auth.NewID()
	if _, err := conn.ExecContext(ctx, `
INSERT INTO players (id, space_id, display_name, user_id, created_at) VALUES
(?, ?, ?, ?, ?),
(?, ?, ?, ?, ?),
(?, ?, ?, NULL, ?),
(?, ?, ?, NULL, ?)`,
		alicePlayer, spaceID, "Alice", aliceID, now,
		bobPlayer, spaceID, "Bob", bobID, now,
		pappaPlayer, spaceID, "Pappa", now,
		ingridPlayer, spaceID, "Ingrid", now); err != nil {
		return err
	}

	for _, fixture := range []struct {
		home, visitor string
		bestOf        int
		points        int
		status        string
		games         [][2]int
	}{
		{alicePlayer, bobPlayer, 3, 11, "completed", [][2]int{{11, 9}, {11, 7}}},
		{pappaPlayer, ingridPlayer, 5, 11, "completed", [][2]int{{8, 11}, {11, 9}, {11, 7}, {11, 6}}},
		{alicePlayer, pappaPlayer, 1, 5, "completed", [][2]int{{5, 3}}},
		{bobPlayer, ingridPlayer, 3, 11, "in_progress", [][2]int{{4, 2}}},
	} {
		if err := seedMatch(ctx, conn, spaceID, aliceID, fixture.home, fixture.visitor, fixture.bestOf, fixture.points, fixture.status, fixture.games); err != nil {
			return err
		}
	}

	if err := seedRoundRobinTournament(ctx, conn, spaceID, aliceID, []string{alicePlayer, bobPlayer, pappaPlayer, ingridPlayer}); err != nil {
		return err
	}
	extras := []string{auth.NewID(), auth.NewID(), auth.NewID(), auth.NewID()}
	for i, name := range []string{"Ellen", "Frank", "Göran", "Helga"} {
		if _, err := conn.ExecContext(ctx, `INSERT INTO players (id, space_id, display_name, created_at) VALUES (?, ?, ?, ?)`, extras[i], spaceID, name, now); err != nil {
			return err
		}
	}
	if err := seedBracketTournament(ctx, conn, spaceID, aliceID, append([]string{alicePlayer, bobPlayer, pappaPlayer, ingridPlayer}, extras...)); err != nil {
		return err
	}
	return nil
}

func seedMatch(ctx context.Context, conn *sql.DB, spaceID, createdBy, homePlayer, visitorPlayer string, bestOf, pointsToWin int, status string, games [][2]int) error {
	now := nowMS()
	matchID := auth.NewID()
	var winner any
	var completedAt any
	if status == "completed" {
		homeWins := 0
		visitorWins := 0
		for _, game := range games {
			if game[0] > game[1] {
				homeWins++
			} else {
				visitorWins++
			}
		}
		if homeWins > visitorWins {
			winner = "home"
		} else {
			winner = "visitor"
		}
		completedAt = now
	}
	if _, err := conn.ExecContext(ctx, `
INSERT INTO matches (id, space_id, kind, best_of, points_to_win, status, winner_side, started_at, completed_at, created_by, created_at, updated_at)
VALUES (?, ?, 'singles', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		matchID, spaceID, bestOf, pointsToWin, status, winner, now, completedAt, createdBy, now, now); err != nil {
		return err
	}
	if _, err := conn.ExecContext(ctx, `
INSERT INTO match_participants (match_id, player_id, side, slot) VALUES
(?, ?, 'home', 1),
(?, ?, 'visitor', 1)`,
		matchID, homePlayer, matchID, visitorPlayer); err != nil {
		return err
	}
	for index, score := range games {
		gameID := auth.NewID()
		gameStatus := "completed"
		var gameCompletedAt any = now
		if status != "completed" && index == len(games)-1 {
			gameStatus = "in_progress"
			gameCompletedAt = nil
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO games (id, match_id, game_number, home_score, visitor_score, status, started_at, completed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			gameID, matchID, index+1, score[0], score[1], gameStatus, now, gameCompletedAt); err != nil {
			return err
		}
		sequence := 0
		home, visitor := 0, 0
		for home < score[0] || visitor < score[1] {
			sequence++
			side := "home"
			if visitor < score[1] && (home >= score[0] || sequence%2 == 0) {
				visitor++
				side = "visitor"
			} else if home < score[0] {
				home++
			}
			if _, err := conn.ExecContext(ctx, `INSERT INTO score_events (id, game_id, sequence, scorer_side, home_score_after, visitor_score_after, occurred_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				auth.NewID(), gameID, sequence, side, home, visitor, now); err != nil {
				return err
			}
		}
	}
	return nil
}

func seedRoundRobinTournament(ctx context.Context, conn *sql.DB, spaceID, createdBy string, players []string) error {
	now := nowMS()
	tournamentID := auth.NewID()
	if _, err := conn.ExecContext(ctx, `INSERT INTO tournaments (id, space_id, name, format, best_of, points_to_win, status, created_by, created_at, updated_at) VALUES (?, ?, ?, 'round_robin', 3, 11, 'completed', ?, ?, ?)`,
		tournamentID, spaceID, "Spring League", createdBy, now, now); err != nil {
		return err
	}
	for i, playerID := range players {
		if _, err := conn.ExecContext(ctx, `INSERT INTO tournament_players (tournament_id, player_id, seed) VALUES (?, ?, ?)`, tournamentID, playerID, i+1); err != nil {
			return err
		}
	}
	pairs := [][2]string{{players[0], players[1]}, {players[0], players[2]}, {players[0], players[3]}, {players[1], players[2]}, {players[1], players[3]}, {players[2], players[3]}}
	for _, pair := range pairs {
		if err := seedTournamentMatch(ctx, conn, spaceID, createdBy, tournamentID, "round_robin", nil, pair[0], pair[1], "completed", [][2]int{{11, 7}, {11, 9}}); err != nil {
			return err
		}
	}
	_, err := conn.ExecContext(ctx, `UPDATE tournaments SET winner_player_id = ?, updated_at = ? WHERE id = ?`, players[0], now, tournamentID)
	return err
}

func seedBracketTournament(ctx context.Context, conn *sql.DB, spaceID, createdBy string, players []string) error {
	now := nowMS()
	tournamentID := auth.NewID()
	if _, err := conn.ExecContext(ctx, `INSERT INTO tournaments (id, space_id, name, format, best_of, points_to_win, status, created_by, created_at, updated_at) VALUES (?, ?, ?, 'bracket', 5, 11, 'in_progress', ?, ?, ?)`,
		tournamentID, spaceID, "Cup Clash", createdBy, now, now); err != nil {
		return err
	}
	for i, playerID := range players {
		if _, err := conn.ExecContext(ctx, `INSERT INTO tournament_players (tournament_id, player_id, seed) VALUES (?, ?, ?)`, tournamentID, playerID, i+1); err != nil {
			return err
		}
	}
	for i := 0; i < len(players); i += 2 {
		round := 1
		status := "completed"
		games := [][2]int{{11, 6}, {11, 5}, {11, 8}}
		if i == len(players)-2 {
			status = "in_progress"
			games = [][2]int{{6, 4}}
		}
		if err := seedTournamentMatch(ctx, conn, spaceID, createdBy, tournamentID, "bracket", &round, players[i], players[i+1], status, games); err != nil {
			return err
		}
	}
	for range 2 {
		round := 2
		if err := seedTournamentShell(ctx, conn, spaceID, createdBy, tournamentID, &round); err != nil {
			return err
		}
	}
	round := 3
	return seedTournamentShell(ctx, conn, spaceID, createdBy, tournamentID, &round)
}

func seedTournamentMatch(ctx context.Context, conn *sql.DB, spaceID, createdBy, tournamentID, phase string, round *int, home, visitor, status string, games [][2]int) error {
	now := nowMS()
	matchID := auth.NewID()
	var winner any
	var completedAt any
	if status == "completed" {
		if games[len(games)-1][0] > games[len(games)-1][1] {
			winner = "home"
		} else {
			winner = "visitor"
		}
		completedAt = now
	}
	if _, err := conn.ExecContext(ctx, `
INSERT INTO matches (id, space_id, kind, tournament_id, tournament_phase, tournament_bracket_round, best_of, points_to_win, status, winner_side, started_at, completed_at, created_by, created_at, updated_at)
VALUES (?, ?, 'singles', ?, ?, ?, 3, 11, ?, ?, ?, ?, ?, ?, ?)`,
		matchID, spaceID, tournamentID, phase, nullableInt(round), status, winner, now, completedAt, createdBy, now, now); err != nil {
		return err
	}
	if home != "" {
		if _, err := conn.ExecContext(ctx, `INSERT INTO match_participants (match_id, player_id, side, slot) VALUES (?, ?, 'home', 1)`, matchID, home); err != nil {
			return err
		}
	}
	if visitor != "" {
		if _, err := conn.ExecContext(ctx, `INSERT INTO match_participants (match_id, player_id, side, slot) VALUES (?, ?, 'visitor', 1)`, matchID, visitor); err != nil {
			return err
		}
	}
	for index, score := range games {
		gameID := auth.NewID()
		gameStatus := "completed"
		if status != "completed" && index == len(games)-1 {
			gameStatus = "in_progress"
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO games (id, match_id, game_number, home_score, visitor_score, status, started_at, completed_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			gameID, matchID, index+1, score[0], score[1], gameStatus, now, nullableCompleted(gameStatus, now)); err != nil {
			return err
		}
	}
	return nil
}

func seedTournamentShell(ctx context.Context, conn *sql.DB, spaceID, createdBy, tournamentID string, round *int) error {
	now := nowMS()
	matchID := auth.NewID()
	if _, err := conn.ExecContext(ctx, `
INSERT INTO matches (id, space_id, kind, tournament_id, tournament_phase, tournament_bracket_round, best_of, points_to_win, status, started_at, created_by, created_at, updated_at)
VALUES (?, ?, 'singles', ?, 'bracket', ?, 5, 11, 'in_progress', ?, ?, ?, ?)`,
		matchID, spaceID, tournamentID, nullableInt(round), now, createdBy, now, now); err != nil {
		return err
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO games (id, match_id, game_number, status, started_at) VALUES (?, ?, 1, 'in_progress', ?)`, auth.NewID(), matchID, now)
	return err
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableCompleted(status string, ts int64) any {
	if status == "completed" {
		return ts
	}
	return nil
}

func nowMS() int64 {
	return time.Now().UnixMilli()
}

func randomJoinCode() string {
	id := strings.ToLower(auth.NewID())
	return id[len(id)-8:]
}
