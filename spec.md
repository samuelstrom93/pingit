# Cosmos DB Import — Specification

## Overview

This spec describes a one-time import of historical table tennis data from a legacy Cosmos DB-backed app into Pingit's SQLite database. The legacy data covers Dec 2021 → Feb 2023 and includes 903 standalone matches (singles + a few doubles) plus one tournament ("Lucia 2022"). The import lands in the existing schema with minimal additions: a relaxed CHECK constraint on `matches.best_of`/`points_to_win`, and a new `legacy_cosmos_id` column on `matches` and `tournaments` for idempotent re-runs.

Source data lives as 9 JSON files exported from the Cosmos DB collection. They are inconsistent in casing, structure, and schema version — the import handles a single canonical subset.

## Goals

- Import the legacy match history into the running Pingit instance, preserving original `id`s as `legacy_cosmos_id`.
- Land all data under a single dedicated space named `"Elicit"`, owned by the operator's existing user.
- Auto-link the player `"Samuel Ström"` to the operator's user. All other players become managed players (`user_id = NULL`).
- Import the one tournament with full group-stage and bracket structure intact.
- Make the import idempotent: re-running against the same DB is a no-op (no duplicates).

## Non-Goals

- No round-trip back to Cosmos. One-way migration only.
- No live import endpoint exposed to other users. This is an operator-run CLI subcommand.
- No backfill of `score_events` beyond the single match where they exist in source.
- No automatic merging of legacy players with new Pingit users created later — that is a manual `UPDATE players` task.
- No preservation of email-group data (legacy app feature; out of scope for Pingit).
- No preservation of legacy user-profile records.

## Source Data Inventory

Files reside in `/Users/samue/.openclaw/media/inbound/`. The import only reads the files explicitly marked **keep** below.

| File | Content | Action |
|---|---|---|
| `6d7de665-...json` | 903 standalone matches (singles + 21 doubles), Dec 2021 → Feb 2023 | **keep** |
| `4f41d8ff-...` (octet-stream) | Byte-identical copy of the above | skip |
| `de42384c-...json` | 3 standalone matches (2 are exact duplicates of each other) | **keep, dedup** |
| `002fffff-...json` | "Lucia 2022" tournament (latest version, includes bracket round) | **keep** |
| `0d4687fa-...json` | "Lucia 2022" earlier dump | skip |
| `9e03853f-...json` | "Lucia 2022" camelCase variant | skip |
| `7196173b-...json` | "12312s" tournament — test garbage from 2024 | skip |
| `6a8d42d5-...json` | Email-group records (legacy app feature) | skip |
| `dc6bff6c-...json` | Operator's legacy user profile | skip |

## Core Concepts

- **Legacy match** — a Cosmos document representing a singles or doubles match, possibly multi-set, possibly with per-point `score_events`.
- **Legacy tournament** — a Cosmos document with `Players[]`, optional `Groups[]` (round-robin stage), and `Rounds[].TournamentMatches[]` (bracket stage).
- **Managed player** — a row in `players` with `user_id = NULL`. Has a name and is referenced by matches but is not yet linked to a Pingit user account.
- **Linked player** — a row in `players` with `user_id` set to a real user. Used here only for `"Samuel Ström"`.
- **Legacy ID** — the original Cosmos document `id`. Stored in new columns `matches.legacy_cosmos_id` and `tournaments.legacy_cosmos_id`. Source of idempotency.

## Detailed Design

### Schema migration (`002_legacy_import.sql`)

A new goose migration that:

1. Drops the existing CHECK constraints on `matches.best_of`, `matches.points_to_win`, `tournaments.best_of`, and `tournaments.points_to_win`. (SQLite requires table-rebuild for CHECK changes; use `CREATE TABLE matches_new ... INSERT SELECT ... DROP ... ALTER RENAME` pattern. Recreate indexes.)
2. Adds `legacy_cosmos_id TEXT UNIQUE` to `matches` (nullable; only set for imported rows).
3. Adds `legacy_cosmos_id TEXT UNIQUE` to `tournaments` (nullable).
4. Adds `CREATE UNIQUE INDEX idx_players_space_name ON players(space_id, display_name)` so the importer can use `INSERT ... ON CONFLICT` semantics for player upserts.

