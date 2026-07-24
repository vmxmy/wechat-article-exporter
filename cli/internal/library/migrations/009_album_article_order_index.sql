CREATE INDEX IF NOT EXISTS article_albums_album_ordinal_idx
  ON article_albums(album_id, ordinal, article_id);
