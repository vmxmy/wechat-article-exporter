ALTER TABLE articles ADD COLUMN is_original INTEGER NOT NULL DEFAULT 0 CHECK (is_original IN (0, 1));
ALTER TABLE articles ADD COLUMN wecoin_count INTEGER NOT NULL DEFAULT 0 CHECK (wecoin_count >= 0);
ALTER TABLE articles ADD COLUMN media_duration_seconds INTEGER NOT NULL DEFAULT 0 CHECK (media_duration_seconds >= 0);

CREATE TABLE IF NOT EXISTS saved_article_queries (
  profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  query_json TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  PRIMARY KEY(profile_id, name)
);
