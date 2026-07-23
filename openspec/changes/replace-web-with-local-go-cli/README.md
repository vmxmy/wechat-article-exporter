# replace-web-with-local-go-cli

Replace the Nuxt web app and remote MCP/OAuth services with a local-first Go + Cobra + Bubble Tea binary that preserves the existing product capabilities.

## Decision summary

- The product is one native `wechat-article` binary. Cobra automation, the Bubble Tea workspace, and local stdio MCP are presentation adapters over the same application core.
- Persistent user data is profile-scoped local data: SQLite is the metadata authority, and article HTML/resources are stored in a SHA-256 content-addressed object store. Secrets remain in an OS credential store or the explicitly initialized encrypted vault.
- `mp.ziikoo.app` and `mptext.ziikoo.app` were both historical project-operated services. The former hosted the Nuxt/Nitro Web product and the latter hosted remote MCP/OAuth. They are not runtime dependencies of the replacement binary.
- The historical Web product was not a pure local-storage application: browser Dexie/IndexedDB held part of the library, while login, discovery, selected downloads, PDF rendering, remote MCP, KV, and D1 behavior used online project infrastructure.
- In the replacement product, reading or synchronizing an article does not traverse either historical project domain. Network traffic goes directly to approved WeChat hosts or through a proxy the user explicitly configures; credential-bearing requests are limited to direct or explicitly credential-trusted routes.
- The Web, Nitro, Cloudflare, remote OAuth, and remote MCP implementations are retired from the main branch. Their final reproducible source, migration evidence, and rollback boundary are preserved in the immutable Web-capable archive described by the project documentation.

## Artifacts

- `proposal.md`: motivation, breaking changes, capability inventory, and impact.
- `design.md`: target architecture, data/security model, testing strategy, migration phases, risks, and finalized implementation choices.
- `specs/*/spec.md`: normative requirements and acceptance scenarios for all 12 capabilities.
- `tasks.md`: dependency-ordered implementation and retirement checklist.

The change currently defines 12 capabilities. The exact normative-requirement, acceptance-scenario, and implementation-task counts are derived by the validation commands below so this index does not become stale as the contract is refined.

## Reading order

1. Read `proposal.md` for the product decision, breaking changes, and scope.
2. Read `design.md` for architecture, persistence, security, job, export, migration, and release decisions.
3. Read the capability specs for normative `SHALL` behavior and executable acceptance scenarios.
4. Read `tasks.md` for implementation ordering and completion evidence boundaries.

The highest acceptance seam is the shared application layer. Cobra, Bubble Tea, and MCP contract tests verify adapter parity, while protocol fixtures, SQLite/object-store tests, job recovery tests, exporter structural tests, and native release receipts cover adapter-specific behavior.

## Scope summary

The target product is one native binary with three adapters—Cobra, Bubble Tea, and local stdio MCP—over a shared local application core. It performs WeChat login and data access locally, persists metadata in SQLite and large content in a content-addressed object store, keeps secrets in platform credential storage or an encrypted fallback, and removes the Nuxt/Nitro/Cloudflare/remote-MCP stack only after a compatibility release and a mandatory parity gate.

## Acceptance status

- OpenSpec artifacts: complete (`proposal`, `design`, `specs`, and `tasks`).
- Mandatory functional parity: signed 24/24 by the executable parity gate.
- Repository Web/cloud retirement: complete through task 17.8.
- Remaining external evidence: task 17.9 requires artifact-bound receipts for final clean-room installation, real legacy migration, controlled live login/session restart, synchronization, download, every export format, native-PTY TUI, Cobra, stdio MCP, platform secret persistence, independent backup/restore, process-tree network exclusion, leakage scanning, and OS-enforced offline operation on every supported native platform. It remains unchecked until all five platform receipts and their aggregate release receipt set pass; fixture relabeling, source harnesses, cross-compilation, emulation, or unit tests do not satisfy it.

## Validation

```bash
openspec validate replace-web-with-local-go-cli --strict
openspec status --change replace-web-with-local-go-cli
rg -n '^\s*- \[ \]' openspec/changes/replace-web-with-local-go-cli/tasks.md
rg -c '^### Requirement:' openspec/changes/replace-web-with-local-go-cli/specs/*/spec.md
rg -c '^#### Scenario:' openspec/changes/replace-web-with-local-go-cli/specs/*/spec.md
```
