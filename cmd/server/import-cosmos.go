package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/samuelstrom93/pingit/internal/auth"
)

const (
	cosmosTournamentFile = "002fffff-d6c9-40b9-80fa-e9d8b8ce9f34.json"
	cosmosCamelFile      = "6d7de665-6b0c-4f3e-8920-cf4ad7b41d8d.json"
	cosmosPascalFile     = "de42384c-4414-45ad-aef1-46803c3cc234.json"
)

// cosmosMatch is a typed view of a legacy match document. Tags are PascalCase
// because the camelCase variant gets pre-normalized before unmarshal.
type cosmosMatch struct {
	ID        string       `json:"id"`
	Date      string       `json:"Date"`
	PlayerOne string       `json:"PlayerOne"`
	PlayerTwo string       `json:"PlayerTwo"`
	TeamOne   []string     `json:"TeamOne"`
	TeamTwo   []string     `json:"TeamTwo"`
	Games     []cosmosGame `json:"Games"`
	GameType  int          `json:"GameType"`
	SetAmount int          `json:"SetAmount"`
}

type cosmosGame struct {
	HomeScore    int             `json:"HomeScore"`
	VisitorScore int             `json:"VisitorScore"`
	Score        json.RawMessage `json:"Score"`
}

type cosmosScoreEvent struct {
	Timestamp int64  `json:"Timestamp"`
	Scorer    string `json:"Scorer"`
}

type cosmosTournament struct {
	ID             string                   `json:"id"`
	Date           string                   `json:"Date"`
	TournamentName string                   `json:"TournamentName"`
	GameType       int                      `json:"GameType"`
	SetAmount      int                      `json:"SetAmount"`
	HasGroupStage  bool                     `json:"HasGroupStage"`
	Players        []cosmosTournamentPlayer `json:"Players"`
	Groups         []cosmosGroup            `json:"Groups"`
	Rounds         []cosmosRound            `json:"Rounds"`
}

type cosmosTournamentPlayer struct {
	Name         string `json:"Name"`
	IsEliminated bool   `json:"IsEliminated"`
}

type cosmosGroup struct {
	GroupID int           `json:"GroupId"`
	Matches []cosmosMatch `json:"Matches"`
}

type cosmosRound struct {
	Round             int                     `json:"Round"`
	TournamentMatches []cosmosTournamentMatch `json:"TournamentMatches"`
}

type cosmosTournamentMatch struct {
	Bracket    int          `json:"Bracket"`
	IsWalkover bool         `json:"IsWalkover"`
	PlayerOne  string       `json:"PlayerOne"`
	PlayerTwo  string       `json:"PlayerTwo"`
	Date       string       `json:"Date"`
	Games      []cosmosGame `json:"Games"`
	GameType   int          `json:"GameType"`
	SetAmount  int          `json:"SetAmount"`
}

type importSummary struct {
	LinkedPlayers     int
	ManagedPlayers    int
	Tournaments       int
	TournamentSkipped int
	GroupMatches      int
	BracketMatches    int
	SinglesMatches    int
	DoublesMatches    int
	ScoreEvents       int
	SkippedByContent  int
	SkippedByLegacyID int
}

