CREATE TABLE IF NOT EXISTS app_metadata (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS profiles (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS accounts (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  fakeid TEXT NOT NULL,
  nickname TEXT NOT NULL DEFAULT '',
  alias TEXT NOT NULL DEFAULT '',
  signature TEXT NOT NULL DEFAULT '',
  avatar_url TEXT NOT NULL DEFAULT '',
  service_type INTEGER NOT NULL DEFAULT 0,
  completed INTEGER NOT NULL DEFAULT 0 CHECK (completed IN (0, 1)),
  message_count INTEGER NOT NULL DEFAULT 0,
  article_count INTEGER NOT NULL DEFAULT 0,
  upstream_total INTEGER NOT NULL DEFAULT 0,
  sync_cursor TEXT NOT NULL DEFAULT '',
  last_sync_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(profile_id, fakeid)
);

CREATE TABLE IF NOT EXISTS articles (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  account_id TEXT REFERENCES accounts(id) ON DELETE SET NULL,
  aid TEXT NOT NULL DEFAULT '',
  appmsg_id INTEGER,
  item_index INTEGER,
  title TEXT NOT NULL DEFAULT '',
  author TEXT NOT NULL DEFAULT '',
  digest TEXT NOT NULL DEFAULT '',
  canonical_url TEXT NOT NULL,
  cover_url TEXT NOT NULL DEFAULT '',
  published_at INTEGER,
  updated_at_upstream INTEGER,
  message_type INTEGER NOT NULL DEFAULT 0,
  state TEXT NOT NULL DEFAULT '',
  is_deleted INTEGER NOT NULL DEFAULT 0 CHECK (is_deleted IN (0, 1)),
  is_paid INTEGER NOT NULL DEFAULT 0 CHECK (is_paid IN (0, 1)),
  is_single INTEGER NOT NULL DEFAULT 0 CHECK (is_single IN (0, 1)),
  content_status TEXT NOT NULL DEFAULT 'missing',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(profile_id, canonical_url)
);

CREATE INDEX IF NOT EXISTS articles_profile_published_idx ON articles(profile_id, published_at DESC, id);
CREATE INDEX IF NOT EXISTS articles_account_published_idx ON articles(account_id, published_at DESC, id);
CREATE INDEX IF NOT EXISTS articles_profile_state_idx ON articles(profile_id, state, published_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS articles_stable_identity_idx
  ON articles(profile_id, account_id, aid)
  WHERE account_id IS NOT NULL AND aid <> '';

CREATE TABLE IF NOT EXISTS albums (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  account_id TEXT REFERENCES accounts(id) ON DELETE SET NULL,
  upstream_id TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  description TEXT NOT NULL DEFAULT '',
  article_count INTEGER NOT NULL DEFAULT 0,
  is_paid INTEGER NOT NULL DEFAULT 0 CHECK (is_paid IN (0, 1)),
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(profile_id, upstream_id)
);

CREATE TABLE IF NOT EXISTS article_albums (
  article_id TEXT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  album_id TEXT NOT NULL REFERENCES albums(id) ON DELETE CASCADE,
  ordinal INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(article_id, album_id)
);

CREATE TABLE IF NOT EXISTS content_versions (
  id TEXT PRIMARY KEY,
  article_id TEXT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  object_digest TEXT NOT NULL,
  kind TEXT NOT NULL,
  media_type TEXT NOT NULL DEFAULT '',
  source_url TEXT NOT NULL DEFAULT '',
  classification TEXT NOT NULL DEFAULT '',
  comment_id TEXT NOT NULL DEFAULT '',
  captured_at INTEGER NOT NULL,
  is_current INTEGER NOT NULL DEFAULT 1 CHECK (is_current IN (0, 1)),
  UNIQUE(article_id, kind, object_digest)
);

CREATE TABLE IF NOT EXISTS metric_snapshots (
  id TEXT PRIMARY KEY,
  article_id TEXT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  read_count INTEGER NOT NULL DEFAULT 0,
  old_like_count INTEGER NOT NULL DEFAULT 0,
  like_count INTEGER NOT NULL DEFAULT 0,
  share_count INTEGER NOT NULL DEFAULT 0,
  comment_count INTEGER NOT NULL DEFAULT 0,
  credential_ref TEXT NOT NULL DEFAULT '',
  captured_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS comments (
  id TEXT PRIMARY KEY,
  article_id TEXT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  upstream_id TEXT NOT NULL,
  parent_id TEXT REFERENCES comments(id) ON DELETE CASCADE,
  author_name TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  like_count INTEGER NOT NULL DEFAULT 0,
  created_at_upstream INTEGER,
  raw_object_digest TEXT NOT NULL DEFAULT '',
  fetched_at INTEGER NOT NULL,
  UNIQUE(article_id, upstream_id)
);

CREATE TABLE IF NOT EXISTS replies (
  id TEXT PRIMARY KEY,
  comment_id TEXT NOT NULL REFERENCES comments(id) ON DELETE CASCADE,
  upstream_id TEXT NOT NULL,
  author_name TEXT NOT NULL DEFAULT '',
  content TEXT NOT NULL DEFAULT '',
  like_count INTEGER NOT NULL DEFAULT 0,
  created_at_upstream INTEGER,
  raw_object_digest TEXT NOT NULL DEFAULT '',
  fetched_at INTEGER NOT NULL,
  UNIQUE(comment_id, upstream_id)
);

CREATE TABLE IF NOT EXISTS comment_checkpoints (
  article_id TEXT PRIMARY KEY REFERENCES articles(id) ON DELETE CASCADE,
  continuation TEXT NOT NULL DEFAULT '',
  complete INTEGER NOT NULL DEFAULT 0 CHECK (complete IN (0, 1)),
  updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS objects (
  digest TEXT PRIMARY KEY,
  size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
  media_type TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  verified_at INTEGER
);

CREATE TABLE IF NOT EXISTS resources (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  source_url TEXT NOT NULL,
  object_digest TEXT REFERENCES objects(digest) ON DELETE RESTRICT,
  media_type TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'missing',
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(profile_id, source_url)
);

CREATE TABLE IF NOT EXISTS article_resources (
  article_id TEXT NOT NULL REFERENCES articles(id) ON DELETE CASCADE,
  resource_id TEXT NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
  role TEXT NOT NULL DEFAULT '',
  ordinal INTEGER NOT NULL DEFAULT 0,
  original_url TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(article_id, resource_id, role, ordinal)
);

CREATE TABLE IF NOT EXISTS credential_refs (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  account_id TEXT REFERENCES accounts(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  secret_ref TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'unknown',
  validated_at INTEGER,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(profile_id, kind, secret_ref)
);

CREATE TABLE IF NOT EXISTS network_routes (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  endpoint TEXT NOT NULL DEFAULT '',
  authorization_ref TEXT NOT NULL DEFAULT '',
  trust_level TEXT NOT NULL DEFAULT 'public-only',
  request_classes TEXT NOT NULL DEFAULT '[]',
  priority INTEGER NOT NULL DEFAULT 100,
  enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
  cooldown_until INTEGER,
  consecutive_failures INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL,
  UNIQUE(profile_id, name)
);

CREATE TABLE IF NOT EXISTS route_health_samples (
  id TEXT PRIMARY KEY,
  route_id TEXT NOT NULL REFERENCES network_routes(id) ON DELETE CASCADE,
  success INTEGER NOT NULL CHECK (success IN (0, 1)),
  latency_ms INTEGER,
  status_code INTEGER,
  error_class TEXT NOT NULL DEFAULT '',
  sampled_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS jobs (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  state TEXT NOT NULL,
  idempotency_key TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL DEFAULT '{}',
  checkpoint_json TEXT NOT NULL DEFAULT '{}',
  lease_owner TEXT NOT NULL DEFAULT '',
  lease_expires_at INTEGER,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  started_at INTEGER,
  updated_at INTEGER NOT NULL,
  completed_at INTEGER,
  UNIQUE(profile_id, kind, idempotency_key)
);

CREATE INDEX IF NOT EXISTS jobs_profile_state_idx ON jobs(profile_id, state, created_at, id);

CREATE TABLE IF NOT EXISTS job_items (
  id TEXT PRIMARY KEY,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  item_key TEXT NOT NULL,
  state TEXT NOT NULL,
  attempt_count INTEGER NOT NULL DEFAULT 0,
  checkpoint_json TEXT NOT NULL DEFAULT '{}',
  error_class TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  started_at INTEGER,
  updated_at INTEGER NOT NULL,
  completed_at INTEGER,
  UNIQUE(job_id, item_key)
);

CREATE TABLE IF NOT EXISTS job_attempts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  item_id TEXT REFERENCES job_items(id) ON DELETE CASCADE,
  attempt_number INTEGER NOT NULL,
  route_id TEXT REFERENCES network_routes(id) ON DELETE SET NULL,
  request_id TEXT NOT NULL DEFAULT '',
  failure_class TEXT NOT NULL DEFAULT '',
  error_message TEXT NOT NULL DEFAULT '',
  started_at INTEGER NOT NULL,
  completed_at INTEGER,
  UNIQUE(job_id, item_id, attempt_number)
);

CREATE TABLE IF NOT EXISTS job_logs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
  item_id TEXT REFERENCES job_items(id) ON DELETE CASCADE,
  level TEXT NOT NULL,
  message TEXT NOT NULL,
  fields_json TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS exports (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  job_id TEXT REFERENCES jobs(id) ON DELETE SET NULL,
  format TEXT NOT NULL,
  manifest_json TEXT NOT NULL DEFAULT '{}',
  output_root TEXT NOT NULL DEFAULT '',
  state TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  completed_at INTEGER
);

CREATE TABLE IF NOT EXISTS export_files (
  id TEXT PRIMARY KEY,
  export_id TEXT NOT NULL REFERENCES exports(id) ON DELETE CASCADE,
  relative_path TEXT NOT NULL,
  size_bytes INTEGER NOT NULL DEFAULT 0,
  sha256 TEXT NOT NULL DEFAULT '',
  media_type TEXT NOT NULL DEFAULT '',
  UNIQUE(export_id, relative_path)
);

CREATE TABLE IF NOT EXISTS debug_incidents (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  operation TEXT NOT NULL,
  classification TEXT NOT NULL DEFAULT '',
  request_id TEXT NOT NULL DEFAULT '',
  object_digest TEXT REFERENCES objects(digest) ON DELETE SET NULL,
  summary TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  expires_at INTEGER
);
