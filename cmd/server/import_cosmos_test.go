package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"

	pingit "github.com/samuelstrom93/pingit"
	"github.com/samuelstrom93/pingit/internal/auth"
	"github.com/samuelstrom93/pingit/internal/db"
	_ "modernc.org/sqlite"
)

const testFixtureDir = "testdata/cosmos"

func newTestConn(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", "file::memory:?cache=shared&_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.Migrate(conn, pingit.MigrationsFS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return conn
}

func seedOperator(t *testing.T, conn *sql.DB) (string, string) {
	t.Helper()
	uid := auth.NewID()
	email := "operator@example.com"
	if _, err := conn.ExecContext(context.Background(),
		`INSERT INTO users (id, email, display_name, is_super_admin, created_at) VALUES (?, ?, 'Operator', 0, 1700000000000)`,
		uid, email); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	return uid, email
}

func count(t *testing.T, conn *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := conn.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count %q: %v", query, err)
	}
	return n
}

func TestImportCosmos_Smoke(t *testing.T) {
	conn := newTestConn(t)
	_, email := seedOperator(t, conn)

	args := []string{"--dir", testFixtureDir, "--user-email", email}
	if err := importCosmos(context.Background(), conn, args); err != nil {
		t.Fatalf("import: %v", err)
	}

	if got := count(t, conn, `SELECT COUNT(*) FROM spaces WHERE name = 'Elicit'`); got != 1 {
		t.Errorf("spaces named Elicit = %d, want 1", got)
	}

	// 6 unique trimmed names: Andreas Carlsson, Samuel Ström, Fredrik Karlsson,
	// André Pontes, Daniel Ferenczi, Patrik Johansson.
	if got := count(t, conn, `SELECT COUNT(*) FROM players`); got != 6 {
		t.Errorf("players = %d, want 6", got)
	}
	// Samuel Ström should be linked to the operator.
	if got := count(t, conn, `SELECT COUNT(*) FROM players WHERE display_name = 'Samuel Ström' AND user_id IS NOT NULL`); got != 1 {
		t.Errorf("linked Samuel Ström = %d, want 1", got)
	}
	// Trimming check: "André Pontes " (trailing space) should land as "André Pontes".
	if got := count(t, conn, `SELECT COUNT(*) FROM players WHERE display_name = 'André Pontes'`); got != 1 {
		t.Errorf("trimmed André Pontes = %d, want 1", got)
	}

	if got := count(t, conn, `SELECT COUNT(*) FROM tournaments WHERE legacy_cosmos_id IS NOT NULL`); got != 1 {
		t.Errorf("tournaments imported = %d, want 1", got)
	}
	if got := count(t, conn, `SELECT COUNT(*) FROM tournament_players`); got != 3 {
		t.Errorf("tournament_players = %d, want 3", got)
	}
	if got := count(t, conn, `SELECT COUNT(*) FROM tournament_players WHERE eliminated_at IS NOT NULL`); got != 2 {
		t.Errorf("eliminated tournament_players = %d, want 2", got)
	}

	if got := count(t, conn, `SELECT COUNT(*) FROM matches WHERE tournament_phase = 'group'`); got != 3 {
		t.Errorf("group matches = %d, want 3", got)
	}
	if got := count(t, conn, `SELECT COUNT(*) FROM matches WHERE tournament_phase = 'knockout' AND tournament_bracket_round = 1`); got != 1 {
		t.Errorf("bracket round-1 matches = %d, want 1", got)
	}

	// Standalone after content dedup: 4 from camel + 2 from pascal (66666 dropped).
	if got := count(t, conn, `SELECT COUNT(*) FROM matches WHERE legacy_cosmos_id IS NOT NULL AND tournament_id IS NULL`); got != 6 {
		t.Errorf("standalone matches imported = %d, want 6", got)
	}

	// Doubles vs singles split among standalone.
	if got := count(t, conn, `SELECT COUNT(*) FROM matches WHERE kind = 'doubles' AND tournament_id IS NULL`); got != 1 {
		t.Errorf("standalone doubles = %d, want 1", got)
	}
	if got := count(t, conn, `SELECT COUNT(*) FROM matches WHERE kind = 'singles' AND tournament_id IS NULL`); got != 5 {
		t.Errorf("standalone singles = %d, want 5", got)
	}

	// Doubles match has 4 participants; singles standalone has 2 each.
	doublesMatch := singleString(t, conn, `SELECT id FROM matches WHERE kind = 'doubles' LIMIT 1`)
	if got := count(t, conn, `SELECT COUNT(*) FROM match_participants WHERE match_id = ?`, doublesMatch); got != 4 {
		t.Errorf("doubles participants = %d, want 4", got)
	}
	if got := count(t, conn, `SELECT COUNT(*) FROM match_participants WHERE match_id IN (SELECT id FROM matches WHERE kind = 'singles' AND tournament_id IS NULL)`); got != 5*2 {
		t.Errorf("singles standalone participants = %d, want 10", got)
	}

	// Score events: 5 from match 44444444.
	if got := count(t, conn, `SELECT COUNT(*) FROM score_events`); got != 5 {
		t.Errorf("score_events = %d, want 5", got)
	}
	// Last event's home_score_after should be 3, visitor 2 (replay of 5 events).
	if got := count(t, conn, `SELECT COUNT(*) FROM score_events WHERE sequence = 5 AND home_score_after = 3 AND visitor_score_after = 2`); got != 1 {
		t.Errorf("final replay state = %d, want 1", got)
	}

	// Migration check: an "illegal" insert (best_of=2, points_to_win=7) should now succeed.
	mid := auth.NewID()
	if _, err := conn.ExecContext(context.Background(),
		`INSERT INTO matches (id, space_id, kind, best_of, points_to_win, status, started_at, created_by, created_at, updated_at)
		 VALUES (?, (SELECT id FROM spaces WHERE name='Elicit'), 'singles', 2, 7, 'completed', 1, (SELECT id FROM users WHERE email = ?), 1, 1)`,
		mid, email); err != nil {
		t.Errorf("constraint-relaxed match insert should succeed: %v", err)
	}
}