func importCosmos(ctx context.Context, conn *sql.DB, args []string) error {
	fs := flag.NewFlagSet("import-cosmos", flag.ExitOnError)
	dirFlag := fs.String("dir", "", "directory containing the Cosmos JSON files (required)")
	emailFlag := fs.String("user-email", "", "operator user email; rows are attributed to this user (required)")
	spaceFlag := fs.String("space-name", "Elicit", "target space name; created if missing")
	dryRunFlag := fs.Bool("dry-run", false, "parse and insert into a transaction but roll back at the end")
	fs.SetOutput(os.Stdout)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dirFlag == "" {
		fs.Usage()
		return errors.New("--dir is required")
	}
	if *emailFlag == "" {
		fs.Usage()
		return errors.New("--user-email is required")
	}

	operatorID, err := lookupOperator(ctx, conn, *emailFlag)
	if err != nil {
		return err
	}

	tournament, err := loadTournament(*dirFlag)
	if err != nil {
		return fmt.Errorf("load tournament file: %w", err)
	}
	camelMatches, err := loadStandaloneMatches(*dirFlag, cosmosCamelFile, true)
	if err != nil {
		return fmt.Errorf("load standalone matches (%s): %w", cosmosCamelFile, err)
	}
	pascalMatches, err := loadStandaloneMatches(*dirFlag, cosmosPascalFile, false)
	if err != nil {
		return fmt.Errorf("load standalone matches (%s): %w", cosmosPascalFile, err)
	}
	standalone := append([]cosmosMatch{}, camelMatches...)
	standalone = append(standalone, pascalMatches...)

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	now := time.Now().UnixMilli()
	spaceID, err := ensureSpace(ctx, tx, *spaceFlag, operatorID, now)
	if err != nil {
		return fmt.Errorf("ensure space: %w", err)
	}

	names, err := collectPlayerNames(tournament, standalone)
	if err != nil {
		return err
	}
	playerIDs, linked, managed, err := upsertPlayers(ctx, tx, spaceID, names, operatorID, now)
	if err != nil {
		return fmt.Errorf("upsert players: %w", err)
	}

	summary := importSummary{LinkedPlayers: linked, ManagedPlayers: managed}

	if tournament != nil {
		if err := importTournament(ctx, tx, tournament, spaceID, operatorID, playerIDs, &summary); err != nil {
			return fmt.Errorf("import tournament: %w", err)
		}
	}

	if err := importStandalone(ctx, tx, standalone, spaceID, operatorID, playerIDs, &summary); err != nil {
		return fmt.Errorf("import standalone matches: %w", err)
	}

	prefix := ""
	if *dryRunFlag {
		prefix = "[dry-run] "
	} else {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit: %w", err)
		}
		committed = true
	}

	fmt.Printf("%simport summary:\n", prefix)
	fmt.Printf("  %splayers: %d linked, %d managed\n", prefix, summary.LinkedPlayers, summary.ManagedPlayers)
	fmt.Printf("  %stournaments: %d imported, %d skipped (already present)\n", prefix, summary.Tournaments, summary.TournamentSkipped)
	fmt.Printf("  %stournament matches: %d group, %d bracket\n", prefix, summary.GroupMatches, summary.BracketMatches)
	fmt.Printf("  %sstandalone matches: %d singles, %d doubles\n", prefix, summary.SinglesMatches, summary.DoublesMatches)
	fmt.Printf("  %sscore events: %d\n", prefix, summary.ScoreEvents)
	fmt.Printf("  %sskipped: %d by content, %d by legacy_cosmos_id\n", prefix, summary.SkippedByContent, summary.SkippedByLegacyID)
	return nil
}

func lookupOperator(ctx context.Context, conn *sql.DB, email string) (string, error) {
	var id string
	err := conn.QueryRowContext(ctx, `SELECT id FROM users WHERE email = ?`, email).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", fmt.Errorf("operator user with email %q not found", email)
	}
	if err != nil {
		return "", fmt.Errorf("look up operator: %w", err)
	}
	return id, nil
}

func loadStandaloneMatches(dir, name string, normalize bool) ([]cosmosMatch, error) {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	if normalize {
		data = normalizeJSON(data)
	}
	var matches []cosmosMatch
	if err := json.Unmarshal(data, &matches); err != nil {
		return nil, fmt.Errorf("unmarshal matches: %w", err)
	}
	return matches, nil
}

func loadTournament(dir string) (*cosmosTournament, error) {
	path := filepath.Join(dir, cosmosTournamentFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	// File can be either an array-of-one or a bare object.
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) > 0 && trimmed[0] == '[' {
		var arr []cosmosTournament
		if err := json.Unmarshal(data, &arr); err != nil {
			return nil, fmt.Errorf("unmarshal tournament array: %w", err)
		}
		if len(arr) == 0 {
			return nil, nil
		}
		return &arr[0], nil
	}
	var t cosmosTournament
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("unmarshal tournament: %w", err)
	}
	return &t, nil
}

