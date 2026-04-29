-- +goose Up
-- +goose StatementBegin
-- Rebuild matches without best_of/points_to_win CHECK constraints,
-- and add legacy_cosmos_id (nullable, UNIQUE) for idempotent legacy imports.
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
INSERT INTO matches_new (
  id, space_id, kind, tournament_id, tournament_phase, tournament_group_id,
  tournament_bracket_round, best_of, points_to_win, status, winner_side,
  started_at, completed_at, created_by, created_at, updated_at, deleted_at,
  legacy_cosmos_id
)
SELECT
  id, space_id, kind, tournament_id, tournament_phase, tournament_group_id,
  tournament_bracket_round, best_of, points_to_win, status, winner_side,
  started_at, completed_at, created_by, created_at, updated_at, deleted_at,
  NULL
FROM matches;
DROP TABLE matches;
ALTER TABLE matches_new RENAME TO matches;
CREATE INDEX idx_matches_space ON matches(space_id);
CREATE INDEX idx_matches_tournament ON matches(tournament_id);
-- +goose StatementEnd

-- +goose StatementBegin
-- Rebuild tournaments without best_of/points_to_win CHECK constraints,
-- and add legacy_cosmos_id (nullable, UNIQUE).
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
INSERT INTO tournaments_new (
  id, space_id, name, format, best_of, points_to_win, status,
  winner_player_id, created_by, created_at, updated_at, deleted_at,
  legacy_cosmos_id
)
SELECT
  id, space_id, name, format, best_of, points_to_win, status,
  winner_player_id, created_by, created_at, updated_at, deleted_at,
  NULL
FROM tournaments;
DROP TABLE tournaments;
ALTER TABLE tournaments_new RENAME TO tournaments;
-- +goose StatementEnd

-- +goose StatementBegin
-- Enable INSERT ... ON CONFLICT(space_id, display_name) for player upserts.
CREATE UNIQUE INDEX idx_players_space_name ON players(space_id, display_name);
-- +goose StatementEnd

-- +goose Down
-- Note: the original CHECK(best_of IN (1,3,5,7)) and CHECK(points_to_win IN (5,11))
-- constraints cannot be losslessly restored if imported rows violate them. The
-- down path drops the new column and the player uniqueness index but leaves the
-- relaxed schema in place.
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_players_space_name;
ALTER TABLE matches DROP COLUMN legacy_cosmos_id;
ALTER TABLE tournaments DROP COLUMN legacy_cosmos_id;
-- +goose StatementEnd
