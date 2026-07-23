CREATE TABLE IF NOT EXISTS scheduler_permits (
  id TEXT PRIMARY KEY,
  profile_id TEXT NOT NULL REFERENCES profiles(id) ON DELETE CASCADE,
  owner TEXT NOT NULL,
  operation TEXT NOT NULL DEFAULT '',
  host TEXT NOT NULL DEFAULT '',
  sensitive INTEGER NOT NULL DEFAULT 0 CHECK(sensitive IN (0, 1)),
  acquired_at INTEGER NOT NULL,
  renewed_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS scheduler_permits_profile_expiry_idx
  ON scheduler_permits(profile_id, expires_at);
CREATE INDEX IF NOT EXISTS scheduler_permits_profile_operation_expiry_idx
  ON scheduler_permits(profile_id, operation, expires_at);
CREATE INDEX IF NOT EXISTS scheduler_permits_profile_host_expiry_idx
  ON scheduler_permits(profile_id, host, expires_at);
CREATE INDEX IF NOT EXISTS scheduler_permits_profile_sensitive_expiry_idx
  ON scheduler_permits(profile_id, sensitive, expires_at);