// normalizeJSON rewrites the six known camelCase keys used in the legacy
// standalone match file into their PascalCase equivalents so a single typed
// struct can decode both formats.
var camelToPascalKeys = []struct{ from, to string }{
	{`"playerOne":`, `"PlayerOne":`},
	{`"playerTwo":`, `"PlayerTwo":`},
	{`"teamOne":`, `"TeamOne":`},
	{`"teamTwo":`, `"TeamTwo":`},
	{`"date":`, `"Date":`},
	{`"games":`, `"Games":`},
	{`"gameType":`, `"GameType":`},
	{`"setAmount":`, `"SetAmount":`},
}

func normalizeJSON(data []byte) []byte {
	out := data
	for _, pair := range camelToPascalKeys {
		out = bytes.ReplaceAll(out, []byte(pair.from), []byte(pair.to))
	}
	return out
}

func ensureSpace(ctx context.Context, tx *sql.Tx, name, operatorID string, now int64) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM spaces WHERE name = ? AND deleted_at IS NULL`, name).Scan(&id)
	if err == nil {
		// ensure operator is admin
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?, ?, 'admin', ?)
			 ON CONFLICT(space_id, user_id) DO UPDATE SET role='admin'`,
			id, operatorID, now); err != nil {
			return "", err
		}
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id = auth.NewID()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO spaces (id, name, is_invite_only, created_by, created_at) VALUES (?, ?, 1, ?, ?)`,
		id, name, operatorID, now); err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO space_members (space_id, user_id, role, joined_at) VALUES (?, ?, 'admin', ?)`,
		id, operatorID, now); err != nil {
		return "", err
	}
	return id, nil
}

