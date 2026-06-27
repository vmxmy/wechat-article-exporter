import type { H3Event } from 'h3';
import { getTokenFromEvent, resolveAuthKeyFromEvent } from '~/server/services/api/auth-session';
import {
  D1_LIST_LIMIT,
  type D1CacheDeleteRequestEntry,
  type D1CacheWriteRequestEntry,
  isCloudMirroredTable,
} from '~/shared/utils/d1-cache';

interface D1StatementLike {
  bind(...values: unknown[]): {
    run(): Promise<unknown>;
    first<T = unknown>(): Promise<T | null>;
    all<T = unknown>(): Promise<{ results: T[] }>;
  };
}

interface D1DatabaseLike {
  exec(query: string): Promise<unknown>;
  prepare(query: string): D1StatementLike;
}

// owner_key 隔离在 v2 引入：v1 的行没有用户维度（全局共享，可被任意登录用户越权读写）。
// v3 引入 scope_key（账号维度作用域过滤），支持账号级 list 对账。
const MIRROR_SCHEMA_VERSION = 3;
const SCHEMA_META_TABLE = '_cache_schema_meta';

const initializedTables = new Set<string>();

async function sha256Hex(input: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(input));
  return Array.from(new Uint8Array(digest))
    .map(byte => byte.toString(16).padStart(2, '0'))
    .join('');
}

function sanitizeIdentifier(value: string): string {
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(value)) {
    throw createError({
      statusCode: 500,
      statusMessage: `Invalid D1 identifier: ${value}`,
    });
  }

  return value;
}

function getD1Database(event: H3Event): D1DatabaseLike | null {
  // 必须传 event：Cloudflare 上运行时 env（NUXT_*）覆盖只在 useRuntimeConfig(event) 时生效。
  const config = useRuntimeConfig(event);
  const bindingName = sanitizeIdentifier(config.d1Cache.binding);
  const cloudflareEnv = (event.context as { cloudflare?: { env?: Record<string, unknown> } }).cloudflare?.env;
  const db = cloudflareEnv?.[bindingName];

  if (!db) {
    return null;
  }

  return db as D1DatabaseLike;
}

function createMirrorTableSql(tableName: string): string {
  return `CREATE TABLE IF NOT EXISTS ${tableName} (owner_key TEXT NOT NULL, cache_table TEXT NOT NULL, entry_key TEXT NOT NULL, scope_key TEXT, payload_json TEXT NOT NULL, blob_field TEXT, blob_type TEXT, blob_payload BLOB, updated_at INTEGER NOT NULL, PRIMARY KEY (owner_key, cache_table, entry_key))`;
}

async function ensureMirrorTable(db: D1DatabaseLike, tableName: string): Promise<void> {
  if (initializedTables.has(tableName)) {
    return;
  }

  await db.exec(
    `CREATE TABLE IF NOT EXISTS ${SCHEMA_META_TABLE} (table_name TEXT PRIMARY KEY, version INTEGER NOT NULL)`
  );

  const meta = await db
    .prepare(`SELECT version FROM ${SCHEMA_META_TABLE} WHERE table_name = ?`)
    .bind(tableName)
    .first<{ version: number }>();

  const currentVersion = meta?.version ?? 0;
  if (currentVersion < MIRROR_SCHEMA_VERSION) {
    if (currentVersion < 2) {
      // <v2 无 owner_key（主键结构不同），且本是越权共享的脏镜像，DROP 重建（丢失无损）。createMirrorTableSql 已是 v3 形态。
      await db.exec(`DROP TABLE IF EXISTS ${tableName}`);
      await db.exec(createMirrorTableSql(tableName));
    } else {
      // v2→v3 仅新增 scope_key 列、主键不变，增量 ALTER 不丢数据（与“从 D1 对账拉回本地”的新语义一致）。
      await db.exec(`ALTER TABLE ${tableName} ADD COLUMN scope_key TEXT`);
    }
    // scope_key 索引：账号级 list 对账按 (owner_key, cache_table, scope_key) 行级过滤（两条路径都需要，IF NOT EXISTS 幂等）。
    await db.exec(
      `CREATE INDEX IF NOT EXISTS ${tableName}_scope_idx ON ${tableName} (owner_key, cache_table, scope_key)`
    );
    await db
      .prepare(
        `INSERT INTO ${SCHEMA_META_TABLE} (table_name, version) VALUES (?, ?) ON CONFLICT(table_name) DO UPDATE SET version = excluded.version`
      )
      .bind(tableName, MIRROR_SCHEMA_VERSION)
      .run();
  }

  initializedTables.add(tableName);
}

