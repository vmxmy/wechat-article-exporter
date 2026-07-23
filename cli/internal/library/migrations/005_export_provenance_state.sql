ALTER TABLE exports ADD COLUMN output_authorization_json TEXT NOT NULL DEFAULT 'null';
ALTER TABLE exports ADD COLUMN provenance_json TEXT NOT NULL DEFAULT '{}';
ALTER TABLE exports ADD COLUMN provenance_path TEXT NOT NULL DEFAULT '';
ALTER TABLE exports ADD COLUMN provenance_sha256 TEXT NOT NULL DEFAULT '';
ALTER TABLE exports ADD COLUMN provenance_state TEXT NOT NULL DEFAULT 'pending';
ALTER TABLE exports ADD COLUMN provenance_error TEXT NOT NULL DEFAULT '';

-- Legacy versions did not enforce one export per profile/job. Retain the
-- earliest deterministic record and detach later duplicates before indexing.
UPDATE exports
SET job_id = NULL
WHERE job_id IS NOT NULL
  AND EXISTS (
    SELECT 1
    FROM exports retained
    WHERE retained.profile_id = exports.profile_id
      AND retained.job_id = exports.job_id
      AND (
        retained.created_at < exports.created_at OR
        retained.created_at = exports.created_at AND retained.id < exports.id
      )
  );

CREATE UNIQUE INDEX IF NOT EXISTS exports_profile_job_idx ON exports(profile_id, job_id) WHERE job_id IS NOT NULL;
