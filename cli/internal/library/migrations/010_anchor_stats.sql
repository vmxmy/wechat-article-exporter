CREATE TABLE IF NOT EXISTS anchor_stats (
  surface     TEXT NOT NULL,
  anchor      TEXT NOT NULL,
  hit_count   INTEGER NOT NULL DEFAULT 0,
  last_hit_at INTEGER NOT NULL,
  PRIMARY KEY (surface, anchor)
) STRICT;