func TestImportCosmos_Idempotent(t *testing.T) {
	conn := newTestConn(t)
	_, email := seedOperator(t, conn)

	args := []string{"--dir", testFixtureDir, "--user-email", email}
	if err := importCosmos(context.Background(), conn, args); err != nil {
		t.Fatalf("first import: %v", err)
	}
	before := snapshot(t, conn)
	if err := importCosmos(context.Background(), conn, args); err != nil {
		t.Fatalf("second import: %v", err)
	}
	after := snapshot(t, conn)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("idempotency violation:\n  before=%v\n  after=%v", before, after)
	}
}

func TestImportCosmos_DryRun(t *testing.T) {
	conn := newTestConn(t)
	_, email := seedOperator(t, conn)

	args := []string{"--dir", testFixtureDir, "--user-email", email, "--dry-run"}
	if err := importCosmos(context.Background(), conn, args); err != nil {
		t.Fatalf("dry-run import: %v", err)
	}
	if got := count(t, conn, `SELECT COUNT(*) FROM matches`); got != 0 {
		t.Errorf("dry-run committed: matches = %d, want 0", got)
	}
	if got := count(t, conn, `SELECT COUNT(*) FROM players`); got != 0 {
		t.Errorf("dry-run committed: players = %d, want 0", got)
	}
}

func TestImportCosmos_BadEmail(t *testing.T) {
	conn := newTestConn(t)
	args := []string{"--dir", testFixtureDir, "--user-email", "nobody@example.com"}
	if err := importCosmos(context.Background(), conn, args); err == nil {
		t.Fatal("expected error for unknown operator email, got nil")
	}
}

func TestNormalizeJSON(t *testing.T) {
	camel := []byte(`{"playerOne":"a","playerTwo":"b","date":"2022-01-01","games":[{"HomeScore":11,"VisitorScore":5}],"gameType":0,"setAmount":0}`)
	out := normalizeJSON(camel)
	var got cosmosMatch
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("unmarshal normalized: %v", err)
	}
	if got.PlayerOne != "a" || got.PlayerTwo != "b" || got.Date != "2022-01-01" {
		t.Errorf("normalized fields wrong: %+v", got)
	}
	if len(got.Games) != 1 || got.Games[0].HomeScore != 11 {
		t.Errorf("normalized games wrong: %+v", got.Games)
	}
}

func snapshot(t *testing.T, conn *sql.DB) map[string]int {
	t.Helper()
	tables := []string{"spaces", "space_members", "players", "tournaments", "tournament_players", "matches", "match_participants", "games", "score_events"}
	out := map[string]int{}
	for _, table := range tables {
		out[table] = count(t, conn, "SELECT COUNT(*) FROM "+table)
	}
	return out
}

func singleString(t *testing.T, conn *sql.DB, query string, args ...any) string {
	t.Helper()
	var s string
	if err := conn.QueryRowContext(context.Background(), query, args...).Scan(&s); err != nil {
		t.Fatalf("singleString %q: %v", query, err)
	}
	return s
}