Rationale: drop rather than widen the CHECK constraints. The legacy data doesn't fit the canonical table-tennis ranges (we see scores up to 20, 5-point quick games, odd set counts). Future Pingit-native matches go through the API which validates separately at the handler layer.

### CLI subcommand: `cmd/server import-cosmos`

Mirrors the existing `seed` subcommand pattern. Flags:

- `--dir <path>` — directory containing the JSON files. Required.
- `--user-email <email>` — operator's existing user. Required. Used as `created_by` and as the `user_id` for the `"Samuel Ström"` player.
- `--space-name <name>` — defaults to `"Elicit"`. Created if not present (with operator as `created_by` and admin member).
- `--dry-run` — parse and validate, log what would be inserted, but commit nothing.

Run order:

1. Resolve operator user (fail if not found).
2. Upsert space `"Elicit"`.
3. Parse `002fffff-...json` (Lucia tournament) and `de42384c-...json` + `6d7de665-...json` (standalone matches).
4. Build canonical player set (trimmed, unique). Upsert `players`.
5. Insert tournament + tournament_players + tournament-stage matches.
6. Insert standalone matches (deduped on `legacy_cosmos_id`).
7. Insert `score_events` for the one match that has them.

Everything wraps in a single transaction. On error, roll back; print error with the offending source `id`.

### Player handling

- Trim whitespace on every name read from source. `"André Pontes "` → `"André Pontes"`.
- Build a set of unique trimmed names across all source files (Lucia + standalone + doubles team rosters).
- For each name, upsert into `players` keyed on `(space_id, display_name)`:
  - If `display_name == "Samuel Ström"`, set `user_id` = operator's user_id.
  - Otherwise, `user_id = NULL` (managed player).
- The 21 doubles matches contain `teamOne[]` / `teamTwo[]` of two names each. Both names get player rows.
- Players with empty/null names: source has 21 doubles where `playerOne`/`playerTwo` are null but team arrays are populated — these are valid doubles, not garbage. No actual empty-name singles in source.

### Match handling

For each Cosmos match document (singles `gameType: 0` or doubles `gameType: 1`):

- **Dedup:** if a row with the same `legacy_cosmos_id` exists, skip silently.
- **Standalone-match dedup beyond `id`:** for the 4 rows in `de42384c-...json` that are byte-near-duplicates with different `id`s, keep only the first encountered (compare on `(date, playerOne, playerTwo, games[].HomeScore, games[].VisitorScore)`). Log skipped duplicates.
- **`kind`:** `'singles'` if `gameType == 0`, `'doubles'` if `gameType == 1`.
- **`status`:** `'completed'` always (legacy data is historical and final).
- **`started_at` / `completed_at`:**
  - Default: both = parsed `date` field (Unix ms).
  - Exception: the one match with populated `games[0].Score` array (per-point events): `started_at` = first event timestamp, `completed_at` = last event timestamp.
- **`best_of`:** `len(games)` rounded up to the nearest of `{1,3,5,7}` if 1–7, otherwise stored raw (constraint dropped).
- **`points_to_win`:** `11` if any game has `max(HomeScore, VisitorScore) >= 10`, else `5`.
- **`winner_side`:** computed by counting games won per side. `'home'` if home took more games, `'visitor'` otherwise. Ties (rare; only abandoned matches) → leave `NULL` and set `status = 'abandoned'` instead.
- **`tournament_id` / `tournament_phase` / `tournament_group_id` / `tournament_bracket_round`:** populated only for matches sourced from inside the Lucia tournament; `NULL` for standalone.
- **`created_by`:** operator's user_id.
- **`legacy_cosmos_id`:** the source document `id`.
- **`space_id`:** Elicit space.

### Match participants

