-- Legacy terminal exports can lack immutable producer/source metadata needed
-- to construct a truthful provenance manifest. Keep that terminal outcome
-- distinct from retryable finalization failures so recovery does not loop.
-- The state column is intentionally open-ended for forward compatibility.
SELECT 1;
