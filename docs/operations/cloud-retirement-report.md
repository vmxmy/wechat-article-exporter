# Cloud retirement report

- Retirement date: 2026-07-22 (Asia/Shanghai)
- Scope: project-operated Web, remote MCP/OAuth, Cloudflare Pages, Worker, KV, D1, custom hostnames, and production bindings for `wechat-article-exporter`
- Data handling: this report contains resource identifiers and negative verification only. It contains no API token, OAuth token, WeChat credential, article body, user identifier, or secret value.

## Preconditions and data inventory

The final Web-capable source and rollback instructions were preserved before shutdown. The compatibility release and mandatory parity gate were already signed off. Before deletion:

- the three target KV namespaces each contained `0` keys;
- the D1 database contained `0` rows in `cache_entries`;
- D1 contained only cache/schema support tables and indexes, including `_cache_schema_meta`, `_cf_KV`, and `cache_entries`;
- no user article or Credential bytes were found in the resources being retired.

The project did not retain cloud copies of local SQLite, object-store data, OS-keyring entries, encrypted-vault bytes, or exported article archives.

## Deleted or detached resources

| Resource | Identifier | Result |
| --- | --- | --- |
| Cloudflare Worker | `wechat-article-mcp` | Deleted |
| Worker OAuth KV | `d44eb3b7fd744963a18e46a934b72d93` | Deleted after zero-key check |
| Worker OAuth KV | `ce9220f85744491dad21c7bc9c8ec39a` | Deleted after zero-key check |
| Cloudflare Pages custom hostname | historical Web hostname | Detached before project deletion |
| Cloudflare Pages project | `wechat-article-exporter` | Deleted |
| Pages KV | `22d46ff673c946f1a5f3b790818b5618` | Deleted after zero-key check |
| D1 database | `wechat-article-cache` / `361958fa-c8df-4828-ac1c-ad77f23f2924` | Deleted after zero-row check |

Repository deployment workflows, Worker source, Pages configuration, bindings, remote OAuth/MCP clients, and Web runtime code were removed in the retirement commit. Provider-side resource deletion invalidates their bindings; no reusable secret value is recorded in source or this receipt.

## Post-deletion negative verification

The shutdown was verified through authoritative Cloudflare APIs rather than local DNS, because the local network maps external DNS through synthetic `198.18.0.0/15` addresses.

- Worker lookup returned not found (`404`, Cloudflare code `10007`).
- Pages project lookup returned not found (`404`, Cloudflare code `8000007`).
- filtered KV namespace listing returned an empty result for all three identifiers.
- filtered D1 listing returned an empty result for `361958fa-c8df-4828-ac1c-ad77f23f2924`.
- Cloudflare DNS API returned no records for either retired product hostname.
- repository production-reference search found no retired-domain references outside explicitly historical OpenSpec, migration, release, parity, or archive evidence.

Any future negative verification failure blocks a retirement-complete claim and requires incident review before a release.

## Logs, analytics, and retention

No Logpush dataset or project-owned article-content archive was attached to the deleted Worker, Pages project, KV namespaces, or D1 database. Provider audit/security records that Cloudflare retains independently follow the provider/account retention policy and are not application data stores. Historical Sentry/Umami configuration was removed with the retired Web application; no current Go runtime sends article content, credentials, sessions, or diagnostic bundles to either service.

The local product keeps debug incidents and completed job logs in the user's profile only. Garbage collection defaults to a 30-day retention window for both categories and requires an explicit dry run plus exact confirmation before deletion.

## Rollback boundary

Normal operation cannot restore these resources and contains no deployment credentials or bindings. Emergency historical-service recovery would require the immutable final Web-capable archive, a separately authorized infrastructure rebuild, and newly provisioned secrets. It must not read, modify, downgrade, or import the user's current local SQLite database.