- **Singles:** two rows in `match_participants`. `side='home', slot=1, player_id=<playerOne>`. `side='visitor', slot=1, player_id=<playerTwo>`. Slot 2 is unused.
- **Doubles:** four rows. Home: `teamOne[0]` slot 1, `teamOne[1]` slot 2. Visitor: `teamTwo[0]` slot 1, `teamTwo[1]` slot 2.

### Games

For each match, iterate `games[]`:

- One `games` row per element. `game_number = index + 1`.
- `home_score = HomeScore`, `visitor_score = VisitorScore`.
- `status = 'completed'`.
- `started_at = completed_at = match.started_at` (legacy data has no per-set timestamps except in the one events-rich match — there, distribute roughly across the event timestamps for that game).

### Score events

For **every** match where `games[].Score` is a populated array of `{Timestamp, Scorer}` objects (not an empty array, empty object, or null), import the events:

- One `score_events` row per entry. `sequence` = index + 1 (1-based).
- `scorer_side`: `'home'` if `Scorer == playerOne` (or in `teamOne` for doubles), `'visitor'` otherwise.
- `home_score_after` / `visitor_score_after`: derived by replaying events from 0–0.
- `occurred_at`: the event `Timestamp` (already Unix ms).
- `game_id` references the corresponding `games` row for that set.

For matches that have score events, set `started_at` and `completed_at` on both the match and on each affected game from the first/last event timestamps (per game where applicable).

Sample matches confirmed to have events: `ef4b700a-...` (Andreas vs Fredrik 2022-09-07), `9e7e1b5c-...`, possibly others. Don't hardcode IDs — detect via populated `Score` array.

### Tournament handling (Lucia 2022)

Source: `002fffff-...json`, single tournament document.

Insert one `tournaments` row:

- `name = "Lucia 2022"`
- `format = 'groups_knockout'` (has both `Groups` and `Rounds`)
- `best_of = 1` (Cosmos `SetAmount = 1` — single-set group matches; bracket round used 5-set best-of-5)
- `points_to_win = 11` (all scores fit)
- `status = 'completed'`
- `winner_player_id`: the player with `IsEliminated == false` (`Patrik Johansson`)
- `legacy_cosmos_id` = source document `id`

Insert one `tournament_players` row per source `Players[]` entry. `eliminated_at = tournament.created_at` for `IsEliminated == true`, `NULL` otherwise. `seed = NULL` (not present in source).

