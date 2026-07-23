ALTER TABLE exports ADD COLUMN provenance_generation INTEGER NOT NULL DEFAULT 1;
ALTER TABLE exports ADD COLUMN provenance_claimed_at INTEGER;

-- A pre-generation writer has no reliable lease timestamp. Mark it stale so
-- the first post-upgrade process can reclaim it immediately.
UPDATE exports SET provenance_claimed_at = 0 WHERE provenance_state = 'writing';
