/* SQLite schema for Smart Blocks Radio Suite */
/* Use TEXT for UUIDs. Use JSON stored as TEXT. Use WITHOUT ROWID only where safe. */

PRAGMA foreign_keys = ON;

CREATE TABLE users (
  id TEXT PRIMARY KEY,
  email TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('admin','manager','dj')) DEFAULT 'dj',
  created_at TEXT NOT NULL
);

CREATE TABLE stations (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  timezone TEXT NOT NULL DEFAULT 'UTC',
  created_at TEXT NOT NULL
);

CREATE TABLE mounts (
  id TEXT PRIMARY KEY,
  station_id TEXT NOT NULL,
  name TEXT NOT NULL,
  url_path TEXT NOT NULL,
  format TEXT NOT NULL,
  bitrate_kbps INTEGER NOT NULL,
  encoder_preset TEXT NOT NULL DEFAULT '{}',
  UNIQUE(station_id, name),
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE
);

CREATE TABLE media (
  id TEXT PRIMARY KEY,
  storage_key TEXT NOT NULL UNIQUE,
  duration_ms INTEGER NOT NULL,
  loudness_integrated REAL,
  loudness_range REAL,
  peak_dbfs REAL,
  cue_in_ms INTEGER DEFAULT 0,
  cue_out_ms INTEGER,
  intro_end_ms INTEGER,
  outro_start_ms INTEGER,
  artist TEXT,
  title TEXT,
  album TEXT,
  label TEXT,
  genre TEXT,
  year INTEGER,
  bpm INTEGER,
  language TEXT,
  mood TEXT,
  explicit INTEGER DEFAULT 0,
  tags TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL
);

CREATE INDEX media_artist_title_idx ON media(artist, title);

CREATE TABLE rulesets (
  id TEXT PRIMARY KEY,
  station_id TEXT NOT NULL,
  name TEXT NOT NULL,
  rules TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(station_id, name),
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE
);

CREATE TABLE clocks (
  id TEXT PRIMARY KEY,
  station_id TEXT NOT NULL,
  name TEXT NOT NULL,
  clock_json TEXT NOT NULL,
  UNIQUE(station_id, name),
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE
);

CREATE TABLE schedules (
  id TEXT PRIMARY KEY,
  station_id TEXT NOT NULL,
  clock_id TEXT NOT NULL,
  mount_id TEXT NOT NULL,
  start_time TEXT NOT NULL,
  duration_ms INTEGER NOT NULL,
  priority INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE,
  FOREIGN KEY (clock_id) REFERENCES clocks(id),
  FOREIGN KEY (mount_id) REFERENCES mounts(id)
);

CREATE INDEX schedules_time_idx ON schedules(station_id, start_time);

CREATE TABLE plays (
  id TEXT PRIMARY KEY,
  station_id TEXT NOT NULL,
  mount_id TEXT NOT NULL,
  media_id TEXT,
  started_at TEXT NOT NULL,
  ended_at TEXT,
  artist TEXT,
  title TEXT,
  seed INTEGER,
  scheduler_run_id TEXT,
  category TEXT,
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE,
  FOREIGN KEY (mount_id) REFERENCES mounts(id),
  FOREIGN KEY (media_id) REFERENCES media(id)
);

CREATE INDEX plays_recent_idx ON plays(station_id, started_at);
CREATE INDEX plays_artist_idx ON plays(station_id, artist, started_at);

CREATE TABLE quotas_memory (
  id TEXT PRIMARY KEY,
  station_id TEXT NOT NULL,
  key_type TEXT NOT NULL,
  key_value TEXT NOT NULL,
  last_played_at TEXT NOT NULL,
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE
);

CREATE INDEX quotas_key_idx ON quotas_memory(station_id, key_type, key_value);

CREATE TABLE analysis_jobs (
  id TEXT PRIMARY KEY,
  media_id TEXT NOT NULL,
  status TEXT NOT NULL,
  error TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY (media_id) REFERENCES media(id) ON DELETE CASCADE
);

CREATE TABLE events (
  id TEXT PRIMARY KEY,
  station_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_at TEXT NOT NULL,
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE
);
