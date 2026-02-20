CREATE TABLE IF NOT EXISTS profiles (
  id TEXT PRIMARY KEY,
  display_name TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS film_logs (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL,
  title TEXT NOT NULL,
  logged_at TEXT NOT NULL,
  local_date TEXT NOT NULL,
  rating REAL,
  rewatch INTEGER NOT NULL DEFAULT 0,
  notes TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  FOREIGN KEY(profile_id) REFERENCES profiles(id)
);

CREATE INDEX IF NOT EXISTS idx_film_logs_profile_local_date ON film_logs(profile_id, local_date);
CREATE INDEX IF NOT EXISTS idx_film_logs_profile_logged_at ON film_logs(profile_id, logged_at);
CREATE INDEX IF NOT EXISTS idx_film_logs_profile_title ON film_logs(profile_id, title);
