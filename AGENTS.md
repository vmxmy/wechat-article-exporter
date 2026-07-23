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

## Security and Retirement Boundaries
Do not commit WeChat sessions, credentials, article bodies from real users, local databases, exported archives, or agent state. Secrets stay in OS keyrings or the explicit encrypted vault and must be redacted before persistence or output. Sensitive traffic uses direct transport or an explicitly credential-trusted proxy. The Nuxt/Nitro/Web Worker/remote OAuth product is retired; do not reintroduce project-operated runtime dependencies or network-listening MCP as a shortcut. Historical domain and infrastructure references belong only in explicitly historical documentation.

## Image Generation
- For third-party or fixed-path image generation, prefer the official Codex CLI at `$IMAGE_GEN` instead of the built-in one-off image tool.
- The CLI reads `OPENAI_BASE_URL` and `OPENAI_API_KEY`; the key is loaded from macOS Keychain and must never be committed or printed.
- Before production use, verify `POST /v1/images/generations` with a low-quality 1024×1024 smoke test. A model appearing in `/v1/models` is not sufficient evidence that the Images API works.
- Save temporary inputs under `tmp/imagegen/` and final assets under `output/imagegen/`.
- Use `generate-batch --concurrency 5` for multiple distinct prompts; use `--n` only for variants of one prompt.
