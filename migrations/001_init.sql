-- +goose Up
CREATE TABLE users (
  id TEXT PRIMARY KEY,
  email TEXT UNIQUE NOT NULL,
  display_name TEXT NOT NULL,
  avatar_url TEXT,
  is_super_admin INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);

CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id),
  expires_at INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  user_agent TEXT,
  ip TEXT
);
CREATE INDEX idx_sessions_user ON sessions(user_id);

CREATE TABLE magic_link_tokens (
  token TEXT PRIMARY KEY,
  email TEXT NOT NULL,
  expires_at INTEGER NOT NULL,
  used_at INTEGER,
  created_at INTEGER NOT NULL
);

CREATE TABLE spaces (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  description TEXT,
  is_invite_only INTEGER NOT NULL DEFAULT 1,
  join_code TEXT UNIQUE,
  created_by TEXT NOT NULL REFERENCES users(id),
  created_at INTEGER NOT NULL,
  deleted_at INTEGER
);

CREATE TABLE space_members (
  space_id TEXT NOT NULL REFERENCES spaces(id),
  user_id TEXT NOT NULL REFERENCES users(id),
  role TEXT NOT NULL CHECK(role IN ('admin','member')),
  joined_at INTEGER NOT NULL,
  PRIMARY KEY (space_id, user_id)
);

CREATE TABLE space_invitations (
  id TEXT PRIMARY KEY,
  space_id TEXT NOT NULL REFERENCES spaces(id),
  email TEXT NOT NULL,
  token TEXT UNIQUE NOT NULL,
  invited_by TEXT NOT NULL REFERENCES users(id),
  status TEXT NOT NULL CHECK(status IN ('pending','accepted','declined','expired')),
  created_at INTEGER NOT NULL,
  accepted_at INTEGER
);

CREATE TABLE players (
  id TEXT PRIMARY KEY,
  space_id TEXT NOT NULL REFERENCES spaces(id),
  display_name TEXT NOT NULL,
  user_id TEXT REFERENCES users(id),
  avatar_url TEXT,
  created_at INTEGER NOT NULL,
  deleted_at INTEGER
);
CREATE INDEX idx_players_space ON players(space_id);
CREATE INDEX idx_players_user ON players(user_id);

CREATE TABLE tournaments (
  id TEXT PRIMARY KEY,
  space_id TEXT NOT NULL REFERENCES spaces(id),
  name TEXT NOT NULL,
  format TEXT NOT NULL CHECK(format IN ('round_robin','bracket')),
  best_of INTEGER NOT NULL CHECK(best_of IN (1,3,5,7)),
  points_to_win INTEGER NOT NULL CHECK(points_to_win IN (5,11)),
  status TEXT NOT NULL CHECK(status IN ('setup','in_progress','completed')),
  winner_player_id TEXT REFERENCES players(id),
  created_by TEXT NOT NULL REFERENCES users(id),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  deleted_at INTEGER
);

CREATE TABLE matches (
  id TEXT PRIMARY KEY,
  space_id TEXT NOT NULL REFERENCES spaces(id),
  kind TEXT NOT NULL CHECK(kind IN ('singles','doubles')),
  tournament_id TEXT REFERENCES tournaments(id),
  tournament_phase TEXT,
  tournament_group_id TEXT,
  tournament_bracket_round INTEGER,
  best_of INTEGER NOT NULL CHECK(best_of IN (1,3,5,7)),
  points_to_win INTEGER NOT NULL CHECK(points_to_win IN (5,11)),
  status TEXT NOT NULL CHECK(status IN ('in_progress','completed','abandoned')),
  winner_side TEXT CHECK(winner_side IN ('home','visitor') OR winner_side IS NULL),
  started_at INTEGER NOT NULL,
  completed_at INTEGER,
  created_by TEXT NOT NULL REFERENCES users(id),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  deleted_at INTEGER
);
CREATE INDEX idx_matches_space ON matches(space_id);
CREATE INDEX idx_matches_tournament ON matches(tournament_id);

CREATE TABLE match_participants (
  match_id TEXT NOT NULL REFERENCES matches(id),
  player_id TEXT NOT NULL REFERENCES players(id),
  side TEXT NOT NULL CHECK(side IN ('home','visitor')),
  slot INTEGER NOT NULL CHECK(slot IN (1,2)),
  PRIMARY KEY (match_id, side, slot)
);
CREATE INDEX idx_participants_player ON match_participants(player_id);

CREATE TABLE games (
  id TEXT PRIMARY KEY,
  match_id TEXT NOT NULL REFERENCES matches(id),
  game_number INTEGER NOT NULL,
  home_score INTEGER NOT NULL DEFAULT 0,
  visitor_score INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL CHECK(status IN ('in_progress','completed')),
  started_at INTEGER NOT NULL,
  completed_at INTEGER,
  UNIQUE(match_id, game_number)
);

CREATE TABLE score_events (
  id TEXT PRIMARY KEY,
  game_id TEXT NOT NULL REFERENCES games(id),
  sequence INTEGER NOT NULL,
  scorer_side TEXT NOT NULL CHECK(scorer_side IN ('home','visitor')),
  home_score_after INTEGER NOT NULL,
  visitor_score_after INTEGER NOT NULL,
  occurred_at INTEGER NOT NULL,
  UNIQUE(game_id, sequence)
);

CREATE TABLE tournament_players (
  tournament_id TEXT NOT NULL REFERENCES tournaments(id),
  player_id TEXT NOT NULL REFERENCES players(id),
  seed INTEGER,
  eliminated_at INTEGER,
  PRIMARY KEY (tournament_id, player_id)
);

CREATE TABLE match_edits (
  id TEXT PRIMARY KEY,
  match_id TEXT NOT NULL REFERENCES matches(id),
  edited_by TEXT NOT NULL REFERENCES users(id),
  edited_at INTEGER NOT NULL,
  changes_json TEXT NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS match_edits;
DROP TABLE IF EXISTS tournament_players;
DROP TABLE IF EXISTS score_events;
DROP TABLE IF EXISTS games;
DROP TABLE IF EXISTS match_participants;
DROP TABLE IF EXISTS matches;
DROP TABLE IF EXISTS players;
DROP TABLE IF EXISTS tournaments;
DROP TABLE IF EXISTS space_invitations;
DROP TABLE IF EXISTS space_members;
DROP TABLE IF EXISTS spaces;
DROP TABLE IF EXISTS magic_link_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
