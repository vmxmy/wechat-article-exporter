# Database compatibility policy

## Compatibility window

Every stable `wechat-article` release must open and upgrade databases from the earliest stable local-CLI schema
baseline through the schema bundled in that release. The first compatibility window is therefore:

| Baseline | Status | Upgrade obligation |
| --- | --- | --- |
| Schema 1 (`001_initial.sql`) | Minimum supported baseline | Must upgrade directly to the current schema without data loss. |
| Schema 2 (`002_discovery_fields.sql`) | Supported baseline | Must upgrade directly while preserving discovery fields and user data. |
| Schema 3 (`003_reply_checkpoints.sql`) | Supported baseline | Must upgrade while preserving reply continuation checkpoints. |
| Schema 4 (`004_export_provenance.sql`) | Supported baseline | Must upgrade while preserving export files and adding provenance metadata. |
| Schema 5 (`005_export_provenance_state.sql`) | Supported baseline | Must upgrade while preserving provenance state and failure diagnostics. |
| Schema 6 (`006_export_provenance_generation.sql`) | Supported baseline | Must upgrade while preserving generation fencing and stale-writer recovery state. |
| Schema 7 (`007_scheduler_permits.sql`) | Supported baseline | Must upgrade while preserving cross-process scheduler permits. |
| Schema 8 (`008_export_provenance_unavailable.sql`) | Current schema | Must preserve the terminal unavailable state for legacy provenance that cannot be reconstructed truthfully. |

Schema 0 or databases without `schema_migrations` are not a promised release baseline. Legacy browser/Dexie data uses
the separately versioned import archive rather than SQLite schema migration. A database whose recorded schema is newer
than the running binary is never downgraded; the binary refuses to open it for writes and directs the user to upgrade
the CLI or restore a compatible backup.

The minimum baseline may advance only in a stable release that announces the change and leaves at least one stable
release capable of bridging from the previous minimum to the new minimum. Dropping a baseline requires release notes,
an upgrade path, and retained fixtures or tags sufficient to reproduce the bridge. Patch releases do not narrow the
window.

## Migration rules

- Every persistent DDL change is an ordered, embedded SQL migration. Runtime feature paths may verify required
  schema objects, but they do not create schema or define schema history.
- Migration numbers are contiguous and immutable after release. A released migration is never edited or reordered;
  corrections use the next number.
- Opening an older supported database creates a SQLite snapshot beside the database before applying migrations.
- Each migration is applied transactionally where SQLite permits it, and `schema_migrations` is updated only in the
  same successful transaction.
- Release binaries use `modernc.org/sqlite` with `CGO_ENABLED=0` on every supported target.

## Release gates

Release CI reconstructs every promised baseline from the embedded migration that originally created it, seeds related
profile/account/article/comment/reply data, and opens it with the candidate binary's library code. The gate verifies:

- all ordered migrations are recorded exactly once and the final version equals the current schema;
- seeded user data and foreign-key relationships survive;
- discovery columns, reply checkpoints, export provenance generations, and scheduler-permit tables/indexes exist;
- a readable pre-migration snapshot retains the source schema and data;
- current-schema reopen is idempotent;
- newer schemas are rejected and a failed migration does not record a version or leave partial tables.

The same workflow runs backup creation, independent verification, restore rollback/conflict tests, SQLite integrity
tests, and cross-platform binary smoke tests. A release must not be published when any compatibility baseline or
backup/restore gate fails.

## Adding a schema version

1. Add the next contiguous `NNN_description.sql` file; do not modify an already released migration. Schema 3 moves
   `reply_checkpoints` out of runtime lazy DDL, schemas 4–6 add fenced export provenance, schema 7 adds
   cross-process scheduler permits, and schema 8 adds the terminal unavailable provenance contract.
2. Set the library's current schema version to `NNN` while keeping the minimum at the documented compatibility floor.
3. Extend the migration assertions for the new objects or transformed data. The baseline matrix automatically adds
   the new current baseline and continues testing every older promised baseline.
4. Update the table above and release notes. If the minimum changes, document and test the required bridge release.
