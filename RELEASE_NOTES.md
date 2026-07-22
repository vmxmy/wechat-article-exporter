# wechat-article v2.0.0 — local compatibility release

This is the first stable local-first release of `wechat-article`. The native binary now provides the product through Cobra commands, a Bubble Tea terminal workspace, and a local stdio MCP server over one shared application core.

## Highlights

- Local WeChat QR login with profile-isolated session storage in the operating-system credential store, plus an explicitly initialized encrypted-vault fallback.
- Local SQLite library and SHA-256 content-addressed object store for accounts, articles, albums, resources, jobs, comments, metrics, and exports.
- Resumable account/article/album synchronization, article and resource download, comments/replies, engagement metadata, and credential-aware paid-content requests.
- HTML, Markdown, text, JSON, Excel, DOCX, and local-Chromium PDF exports with stable manifests, checksums, atomic output, and resume support.
- Cobra automation, Bubble Tea TUI, and local `wechat-article mcp serve --transport stdio` adapters that do not require project-operated Web, KV, D1, or remote MCP services.
- Backup, independent verification, transactional restore, integrity checks, garbage collection, diagnostics, trusted-proxy policy, and legacy Web archive import/verification.

## Network, data, and secret changes

Normal commands now connect from the local process directly to WeChat, or to a proxy explicitly configured by the user. They do not traverse `mp.ziikoo.app` or `mptext.ziikoo.app`. Sensitive requests may use only direct transport or a proxy explicitly marked `credential-trusted`.

Profile metadata is stored in platform-standard local directories. Article bodies and resources remain on the user's machine. WeChat sessions, article credentials, and proxy authorization are excluded from normal JSON output, diagnostics, and default backups.

See the main README and `docs/operations/local-data-security.md` for resolved paths and the complete security model.

## Install and migrate

Download the archive for your operating system from this GitHub Release and verify it against `checksums.txt`. Release assets include macOS Intel/Apple Silicon, Linux x86-64/ARM64, and Windows x86-64 builds, plus per-target CycloneDX SBOMs.

Existing Web users should keep their browser data until they have exported the versioned legacy archive and completed:

```text
wechat-article migration inspect <archive.zip>
wechat-article migration import <archive.zip>
wechat-article migration verify <archive.zip>
```

Legacy remote-CLI OAuth tokens are preserved only for rollback and are never imported as local WeChat credentials. Create or select a local profile, then run `wechat-article login` and scan a new QR code.

Full installation, upgrade, rollback, and migration guidance:

- `docs/getting-started/install-and-upgrade.md`
- `docs/migration/local-cli-transition.md`
- `docs/operations/backup-restore.md`

## Compatibility window and retirement

The existing Web application and remote MCP deployment remain available for compatibility rollback and browser-local data export in this release. Remote OAuth is deprecated for normal use.

The earliest planned retirement date is **2026-12-31**, and retirement remains conditional on the published compatibility release, the signed 24/24 mandatory parity audit, the final Web-capable archive, and the documented migration window. Any schedule change will be announced with a new dated notice.

The signed parity report and known intentional differences are recorded in `docs/release/parity-report.md`. The full staged retirement and rollback policy is in `docs/compatibility-retirement.md`.
