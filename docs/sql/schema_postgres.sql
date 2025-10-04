/* PostgreSQL schema for Smart Blocks Radio Suite */
/* No double hyphen comments to avoid dashes in prose. */

/* Basic enums */
CREATE TYPE user_role AS ENUM ('admin', 'manager', 'dj');

/* Users and access */
CREATE TABLE users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email TEXT NOT NULL UNIQUE,
  display_name TEXT NOT NULL,
  role user_role NOT NULL DEFAULT 'dj',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE stations (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL UNIQUE,
  timezone TEXT NOT NULL DEFAULT 'UTC',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE mounts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  station_id UUID NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  url_path TEXT NOT NULL,
  format TEXT NOT NULL, /* mp3, opus, vorbis, aac */
  bitrate_kbps INT NOT NULL,
  encoder_preset JSONB NOT NULL DEFAULT '{}',
  UNIQUE(station_id, name)
);

/* Media library */
CREATE TABLE media (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  storage_key TEXT NOT NULL UNIQUE,
  duration_ms INT NOT NULL,
  loudness_integrated NUMERIC(6,2),
  loudness_range NUMERIC(6,2),
  peak_dbfs NUMERIC(6,2),
  cue_in_ms INT DEFAULT 0,
  cue_out_ms INT,
  intro_end_ms INT,
  outro_start_ms INT,
  artist TEXT,
  title TEXT,
  album TEXT,
  label TEXT,
  genre TEXT,
  year INT,
  bpm INT,
  language TEXT,
  mood TEXT,
  explicit BOOLEAN DEFAULT FALSE,
  tags JSONB NOT NULL DEFAULT '[]',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX media_tags_gin ON media USING GIN (tags);
CREATE INDEX media_artist_title_idx ON media(artist, title);

/* Rulesets and clocks */
CREATE TABLE rulesets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  station_id UUID NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  rules JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(station_id, name)
);

CREATE TABLE clocks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  station_id UUID NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  clock_json JSONB NOT NULL,
  UNIQUE(station_id, name)
);

/* Schedule */
CREATE TABLE schedules (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  station_id UUID NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
  clock_id UUID NOT NULL REFERENCES clocks(id) ON DELETE RESTRICT,
  mount_id UUID NOT NULL REFERENCES mounts(id) ON DELETE RESTRICT,
  start_time TIMESTAMPTZ NOT NULL,
  duration_ms INT NOT NULL,
  priority INT NOT NULL DEFAULT 0
);

CREATE INDEX schedules_time_idx ON schedules(station_id, start_time);

/* Playout plan and history */
CREATE TABLE plays (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  station_id UUID NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
  mount_id UUID NOT NULL REFERENCES mounts(id) ON DELETE RESTRICT,
  media_id UUID REFERENCES media(id) ON DELETE SET NULL,
  started_at TIMESTAMPTZ NOT NULL,
  ended_at TIMESTAMPTZ,
  artist TEXT,
  title TEXT,
  seed BIGINT,
  scheduler_run_id UUID,
  category TEXT
);

CREATE INDEX plays_recent_idx ON plays(station_id, started_at DESC);
CREATE INDEX plays_artist_idx ON plays(station_id, artist, started_at DESC);

/* Quotas and separation memory */
CREATE TABLE quotas_memory (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  station_id UUID NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
  key_type TEXT NOT NULL, /* artist, title, album, label */
  key_value TEXT NOT NULL,
  last_played_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX quotas_key_idx ON quotas_memory(station_id, key_type, key_value);

/* Analysis jobs */
CREATE TABLE analysis_jobs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  media_id UUID NOT NULL REFERENCES media(id) ON DELETE CASCADE,
  status TEXT NOT NULL, /* pending, running, done, failed */
  error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

/* Webhook events */
CREATE TABLE events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  station_id UUID NOT NULL REFERENCES stations(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

/* Useful views */
CREATE VIEW recent_artist_plays AS
SELECT station_id, artist, max(started_at) AS last_time
FROM plays
WHERE artist IS NOT NULL
GROUP BY station_id, artist;
