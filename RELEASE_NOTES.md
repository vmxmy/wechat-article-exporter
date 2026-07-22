# wechat-article v2.1.0

This is the first post-retirement release of `wechat-article`. The repository and runtime are now centered exclusively on the local Go product: Cobra commands, a Bubble Tea terminal workspace, and a local stdio MCP server over one shared application core.

## Highlights

- Local WeChat QR login with profile-isolated session storage in the operating-system credential store, plus an explicitly initialized encrypted-vault fallback.
- Local SQLite library and SHA-256 content-addressed object store for accounts, articles, albums, resources, jobs, comments, metrics, and exports.
- Resumable account/article/album synchronization, article and resource download, comments/replies, engagement metadata, and credential-aware paid-content requests.
- HTML, Markdown, text, JSON, Excel, DOCX, and local-Chromium PDF exports with stable manifests, checksums, atomic output, and resume support.
- Cobra automation, Bubble Tea TUI, and local `wechat-article mcp serve --transport stdio` adapters that do not require project-operated Web, KV, D1, or remote MCP services.
- Backup, independent verification, transactional restore, integrity checks, garbage collection, diagnostics, trusted-proxy policy, and legacy Web archive import/verification.
- The retired Nuxt, Nitro, Cloudflare Pages, Worker MCP/OAuth, Docker Web, remote client, and JavaScript production surfaces have been removed.

## Network, data, and secret changes

Commands connect from the local process directly to WeChat, or to a proxy explicitly configured by the user. Sensitive requests may use only direct transport or a proxy explicitly marked `credential-trusted`.

Profile metadata is stored in platform-standard local directories. Article bodies and resources remain on the user's machine. WeChat sessions, article credentials, and proxy authorization are excluded from normal JSON output, diagnostics, and default backups.

See the main README and `docs/operations/local-data-security.md` for resolved paths and the complete security model.

## Install and migrate

Download the archive for your operating system from this GitHub Release and verify it against `checksums.txt`. Release assets include macOS Intel/Apple Silicon, Linux x86-64/ARM64, and Windows x86-64 builds, plus per-target CycloneDX SBOMs.

Users who already created a versioned Web archive can import and verify it with:

```text
wechat-article migration inspect <archive.zip>
wechat-article migration import <archive.zip>
wechat-article migration verify <archive.zip>
```

Retired remote-CLI OAuth tokens are not accepted as local WeChat credentials. Create or select a local profile, then run `wechat-article login` and scan a new QR code.

Full installation, upgrade, rollback, and migration guidance:

- `docs/getting-started/install-and-upgrade.md`
- `docs/migration/local-cli-transition.md`
- `docs/operations/backup-restore.md`

## Retirement state

Project-operated Web, remote MCP, and remote OAuth services are retired. Historical source and rollback instructions remain available only in the final Web-capable archive and its immutable tag. The local database is never downgraded or modified by any historical-service recovery procedure.