For each match in `Groups[].Matches[]`:
- Insert as a regular match with `tournament_id = <new>`, `tournament_phase = 'group'` (matches existing app convention), `tournament_group_id = <Cosmos GroupId as text>`.
- Best_of approximated as above (most are `best_of = 1` or `3`).
- No `legacy_cosmos_id` on these (Cosmos didn't assign individual IDs to group matches — the `id` field is null in source).

For each match in `Rounds[].TournamentMatches[]`:
- Insert with `tournament_phase = 'knockout'`, `tournament_bracket_round = Round`.
- The one Lucia bracket match has 5 sets, so `best_of = 5`.
- Same null-`id` situation; no `legacy_cosmos_id`.

The legacy app's pre-computed `Groups[].Players[]` standings (Wins/Losses/Score/etc.) are **not** stored — Pingit can recompute these from the imported matches via existing stats logic.

## Data Model

New migration `002_legacy_import.sql` performs:

```sql
-- +goose Up

-- Rebuild matches without best_of / points_to_win CHECK constraints, add legacy_cosmos_id
CREATE TABLE matches_new (
  id TEXT PRIMARY KEY,
  space_id TEXT NOT NULL REFERENCES spaces(id),
  kind TEXT NOT NULL CHECK(kind IN ('singles','doubles')),
  tournament_id TEXT REFERENCES tournaments(id),
  tournament_phase TEXT,
  tournament_group_id TEXT,
  tournament_bracket_round INTEGER,
  best_of INTEGER NOT NULL,
  points_to_win INTEGER NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('in_progress','completed','abandoned')),
  winner_side TEXT CHECK(winner_side IN ('home','visitor') OR winner_side IS NULL),
  started_at INTEGER NOT NULL,
  completed_at INTEGER,
  created_by TEXT NOT NULL REFERENCES users(id),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  deleted_at INTEGER,
  legacy_cosmos_id TEXT UNIQUE
);
INSERT INTO matches_new SELECT *, NULL FROM matches;
DROP TABLE matches;
ALTER TABLE matches_new RENAME TO matches;
CREATE INDEX idx_matches_space ON matches(space_id);
CREATE INDEX idx_matches_tournament ON matches(tournament_id);

-- Rebuild tournaments without best_of / points_to_win CHECK constraints, add legacy_cosmos_id
CREATE TABLE tournaments_new (
  id TEXT PRIMARY KEY,
  space_id TEXT NOT NULL REFERENCES spaces(id),
  name TEXT NOT NULL,
  format TEXT NOT NULL CHECK(format IN ('round_robin','bracket','groups_knockout')),
  best_of INTEGER NOT NULL,
  points_to_win INTEGER NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('setup','in_progress','completed')),
  winner_player_id TEXT REFERENCES players(id),
  created_by TEXT NOT NULL REFERENCES users(id),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  deleted_at INTEGER,
  legacy_cosmos_id TEXT UNIQUE
);
INSERT INTO tournaments_new SELECT *, NULL FROM tournaments;
DROP TABLE tournaments;
ALTER TABLE tournaments_new RENAME TO tournaments;

-- +goose Down
-- (down migration is best-effort: cannot recover original CHECK constraints losslessly
-- if data has been written that violates them. Drop columns and warn.)
ALTER TABLE matches DROP COLUMN legacy_cosmos_id;
ALTER TABLE tournaments DROP COLUMN legacy_cosmos_id;
```

(Down migration won't restore the dropped CHECKs; document this in the migration comment.)

## Error Handling

- **Missing operator user** → fatal, exit before opening transaction.
- **Missing source files** → fatal per file unless the file is in the skip-list.
- **Malformed JSON** → fatal with file path + error.
- **Unknown `gameType`** (anything other than 0 or 1) → fatal with offending document `id`. Source data only contains 0 and 1; anything else is a sign of unexpected input.
- **Player name resolves to empty after trim** → fatal with offending document `id`.
- **`legacy_cosmos_id` collision (re-import)** → silent skip per row, log count at end.
- **Match referencing a player not in the resolved player set** → fatal (should be impossible after upsert pass, but guard anyway).
- **Lucia tournament import partial failure** → roll back the entire transaction (single-tx import).

All inserts run inside one transaction. Either everything lands or nothing does.

## CLI Output

On success:

```
Resolved operator: <email> (<user_id>)
Space: Elicit (<space_id>) [created|existing]
Players upserted: 11 (1 linked, 10 managed)
Tournaments imported: 1 (Lucia 2022)
Tournament matches: 16 group + 1 bracket = 17
Standalone matches imported: 882 singles + 21 doubles = 903
Standalone matches skipped (dup): 2
Score events imported: 22 (1 match)
```

On dry-run, prefix every counted action with `[dry-run]` and skip commit.

## Security

This is an operator-only CLI command run from the host shell against a local DB file. No auth surface added. The only user-controlled input is the JSON directory path; we read JSON with the standard library and never `eval` or template anything from source data. SQL is parameterized via sqlc-generated functions.

## Resolved Decisions

- **Tournament group-match `started_at`:** source group matches all share the same `Date` (`2023-03-03T20:57:15.801Z`) because the legacy app stamped them at tournament-creation time. **Decision: keep as-is.** Bracket match keeps its own timestamp.
- **Score-event game distribution:** the events-rich match has one `games[]` entry but 22 events. `games[0].started_at` = first event timestamp; `games[0].completed_at` = last event timestamp. **Confirmed.**
- **Future doubles slots:** legacy doubles always has exactly 2 players per side. Schema's `slot IN (1,2)` is fine. No action needed.
- **Lucia winner detection:** trust the `IsEliminated` flag (only `Patrik Johansson` has `false`). Bracket result confirms it independently.
