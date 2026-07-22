# SQLite driver and CGO policy

The local Go product uses `modernc.org/sqlite` and keeps `CGO_ENABLED=0` as the release policy.

Reasons:

- The existing release workflow cross-compiles macOS amd64/arm64, Linux amd64/arm64, and Windows amd64 from one Linux runner.
- `modernc.org/sqlite` provides a `database/sql` driver without a C compiler or platform-specific SQLite library.
- A CGO driver would require native or cross C toolchains for every release target and would make reproducible release builds substantially harder.

Database connections enable foreign keys, WAL mode, a bounded busy timeout, and explicit synchronous behavior. The application limits open connections so every connection receives the same pragmas. Platform smoke tests must cover WAL recovery, backup/restore, and Windows file locking.

The chosen driver and its transitive `modernc.org/libc` version are pinned in `cli/go.mod`/`cli/go.sum`; maintainers should upgrade them together and run the database migration and cross-build matrices.
