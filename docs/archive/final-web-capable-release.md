# Final Web-capable release archive

This document is the non-secret operational record for the final repository version that contains all of these surfaces together:

- the Nuxt Web application and browser-local Dexie library;
- Nitro APIs, Cloudflare Pages KV/D1 bindings, and local PDF rendering endpoint;
- the Cloudflare Worker remote MCP/OAuth adapter;
- the complete local `wechat-article` Go product and legacy Web archive migration tools.

The archive tag is created only after the compatibility release workflow has passed. Record the immutable tag and source commit in the release body and in the commands below; never archive from an uncommitted working tree.

## Immutable archive identity

- Archive tag: `wechat-article-web-final-2026-07-22`
- Source commit: `bda9f3bc3605c8c47e5b4f4cad20bf35c35844e4`
- Git tree: `ae633b07c33be7fc855148ef84e114eab492ab3b`
- Compatibility release: <https://github.com/vmxmy/wechat-article-exporter/releases/tag/wechat-article-v2.0.0>
- Final Web-capable archive release: <https://github.com/vmxmy/wechat-article-exporter/releases/tag/wechat-article-web-final-2026-07-22>

The archive release was published at `2026-07-22T14:42:57Z`. Its annotated tag resolves to the source commit and tree above. The tag is not claimed to carry a GPG signature.

The release contains a deterministic `git archive` source tarball and `checksums.txt`. The published checksum was verified after re-downloading both assets. The extracted archive was checked for every preserved behavior, fixture, schema, migration, documentation, Web, Nitro, and Worker path listed below.

## Preserved behavior and schemas

- Mandatory and intentionally retired behavior: `test/parity/matrix.json` and `docs/release/parity-report.md`.
- Sanitized protocol baselines: `test/fixtures/protocol/`.
- Parser and article fixtures: `samples/` and `cli/internal/processor/testdata/`.
- Legacy browser archive schema: `shared/migration/legacy-archive.ts` and `cli/internal/migration/schema.go`.
- Legacy migration acceptance coverage: `test/legacy-export/archive.test.ts` and `cli/internal/migration/migration_test.go`.
- SQLite compatibility policy and migrations: `docs/architecture/database-compatibility.md` and `cli/internal/library/migrations/`.

Fixtures must remain sanitized. This archive does not contain live QR images, WeChat sessions, cookies, article credentials, OAuth tokens, proxy authorization, private user exports, Cloudflare secret values, or production logs.

## Historical infrastructure inventory

The following names identify the compatibility infrastructure. IDs are operational identifiers, not credentials, but they are retained only here and in the archived tag after retirement:

| Surface | Historical identifier |
| --- | --- |
| Cloudflare Pages project | `wechat-article-exporter` |
| Pages custom domain | `mp.ziikoo.app` |
| Pages KV binding | `KV` |
| Pages D1 binding | `DB` / database `wechat-article-cache` |
| Remote MCP Worker | `wechat-article-mcp` |
| Remote MCP custom domain | `mptext.ziikoo.app` |
| Worker OAuth binding | `OAUTH_KV` |
| Worker migration variables | `REMOTE_OAUTH_DISABLE_AFTER`, `LOCAL_CLI_MIGRATION_URL` |

Secret values are intentionally absent. Obtain any still-authorized compatibility secret through the approved operator channel; do not reconstruct it from repository history or local user data.

## Reproducible validation

From the archived source tree:

```bash
corepack enable
corepack prepare yarn@1.22.22 --activate
yarn install --frozen-lockfile
yarn test:baseline
yarn test:api-core
./node_modules/.bin/vite-node -c test/vite.config.ts test/legacy-export/archive.test.ts
yarn build
yarn test:parity:gate

cd cli
go test ./...
go test -race ./...
go vet ./...
```

The release workflow additionally verifies native archives, checksums, SBOMs, fixture corpora, database upgrades, backup/restore, and native PTY smoke tests.

## Emergency rollback during the compatibility window

Rollback restores only the archived online services. It never downgrades, rewrites, or imports a user's local SQLite database.

1. Check out the exact final Web-capable tag in an isolated clean worktree.
2. Verify the tag, commit, release assets, `checksums.txt`, and this archive record.
3. Run the reproducible validation commands above.
4. Confirm the historical Pages project, Worker, KV, and D1 resources still exist and that their retention window has not expired.
5. Restore required secrets through the approved operator channel. Never copy user sessions, OAuth values, or article credentials from local databases or backups.
6. Build and deploy the Nuxt Pages artifact from the archived tag; deploy the Worker from `mcp-server/` only if remote MCP rollback is explicitly approved.
7. Verify `/health`, Web login, browser-local legacy export, remote migration response, and a synthetic non-user-data flow.
8. Announce the temporary rollback scope and a new retirement date. Keep local CLI migration instructions primary.
9. After the incident, reapply the shutdown procedure and verify that no remote service retained data beyond policy.

If the cloud resources or required secret material have already passed their documented retention boundary, online rollback is no longer supported. Fix-forward the local product instead.

## Retirement evidence

The retirement commit must reference:

- the immutable final Web-capable tag and GitHub Release;
- the compatibility release containing the native binary;
- the signed 24/24 mandatory parity report;
- cloud deletion or rotation receipts without secret values;
- log/data retention checks;
- final clean-room results for each supported platform.