function decodeBase64(base64: string): Uint8Array {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);

  for (let index = 0; index < binary.length; index++) {
    bytes[index] = binary.charCodeAt(index);
  }

  return bytes;
}

function encodeBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary);
}

function normalizeWriteEntry(entry: D1CacheWriteRequestEntry): D1CacheWriteRequestEntry {
  if (!entry || typeof entry !== 'object') {
    throw createError({ statusCode: 400, statusMessage: 'entries must be objects' });
  }

  if (!entry.table || !entry.key || !entry.payload || typeof entry.payload !== 'object') {
    throw createError({ statusCode: 400, statusMessage: 'entry.table, entry.key and entry.payload are required' });
  }

  if (!isCloudMirroredTable(entry.table)) {
    throw createError({ statusCode: 400, statusMessage: `unsupported cache table: ${entry.table}` });
  }

  // scopeKey 向后兼容：旧客户端可缺省为 null，但若提供必须是字符串。
  if (entry.scopeKey != null && typeof entry.scopeKey !== 'string') {
    throw createError({ statusCode: 400, statusMessage: 'entry.scopeKey must be a string' });
  }

  return entry;
}

function normalizeDeleteEntry(entry: D1CacheDeleteRequestEntry): D1CacheDeleteRequestEntry {
  if (!entry || typeof entry !== 'object') {
    throw createError({ statusCode: 400, statusMessage: 'delete entries must be objects' });
  }
  if (!entry.table || !entry.key) {
    throw createError({ statusCode: 400, statusMessage: 'entry.table and entry.key are required' });
  }
  if (!isCloudMirroredTable(entry.table)) {
    throw createError({ statusCode: 400, statusMessage: `unsupported cache table: ${entry.table}` });
  }
  return entry;
}

function chunkArray<T>(arr: T[], size: number): T[][] {
  const chunks: T[][] = [];
  for (let i = 0; i < arr.length; i += size) {
    chunks.push(arr.slice(i, i + size));
  }
  return chunks;
}

