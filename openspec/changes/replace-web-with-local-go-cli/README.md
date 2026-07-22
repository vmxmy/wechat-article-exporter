# replace-web-with-local-go-cli

Replace the Nuxt web app and remote MCP/OAuth services with a local-first Go + Cobra + Bubble Tea binary that preserves the existing product capabilities.

## Artifacts

- `proposal.md`: motivation, breaking changes, capability inventory, and impact.
- `design.md`: target architecture, data/security model, testing strategy, migration phases, risks, and open questions.
- `specs/*/spec.md`: normative requirements and acceptance scenarios for all 11 capabilities.
- `tasks.md`: dependency-ordered implementation and retirement checklist.

## Scope summary

The target product is one native binary with three adapters—Cobra, Bubble Tea, and local stdio MCP—over a shared local application core. It performs WeChat login and data access locally, persists metadata in SQLite and large content in a content-addressed object store, keeps secrets in platform credential storage or an encrypted fallback, and removes the Nuxt/Nitro/Cloudflare/remote-MCP stack only after a compatibility release and a mandatory parity gate.

## Validation

```bash
openspec validate replace-web-with-local-go-cli --strict
openspec status --change replace-web-with-local-go-cli
```