func collectPlayerNames(tournament *cosmosTournament, standalone []cosmosMatch) ([]string, error) {
	set := map[string]struct{}{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" {
			set[s] = struct{}{}
		}
	}
	if tournament != nil {
		for _, p := range tournament.Players {
			add(p.Name)
		}
		for _, g := range tournament.Groups {
			for _, m := range g.Matches {
				add(m.PlayerOne)
				add(m.PlayerTwo)
			}
		}
		for _, r := range tournament.Rounds {
			for _, m := range r.TournamentMatches {
				add(m.PlayerOne)
				add(m.PlayerTwo)
			}
		}
	}
	for _, m := range standalone {
		add(m.PlayerOne)
		add(m.PlayerTwo)
		for _, n := range m.TeamOne {
			add(n)
		}
		for _, n := range m.TeamTwo {
			add(n)
		}
	}
	if len(set) == 0 {
		return nil, errors.New("no player names found in source files")
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func upsertPlayers(ctx context.Context, tx *sql.Tx, spaceID string, names []string, operatorID string, now int64) (map[string]string, int, int, error) {
	const samuel = "Samuel Ström"
	playerIDs := make(map[string]string, len(names))
	linked := 0
	managed := 0
	for _, name := range names {
		id := auth.NewID()
		var userID any
		if name == samuel {
			userID = operatorID
		} else {
			userID = nil
		}
		_, err := tx.ExecContext(ctx,
			`INSERT INTO players (id, space_id, display_name, user_id, created_at)
			 VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT(space_id, display_name) DO NOTHING`,
			id, spaceID, name, userID, now)
		if err != nil {
			return nil, 0, 0, err
		}
		var existingID string
		var existingUser sql.NullString
		err = tx.QueryRowContext(ctx,
			`SELECT id, user_id FROM players WHERE space_id = ? AND display_name = ?`,
			spaceID, name).Scan(&existingID, &existingUser)
		if err != nil {
			return nil, 0, 0, fmt.Errorf("re-select player %q: %w", name, err)
		}
		if name == samuel && (!existingUser.Valid || existingUser.String != operatorID) {
			if _, err := tx.ExecContext(ctx,
				`UPDATE players SET user_id = ? WHERE id = ?`, operatorID, existingID); err != nil {
				return nil, 0, 0, err
			}
			existingUser = sql.NullString{String: operatorID, Valid: true}
		}
		playerIDs[name] = existingID
		if existingUser.Valid {
			linked++
		} else {
			managed++
		}
	}
	return playerIDs, linked, managed, nil
}

func importTournament(ctx context.Context, tx *sql.Tx, t *cosmosTournament, spaceID, operatorID string, players map[string]string, summary *importSummary) error {
	if t.ID == "" {
		return errors.New("tournament document missing id")
	}
	var existingID string
	err := tx.QueryRowContext(ctx, `SELECT id FROM tournaments WHERE legacy_cosmos_id = ?`, t.ID).Scan(&existingID)
	if err == nil {
		summary.TournamentSkipped++
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	tDate, err := parseDateMS(t.Date)
	if err != nil {
		return fmt.Errorf("parse tournament date %q: %w", t.Date, err)
	}
	winnerID := winningTournamentPlayerID(t, players)
	tournamentID := auth.NewID()
	var winner any
	if winnerID != "" {
		winner = winnerID
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO tournaments (id, space_id, name, format, best_of, points_to_win, status, winner_player_id, created_by, created_at, updated_at, legacy_cosmos_id)
		 VALUES (?, ?, ?, 'groups_knockout', ?, 11, 'completed', ?, ?, ?, ?, ?)`,
		tournamentID, spaceID, t.TournamentName, max(1, t.SetAmount), winner, operatorID, tDate, tDate, t.ID); err != nil {
		return err
	}
	summary.Tournaments++

	for _, p := range t.Players {
		name := strings.TrimSpace(p.Name)
		pid, ok := players[name]
		if !ok {
			return fmt.Errorf("tournament player %q not found in upsert map", name)
		}
		var elim any
		if p.IsEliminated {
			elim = tDate
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO tournament_players (tournament_id, player_id, eliminated_at) VALUES (?, ?, ?)`,
			tournamentID, pid, elim); err != nil {
			return err
		}
	}

	for _, g := range t.Groups {
		groupID := strconv.Itoa(g.GroupID)
		for _, m := range g.Matches {
			match := m
			if match.Date == "" {
				match.Date = t.Date
			}
			if err := insertMatch(ctx, tx, insertMatchArgs{
				space:           spaceID,
				operator:        operatorID,
				players:         players,
				match:           &match,
				tournamentID:    &tournamentID,
				tournamentPhase: ptr("group"),
				tournamentGroup: &groupID,
				summary:         summary,
				role:            "tournament-group",
			}); err != nil {
				return err
			}
		}
	}

	for _, r := range t.Rounds {
		round := r.Round
		for _, tm := range r.TournamentMatches {
			match := cosmosMatch{
				Date:      tm.Date,
				PlayerOne: tm.PlayerOne,
				PlayerTwo: tm.PlayerTwo,
				Games:     tm.Games,
				GameType:  tm.GameType,
				SetAmount: tm.SetAmount,
			}
			if match.Date == "" {
				match.Date = t.Date
			}
			if err := insertMatch(ctx, tx, insertMatchArgs{
				space:                spaceID,
				operator:             operatorID,
				players:              players,
				match:                &match,
				tournamentID:         &tournamentID,
				tournamentPhase:      ptr("knockout"),
				tournamentBracketRnd: &round,
				summary:              summary,
				role:                 "tournament-bracket",
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

func winningTournamentPlayerID(t *cosmosTournament, players map[string]string) string {
	for _, p := range t.Players {
		if !p.IsEliminated {
			if id, ok := players[strings.TrimSpace(p.Name)]; ok {
				return id
			}
		}
	}
	return ""
}

func importStandalone(ctx context.Context, tx *sql.Tx, matches []cosmosMatch, spaceID, operatorID string, players map[string]string, summary *importSummary) error {
	seen := map[string]struct{}{}
	for i := range matches {
		match := matches[i]
		fp := matchFingerprint(match)
		if _, dup := seen[fp]; dup {
			summary.SkippedByContent++
			continue
		}
		seen[fp] = struct{}{}
		if err := insertMatch(ctx, tx, insertMatchArgs{
			space:    spaceID,
			operator: operatorID,
			players:  players,
			match:    &match,
			summary:  summary,
			role:     "standalone",
		}); err != nil {
			return err
		}
	}
	return nil
}

type insertMatchArgs struct {
	space                string
	operator             string
	players              map[string]string
	match                *cosmosMatch
	tournamentID         *string
	tournamentPhase      *string
	tournamentGroup      *string
	tournamentBracketRnd *int
	summary              *importSummary
	role                 string // "standalone", "tournament-group", "tournament-bracket"
}

func insertMatch(ctx context.Context, tx *sql.Tx, a insertMatchArgs) error {
	m := a.match
	kind, err := mapKind(m.GameType, m.ID)
	if err != nil {
		return err
	}
	parsed, err := parseDateMS(m.Date)
	if err != nil {
		return fmt.Errorf("parse match date %q (id=%s): %w", m.Date, m.ID, err)
	}
	startedAt := parsed
	completedAt := parsed
	winSide, status := winnerForGames(m.Games)
	bestOf := bestOfForGames(m.Games)
	pointsToWin := pointsToWinForGames(m.Games)
	hasEvents, eventsByGame, firstEventTS, lastEventTS, eventErr := preparseScoreEvents(m, a.players)
	if eventErr != nil {
		return fmt.Errorf("parse score events (id=%s): %w", m.ID, eventErr)
	}
	if hasEvents {
		startedAt = firstEventTS
		completedAt = lastEventTS
	}

	matchID := auth.NewID()
	var legacyID any
	if a.role == "standalone" && m.ID != "" {
		legacyID = m.ID
	}
	var winnerArg any
	if winSide != "" {
		winnerArg = winSide
	}
	var completedArg any
	if status == "completed" {
		completedArg = completedAt
	}
	res, err := tx.ExecContext(ctx, `
INSERT INTO matches (
  id, space_id, kind, tournament_id, tournament_phase, tournament_group_id, tournament_bracket_round,
  best_of, points_to_win, status, winner_side, started_at, completed_at, created_by, created_at, updated_at, legacy_cosmos_id
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(legacy_cosmos_id) DO NOTHING`,
		matchID, a.space, kind, nullableString(a.tournamentID), nullableString(a.tournamentPhase), nullableString(a.tournamentGroup), nullableIntPtr(a.tournamentBracketRnd),
		bestOf, pointsToWin, status, winnerArg, startedAt, completedArg, a.operator, startedAt, startedAt, legacyID,
	)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		// legacy_cosmos_id collision (idempotent re-run)
		a.summary.SkippedByLegacyID++
		return nil
	}

	if err := insertParticipants(ctx, tx, matchID, kind, m, a.players); err != nil {
		return err
	}
	if err := insertGames(ctx, tx, matchID, m, hasEvents, eventsByGame, startedAt, completedAt, status, a.summary); err != nil {
		return err
	}

	switch a.role {
	case "tournament-group":
		a.summary.GroupMatches++
	case "tournament-bracket":
		a.summary.BracketMatches++
	default:
		if kind == "doubles" {
			a.summary.DoublesMatches++
		} else {
			a.summary.SinglesMatches++
		}
	}
	return nil
}

func mapKind(gameType int, id string) (string, error) {
	switch gameType {
	case 0, 2:
		// 0 = casual singles; 2 = tournament-play singles in the legacy dataset.
		return "singles", nil
	case 1:
		return "doubles", nil
	default:
		return "", fmt.Errorf("unsupported gameType %d on document %q", gameType, id)
	}
}

func bestOfForGames(games []cosmosGame) int {
	n := len(games)
	if n <= 1 {
		return 1
	}
	for _, target := range []int{1, 3, 5, 7} {
		if n <= target {
			return target
		}
	}
	return n
}

func pointsToWinForGames(games []cosmosGame) int {
	for _, g := range games {
		if g.HomeScore >= 10 || g.VisitorScore >= 10 {
			return 11
		}
	}
	return 5
}

func winnerForGames(games []cosmosGame) (string, string) {
	if len(games) == 0 {
		return "", "abandoned"
	}
	home, visitor := 0, 0
	for _, g := range games {
		if g.HomeScore > g.VisitorScore {
			home++
		} else if g.VisitorScore > g.HomeScore {
			visitor++
		}
	}
	if home == visitor {
		return "", "abandoned"
	}
	if home > visitor {
		return "home", "completed"
	}
	return "visitor", "completed"
}

func insertParticipants(ctx context.Context, tx *sql.Tx, matchID, kind string, m *cosmosMatch, players map[string]string) error {
	resolve := func(name string) (string, error) {
		name = strings.TrimSpace(name)
		if name == "" {
			return "", fmt.Errorf("empty participant name on match id=%s", m.ID)
		}
		id, ok := players[name]
		if !ok {
			return "", fmt.Errorf("participant %q on match id=%s not in upsert map", name, m.ID)
		}
		return id, nil
	}
	if kind == "doubles" {
		if len(m.TeamOne) != 2 || len(m.TeamTwo) != 2 {
			return fmt.Errorf("doubles match id=%s expects 2 players per team, got home=%d visitor=%d", m.ID, len(m.TeamOne), len(m.TeamTwo))
		}
		players := []struct {
			name string
			side string
			slot int
		}{
			{m.TeamOne[0], "home", 1},
			{m.TeamOne[1], "home", 2},
			{m.TeamTwo[0], "visitor", 1},
			{m.TeamTwo[1], "visitor", 2},
		}
		for _, p := range players {
			pid, err := resolve(p.name)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx,
				`INSERT INTO match_participants (match_id, player_id, side, slot) VALUES (?, ?, ?, ?)`,
				matchID, pid, p.side, p.slot); err != nil {
				return err
			}
		}
		return nil
	}
	homeID, err := resolve(m.PlayerOne)
	if err != nil {
		return err
	}
	visitorID, err := resolve(m.PlayerTwo)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO match_participants (match_id, player_id, side, slot) VALUES (?, ?, 'home', 1), (?, ?, 'visitor', 1)`,
		matchID, homeID, matchID, visitorID); err != nil {
		return err
	}
	return nil
}

type gameEventBundle struct {
	gameNumber int
	events     []scoreEventTuple
}

type scoreEventTuple struct {
	side         string
	homeAfter    int
	visitorAfter int
	occurredAt   int64
}

func preparseScoreEvents(m *cosmosMatch, players map[string]string) (bool, map[int][]scoreEventTuple, int64, int64, error) {
	out := map[int][]scoreEventTuple{}
	hasAny := false
	var firstTS, lastTS int64
	for i, g := range m.Games {
		raw := bytes.TrimSpace(g.Score)
		if len(raw) == 0 || raw[0] != '[' {
			continue
		}
		var events []cosmosScoreEvent
		if err := json.Unmarshal(raw, &events); err != nil {
			// Score may be an object map ({}) instead of array; skip silently.
			continue
		}
		if len(events) == 0 {
			continue
		}
		hasAny = true
		bundle := make([]scoreEventTuple, 0, len(events))
		home, visitor := 0, 0
		for _, e := range events {
			side, err := scorerSide(e.Scorer, m)
			if err != nil {
				return false, nil, 0, 0, err
			}
			if side == "home" {
				home++
			} else {
				visitor++
			}
			bundle = append(bundle, scoreEventTuple{
				side:         side,
				homeAfter:    home,
				visitorAfter: visitor,
				occurredAt:   e.Timestamp,
			})
		}
		out[i] = bundle
		if firstTS == 0 || bundle[0].occurredAt < firstTS {
			firstTS = bundle[0].occurredAt
		}
		last := bundle[len(bundle)-1].occurredAt
		if last > lastTS {
			lastTS = last
		}
	}
	if !hasAny {
		return false, nil, 0, 0, nil
	}
	_ = players // reserved for future doubles-side resolution
	return true, out, firstTS, lastTS, nil
}

func scorerSide(name string, m *cosmosMatch) (string, error) {
	name = strings.TrimSpace(name)
	if m.GameType == 1 {
		for _, t := range m.TeamOne {
			if strings.TrimSpace(t) == name {
				return "home", nil
			}
		}
		for _, t := range m.TeamTwo {
			if strings.TrimSpace(t) == name {
				return "visitor", nil
			}
		}
		return "", fmt.Errorf("scorer %q not in either doubles team (match id=%s)", name, m.ID)
	}
	if strings.TrimSpace(m.PlayerOne) == name {
		return "home", nil
	}
	if strings.TrimSpace(m.PlayerTwo) == name {
		return "visitor", nil
	}
	return "", fmt.Errorf("scorer %q matches neither player (match id=%s)", name, m.ID)
}

func insertGames(ctx context.Context, tx *sql.Tx, matchID string, m *cosmosMatch, hasEvents bool, events map[int][]scoreEventTuple, matchStart, matchEnd int64, status string, summary *importSummary) error {
	for i, g := range m.Games {
		gameID := auth.NewID()
		gameStatus := "completed"
		if status != "completed" && i == len(m.Games)-1 {
			gameStatus = "in_progress"
		}
		gameStart := matchStart
		var gameEnd any = matchEnd
		if hasEvents {
			if bundle, ok := events[i]; ok && len(bundle) > 0 {
				gameStart = bundle[0].occurredAt
				gameEnd = bundle[len(bundle)-1].occurredAt
			}
		}
		if gameStatus != "completed" {
			gameEnd = nil
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO games (id, match_id, game_number, home_score, visitor_score, status, started_at, completed_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			gameID, matchID, i+1, g.HomeScore, g.VisitorScore, gameStatus, gameStart, gameEnd); err != nil {
			return err
		}
		if hasEvents {
			if bundle, ok := events[i]; ok {
				for j, ev := range bundle {
					if _, err := tx.ExecContext(ctx,
						`INSERT INTO score_events (id, game_id, sequence, scorer_side, home_score_after, visitor_score_after, occurred_at)
						 VALUES (?, ?, ?, ?, ?, ?, ?)`,
						auth.NewID(), gameID, j+1, ev.side, ev.homeAfter, ev.visitorAfter, ev.occurredAt); err != nil {
						return err
					}
					summary.ScoreEvents++
				}
			}
		}
	}
	return nil
}

// matchFingerprint identifies same-content matches across files. Used for
// content-level dedup of standalone rows; not used for tournament matches.
func matchFingerprint(m cosmosMatch) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(m.Date))
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(m.GameType))
	b.WriteByte('|')
	b.WriteString(strings.TrimSpace(m.PlayerOne))
	b.WriteByte('|')
	b.WriteString(strings.TrimSpace(m.PlayerTwo))
	b.WriteByte('|')
	for _, n := range m.TeamOne {
		b.WriteString(strings.TrimSpace(n))
		b.WriteByte(',')
	}
	b.WriteByte('|')
	for _, n := range m.TeamTwo {
		b.WriteString(strings.TrimSpace(n))
		b.WriteByte(',')
	}
	b.WriteByte('|')
	for _, g := range m.Games {
		b.WriteString(strconv.Itoa(g.HomeScore))
		b.WriteByte('-')
		b.WriteString(strconv.Itoa(g.VisitorScore))
		b.WriteByte(';')
	}
	return b.String()
}

var legacyNaiveLocation = mustLoadLocation("Europe/Stockholm")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return loc
}

func parseDateMS(raw string) (int64, error) {
	if raw == "" {
		return 0, errors.New("empty date")
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999999Z07:00"}
	for _, layout := range layouts {
		t, err := time.Parse(layout, raw)
		if err == nil {
			return t.UnixMilli(), nil
		}
	}
	// Legacy Cosmos data occasionally omits timezone; treat as Europe/Stockholm
	// since 85% of timezone-bearing entries use that offset.
	naiveLayouts := []string{"2006-01-02T15:04:05.999999999", "2006-01-02T15:04:05"}
	for _, layout := range naiveLayouts {
		t, err := time.ParseInLocation(layout, raw, legacyNaiveLocation)
		if err == nil {
			return t.UnixMilli(), nil
		}
	}
	return 0, fmt.Errorf("unrecognized date format %q", raw)
}

func nullableString(v *string) any {
	if v == nil {
		return nil
	}
	return *v
}

func nullableIntPtr(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func ptr[T any](v T) *T { return &v }
