# Browser workspace capability matrix

Status date: 2026-07-24. This matrix describes the current local product rather than the retired hosted Web product. “Supported” means the adapter has a user-facing route and executable or focused test evidence; it does not mean every CLI flag is copied into a browser control.

| Workflow | Cobra | TUI | stdio MCP | Local browser workspace | Notes / evidence |
| --- | --- | --- | --- | --- | --- |
| Select and isolate profile | Supported | Supported | Startup active profile | Supported (uses active profile) | Browser reads the selected profile; restart it after `profile use`. |
| QR login, session status, logout | Supported | Supported | Supported where policy permits | Supported | Browser uses the same application session seam; no session secret is exposed. |
| Account search, add/manage, sync | Supported | Supported | Supported where policy permits | Supported | Browser account views use bounded API pages and durable jobs. |
| Paginated article search/filter/select | Supported | Supported | Supported | Supported | Browser performs server-side bounded pages; it does not load the complete library. |
| Single article URL ingestion | Supported | Supported | Supported | Supported | Browser returns a persistent job ID. |
| Article preview, resource download, metadata/comments | Supported | Supported | Supported where policy permits | Supported | Browser queues the same persistent article/metadata/comment/resource jobs and opens only local preview state. |
| Album traversal and batch download/export | Supported | Supported | Supported where policy permits | Traversal and batch download supported; batch export not complete | Browser can list albums, traverse one selected album and queue its batch download; use Cobra/TUI/MCP for batch export workflows. |
| Jobs, logs and permitted controls | Supported | Supported | Supported where policy permits | Supported | Browser exposes bounded job detail (job, items, logs, and lease) and shared-engine transitions. |
| Export HTML/Markdown/TXT/JSON/XLSX/DOCX/PDF | Supported | Supported | Supported within allowed roots | Supported | Browser authorizes a default root or descendant through an opaque directory token. |
| Export manifest and verification | Supported | Supported | Supported | Supported | Browser shows safe manifest metadata and verification results. |
| Export artifact streaming / open selected output | Supported | Supported | Not applicable | Supported | Browser streams only manifest-listed artifacts through opaque artifact capabilities. Opening an export output requires that export's exact confirmation and never accepts a host path. |
| Credentials and proxy policies | Supported | Supported | Restricted by profile policy | Supported | Browser fields are write-only; exact confirmation remains required for sensitive actions. |
| Preferences and language | Supported | Supported | Supported where policy permits | Supported | `display.language` accepts only `en` and `zh-CN`; browser offers both locales. |
| Backup, integrity, GC, diagnostics and diagnostic bundle | Supported | Supported | Supported where policy permits | Supported | The real web command wires maintenance facades. Browser supports backup create/verify, integrity, GC plan plus exact-confirmation apply, diagnostics, and opaque one-shot diagnostic-bundle download. |
| Backup restore / arbitrary upload | Supported | Supported | Supported within policy | Not supported | Use confirmed Cobra/TUI flow; no arbitrary host-path API exists. |
| Accessibility | Terminal accessibility modes | Keyboard-first terminal UI | Client-defined | Supported baseline | Browser has labelled controls, visible focus, keyboard navigation and live status; report gaps with diagnostics. |
| Transport / network surface | Local process outbound only | Local process outbound only | stdio only; no TCP | Random `127.0.0.1` loopback only | Browser uses one-time token, HttpOnly session, Host/Origin/CSRF checks and restrictive headers. |

## Completion rule

The browser workspace must remain “not complete” until the remaining partial or unsupported workflows—especially browser restore/archive upload—have an implemented user workflow and corresponding browser/API release evidence. It must not be advertised as a replacement for Cobra, TUI, or MCP before that point.

The clean-room candidate-binary receipt exercises loopback startup, authenticated embedded assets, required security headers, bounded browser API reads, a representative browser mutation, and no retired-domain observation using the sanitized loopback fixture. See [clean-room receipts](./clean-room-receipts.md).
