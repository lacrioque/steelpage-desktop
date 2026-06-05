CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  display_name TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS comments (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  path TEXT NOT NULL,
  line_start INTEGER NOT NULL,
  line_end INTEGER NOT NULL,
  anchor_text TEXT,
  document_sha TEXT,
  author_id INTEGER NOT NULL REFERENCES users(id),
  body TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'open',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  resolved_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_comments_path ON comments(path, status);

-- Single-user desktop build: seed the one local identity so the
-- comments.author_id FK always has a target. The display name is synced
-- to the OS user at startup.
INSERT INTO users (id, display_name, created_at)
VALUES (1, 'You', strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
ON CONFLICT(id) DO NOTHING;
