CREATE TABLE IF NOT EXISTS reply_checkpoints (
  article_id TEXT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  content_id TEXT NOT NULL,
  max_reply_id INTEGER NOT NULL DEFAULT 0,
  total_replies INTEGER NOT NULL DEFAULT 0,
  complete INTEGER NOT NULL DEFAULT 0 CHECK (complete IN (0, 1)),
  attempt_count INTEGER NOT NULL DEFAULT 0,
  last_error TEXT NOT NULL DEFAULT '',
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(article_id, content_id)
);

CREATE INDEX IF NOT EXISTS reply_checkpoints_pending_idx
  ON reply_checkpoints(article_id, complete, updated_at, content_id);
