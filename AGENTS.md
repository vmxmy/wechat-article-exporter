# Repository Guidelines

## Project Structure & Module Organization
The product is the Go module in `cli/`. Cobra composition lives in `cli/internal/app/`, the shared use-case layer in `cli/internal/application/`, and domain modules in `wechat/`, `network/`, `library/`, `objects/`, `jobs/`, `processor/`, `exporter/`, `profiles/`, and `secrets/`. Bubble Tea is in `tui/`; the local stdio protocol is in `mcp/`. Keep sanitized protocol and parser fixtures under `cli/internal/*/testdata/`, `samples/`, and `test/fixtures/protocol/`. Historical retirement evidence belongs under `docs/archive/`, `docs/migration/`, or `docs/release/`.

## Build, Test, and Development Commands
Use Go `>=1.25`.

- `cd cli && go mod download`: install module dependencies.
- `cd cli && go run ./cmd/wechat-article help`: run the Cobra CLI from source.
- `cd cli && go run ./cmd/wechat-article`: open Bubble Tea in a real TTY.
- `cd cli && go build -trimpath -o ./bin/wechat-article ./cmd/wechat-article`: build a local binary.
- `cd cli && go test ./...`: run unit and integration tests.
- `cd cli && go test -race ./...`: run concurrency checks.
- `cd cli && go vet ./...`: run the standard static analyzer.
- `cd cli && go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 -checks='SA*,S1*,QF*' ./...`: run release static checks.

## Architecture and Coding Style
Run `gofmt` on every changed Go file. Cobra, Bubble Tea, and MCP are presentation adapters over `application.Application`; do not duplicate protocol, persistence, scheduling, parsing, or export rules in adapters. SQLite is the metadata authority, large content belongs in the SHA-256 object store, and long-running work belongs in persistent jobs. Preserve platform build tags and `CGO_ENABLED=0` release support.

## Testing Guidelines
Add focused tests beside the package. Processor/exporter changes require sanitized fixture or golden coverage. Database changes require ordered migrations and upgrade tests from every supported baseline. Job/network changes require deterministic clock, retry, lease, cancellation, and redaction coverage. MCP tests must verify stdout protocol purity, bounded messages, EOF/cancellation, policies, and exact confirmations.

## Commit & Pull Request Guidelines
Recent history uses short version bumps plus concise `feat:` and `fix:` subjects. Prefer descriptive commit messages such as `fix: handle empty CGI payload in exporter` and keep each commit narrowly scoped. PRs should explain the user-visible change, link the relevant issue when available, note any config or deployment impact, and include screenshots for UI changes. Call out test coverage and known gaps explicitly.

## Security & Configuration Tips
Do not commit live WeChat credentials, exported article data, or local OMX state. Runtime configuration is environment-driven; common variables include `NUXT_AGGRID_LICENSE`, `NUXT_SENTRY_*`, `NUXT_UMAMI_*`, and Nitro KV settings.
