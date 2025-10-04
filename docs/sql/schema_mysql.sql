/* MySQL schema for Smart Blocks Radio Suite */
/* Use InnoDB and utf8mb4. Avoid double hyphen comments. */
SET NAMES utf8mb4;
SET sql_mode = 'STRICT_ALL_TABLES';

CREATE TABLE users (
  id CHAR(36) PRIMARY KEY,
  email VARCHAR(320) NOT NULL UNIQUE,
  display_name VARCHAR(255) NOT NULL,
  role ENUM('admin','manager','dj') NOT NULL DEFAULT 'dj',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE stations (
  id CHAR(36) PRIMARY KEY,
  name VARCHAR(255) NOT NULL UNIQUE,
  timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE mounts (
  id CHAR(36) PRIMARY KEY,
  station_id CHAR(36) NOT NULL,
  name VARCHAR(255) NOT NULL,
  url_path VARCHAR(255) NOT NULL,
  format VARCHAR(16) NOT NULL,
  bitrate_kbps INT NOT NULL,
  encoder_preset JSON NOT NULL,
  UNIQUE KEY uniq_mount (station_id, name),
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE media (
  id CHAR(36) PRIMARY KEY,
  storage_key VARCHAR(1024) NOT NULL UNIQUE,
  duration_ms INT NOT NULL,
  loudness_integrated DECIMAL(6,2),
  loudness_range DECIMAL(6,2),
  peak_dbfs DECIMAL(6,2),
  cue_in_ms INT DEFAULT 0,
  cue_out_ms INT,
  intro_end_ms INT,
  outro_start_ms INT,
  artist VARCHAR(255),
  title VARCHAR(255),
  album VARCHAR(255),
  label VARCHAR(255),
  genre VARCHAR(255),
  year INT,
  bpm INT,
  language VARCHAR(64),
  mood VARCHAR(64),
  explicit BOOLEAN DEFAULT FALSE,
  tags JSON NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_artist_title (artist, title)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE rulesets (
  id CHAR(36) PRIMARY KEY,
  station_id CHAR(36) NOT NULL,
  name VARCHAR(255) NOT NULL,
  rules JSON NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_ruleset (station_id, name),
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE clocks (
  id CHAR(36) PRIMARY KEY,
  station_id CHAR(36) NOT NULL,
  name VARCHAR(255) NOT NULL,
  clock_json JSON NOT NULL,
  UNIQUE KEY uniq_clock (station_id, name),
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE schedules (
  id CHAR(36) PRIMARY KEY,
  station_id CHAR(36) NOT NULL,
  clock_id CHAR(36) NOT NULL,
  mount_id CHAR(36) NOT NULL,
  start_time DATETIME(3) NOT NULL,
  duration_ms INT NOT NULL,
  priority INT NOT NULL DEFAULT 0,
  KEY idx_sched_time (station_id, start_time),
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE,
  FOREIGN KEY (clock_id) REFERENCES clocks(id),
  FOREIGN KEY (mount_id) REFERENCES mounts(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE plays (
  id CHAR(36) PRIMARY KEY,
  station_id CHAR(36) NOT NULL,
  mount_id CHAR(36) NOT NULL,
  media_id CHAR(36),
  started_at DATETIME(3) NOT NULL,
  ended_at DATETIME(3),
  artist VARCHAR(255),
  title VARCHAR(255),
  seed BIGINT,
  scheduler_run_id CHAR(36),
  category VARCHAR(64),
  KEY idx_recent (station_id, started_at),
  KEY idx_artist (station_id, artist, started_at),
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE,
  FOREIGN KEY (mount_id) REFERENCES mounts(id),
  FOREIGN KEY (media_id) REFERENCES media(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE quotas_memory (
  id CHAR(36) PRIMARY KEY,
  station_id CHAR(36) NOT NULL,
  key_type VARCHAR(32) NOT NULL,
  key_value VARCHAR(255) NOT NULL,
  last_played_at DATETIME(3) NOT NULL,
  KEY idx_quota (station_id, key_type, key_value),
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE analysis_jobs (
  id CHAR(36) PRIMARY KEY,
  media_id CHAR(36) NOT NULL,
  status VARCHAR(16) NOT NULL,
  error TEXT,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  FOREIGN KEY (media_id) REFERENCES media(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE events (
  id CHAR(36) PRIMARY KEY,
  station_id CHAR(36) NOT NULL,
  event_type VARCHAR(64) NOT NULL,
  payload JSON NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (station_id) REFERENCES stations(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