export default defineEventHandler(async event => {
  // 传 event：Cloudflare 上 NUXT_D1_CACHE_ENABLED 等运行时 env 覆盖只在 useRuntimeConfig(event) 时注入。
  const config = useRuntimeConfig(event);

  if (!config.d1Cache.enabled) {
    throw createError({
      statusCode: 503,
      statusMessage: 'D1 cache mirror is disabled',
    });
  }

  const token = await getTokenFromEvent(event);
  if (!token) {
    throw createError({ statusCode: 401, statusMessage: 'Unauthorized' });
  }

  // 按账号隔离：owner_key = sha256(authKey)，由服务端从会话派生，客户端无权传入。
  const authKey = await resolveAuthKeyFromEvent(event);
  if (!authKey) {
    throw createError({ statusCode: 401, statusMessage: 'Unauthorized' });
  }
  const ownerKey = await sha256Hex(authKey);

  const body = await readBody<{
    action?: string;
    entries?: unknown[];
    table?: string;
    key?: string;
    scopeKey?: string;
  }>(event);

  const action = body?.action ?? 'upsert';

  const db = getD1Database(event);
  if (!db) {
    throw createError({
      statusCode: 503,
      statusMessage: 'D1 binding is unavailable in the current runtime',
    });
  }

  const tableName = sanitizeIdentifier(config.d1Cache.table);
  await ensureMirrorTable(db, tableName);

  // --- read ---
  if (action === 'read') {
    if (!body?.table || !body?.key) {
      throw createError({ statusCode: 400, statusMessage: 'table and key are required for read' });
    }
    if (!isCloudMirroredTable(body.table)) {
      throw createError({ statusCode: 400, statusMessage: `unsupported cache table: ${body.table}` });
    }

    const row = await db
      .prepare(
        `SELECT payload_json, blob_field, blob_type, blob_payload FROM ${tableName} WHERE owner_key = ? AND cache_table = ? AND entry_key = ? LIMIT 1`
      )
      .bind(ownerKey, body.table, body.key)
      .first<{
        payload_json: string;
        blob_field: string | null;
        blob_type: string | null;
        blob_payload: ArrayBuffer | null;
      }>();

    if (!row) {
      return { ok: true, found: false };
    }

    return {
      ok: true,
      found: true,
      payload: JSON.parse(row.payload_json),
      blobField: row.blob_field,
      blobType: row.blob_type,
      blobBase64: row.blob_payload ? encodeBase64(row.blob_payload) : null,
    };
  }

  // --- list（账号级对账） ---
  if (action === 'list') {
    if (!body?.table) {
      throw createError({ statusCode: 400, statusMessage: 'table is required for list' });
    }
    if (!isCloudMirroredTable(body.table)) {
      throw createError({ statusCode: 400, statusMessage: `unsupported cache table: ${body.table}` });
    }
    if (body.scopeKey != null && typeof body.scopeKey !== 'string') {
      throw createError({ statusCode: 400, statusMessage: 'scopeKey must be a string' });
    }

    // owner_key 由服务端派生，客户端无权传入；scope_key 存在时追加行级过滤。
    const hasScope = body.scopeKey != null;
    // ORDER BY entry_key 保证截断子集确定（可配合后续 cursor 分页），否则 LIMIT 取到的是非确定行。
    const sql = hasScope
      ? `SELECT entry_key, payload_json, blob_field, blob_type, blob_payload FROM ${tableName} WHERE owner_key = ? AND cache_table = ? AND scope_key = ? ORDER BY entry_key LIMIT ${D1_LIST_LIMIT + 1}`
      : `SELECT entry_key, payload_json, blob_field, blob_type, blob_payload FROM ${tableName} WHERE owner_key = ? AND cache_table = ? ORDER BY entry_key LIMIT ${D1_LIST_LIMIT + 1}`;

    const statement = db.prepare(sql);
    const bound = hasScope ? statement.bind(ownerKey, body.table, body.scopeKey) : statement.bind(ownerKey, body.table);

    const { results } = await bound.all<{
      entry_key: string;
      payload_json: string;
      blob_field: string | null;
      blob_type: string | null;
      blob_payload: ArrayBuffer | null;
    }>();

    const truncated = results.length > D1_LIST_LIMIT;
    const rows = truncated ? results.slice(0, D1_LIST_LIMIT) : results;
    const items = rows.map(row => ({
      key: row.entry_key,
      payload: JSON.parse(row.payload_json),
      blobField: row.blob_field,
      blobType: row.blob_type,
      blobBase64: row.blob_payload ? encodeBase64(row.blob_payload) : null,
    }));

    return { ok: true, items, truncated };
  }

  // --- delete ---
  if (action === 'delete') {
    const rawEntries = Array.isArray(body?.entries) ? body.entries : [];
    if (rawEntries.length === 0) {
      return { ok: true, deleted: 0 };
    }

    const entries = rawEntries.map(e => normalizeDeleteEntry(e as D1CacheDeleteRequestEntry));

    // group by table, then chunk by 80 (D1 bind limit ~100, leave headroom)
    const byTable = new Map<string, string[]>();
    for (const entry of entries) {
      const keys = byTable.get(entry.table) ?? [];
      keys.push(entry.key);
      byTable.set(entry.table, keys);
    }

    for (const [table, keys] of byTable) {
      for (const chunk of chunkArray(keys, 80)) {
        const placeholders = chunk.map(() => '?').join(',');
        await db
          .prepare(
            `DELETE FROM ${tableName} WHERE owner_key = ? AND cache_table = ? AND entry_key IN (${placeholders})`
          )
          .bind(ownerKey, table, ...chunk)
          .run();
      }
    }

    return { ok: true, deleted: entries.length };
  }

  // --- upsert (default) ---
  const entries = Array.isArray(body?.entries)
    ? (body.entries as D1CacheWriteRequestEntry[]).map(normalizeWriteEntry)
    : [];

  if (entries.length === 0) {
    throw createError({ statusCode: 400, statusMessage: 'entries is required' });
  }

  const statement = db.prepare(`
    INSERT INTO ${tableName} (
      owner_key,
      cache_table,
      entry_key,
      scope_key,
      payload_json,
      blob_field,
      blob_type,
      blob_payload,
      updated_at
    ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
    ON CONFLICT(owner_key, cache_table, entry_key) DO UPDATE SET
      scope_key = excluded.scope_key,
      payload_json = excluded.payload_json,
      blob_field = excluded.blob_field,
      blob_type = excluded.blob_type,
      blob_payload = excluded.blob_payload,
      updated_at = excluded.updated_at
  `);

  const updatedAt = Math.floor(Date.now() / 1000);

  for (const entry of entries) {
    const blobPayload = entry.blobBase64 ? decodeBase64(entry.blobBase64) : null;

    await statement
      .bind(
        ownerKey,
        entry.table,
        entry.key,
        entry.scopeKey ?? null,
        JSON.stringify(entry.payload),
        entry.blobField || null,
        entry.blobType || null,
        blobPayload,
        updatedAt
      )
      .run();
  }

  return {
    ok: true,
    written: entries.length,
  };
});
