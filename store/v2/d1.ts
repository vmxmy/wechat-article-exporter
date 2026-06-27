import {
  type D1CacheDeleteRequestEntry,
  type D1CacheListResponse,
  type D1CacheTable,
  type D1CacheWriteOptions,
  type D1CacheWriteRequestEntry,
  type D1CloudTable,
  isCloudMirroredTable,
} from '~/shared/utils/d1-cache';

interface D1CacheEntry {
  table: D1CacheTable;
  key: string;
  record: Record<string, unknown>;
  // 该条目所属账号 fakeid，缺省时由 serializeEntry 从 record.fakeid 派生。
  scopeKey?: string;
}

// --- helpers ---

function arrayBufferToBase64(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  const chunkSize = 0x8000;
  let binary = '';

  for (let index = 0; index < bytes.length; index += chunkSize) {
    const chunk = bytes.subarray(index, index + chunkSize);
    binary += String.fromCharCode(...chunk);
  }

  return btoa(binary);
}

function base64ToArrayBuffer(base64: string): ArrayBuffer {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes.buffer;
}

// --- preference toggle ---

// 401 临时熔断：内存级、带冷却，到期自动恢复，不改用户持久偏好。
let circuitOpenUntil = 0;
const D1_CIRCUIT_COOLDOWN = 60_000;

function isCircuitOpen(): boolean {
  return Date.now() < circuitOpenUntil;
}

// 重新登录后可主动复位熔断（冷却到期也会自动恢复）。
export function resetD1Circuit(): void {
  circuitOpenUntil = 0;
}

// Reads d1MirrorEnabled from localStorage directly (not via usePreferences ref)
// so this module works outside Vue lifecycle contexts.
// explicit options.writeToD1 takes precedence; undefined falls back to preference.
export function resolveD1Toggle(options?: D1CacheWriteOptions): boolean {
  // 熔断期间暂停 D1（含强制写）：token 失效时强制也无意义，冷却到期自动恢复。
  if (isCircuitOpen()) return false;
  if (typeof options?.writeToD1 === 'boolean') return options.writeToD1;
  if (!import.meta.client) return false;
  try {
    const raw = localStorage.getItem('preferences');
    if (!raw) return false;
    return JSON.parse(raw)?.d1MirrorEnabled === true;
  } catch {
    return false;
  }
}

// --- 401 temporary circuit breaker ---

function disableD1OnUnauth(error: unknown): boolean {
  const status = (error as any)?.response?.status ?? (error as any)?.status;
  if (status !== 401) return false;
  // 内存级临时熔断：不再永久改写用户偏好（偶发 401 不应把默认开启的同步自我永久降级）；
  // 冷却到期或重新登录（resetD1Circuit）后自动恢复。
  circuitOpenUntil = Date.now() + D1_CIRCUIT_COOLDOWN;
  window.dispatchEvent(new CustomEvent('d1-auth-error'));
  return true;
}

// --- negative cache (avoids hammering D1 on repeated misses) ---

const negativeCacheMap = new Map<string, number>(); // key -> expiry ms
const NEGATIVE_CACHE_TTL = 5 * 60 * 1000;
const NEGATIVE_CACHE_MAX = 1000;

function isNegativeCached(key: string): boolean {
  const expiry = negativeCacheMap.get(key);
  if (expiry === undefined) return false;
  if (Date.now() > expiry) {
    negativeCacheMap.delete(key);
    return false;
  }
  return true;
}

function setNegativeCached(key: string): void {
  if (negativeCacheMap.size >= NEGATIVE_CACHE_MAX) {
    negativeCacheMap.delete(negativeCacheMap.keys().next().value!);
  }
  negativeCacheMap.set(key, Date.now() + NEGATIVE_CACHE_TTL);
}

// --- write ---

async function serializeEntry(entry: D1CacheEntry): Promise<D1CacheWriteRequestEntry> {
  const payload: Record<string, unknown> = {};
  let blobField: string | null = null;
  let blobType: string | null = null;
  let blobBase64: string | null = null;

  for (const [key, value] of Object.entries(entry.record)) {
    if (value instanceof Blob) {
      if (blobField) {
        throw new Error(`D1 mirror only supports one Blob field per record: ${entry.table}/${entry.key}`);
      }

      blobField = key;
      blobType = value.type || null;
      blobBase64 = arrayBufferToBase64(await value.arrayBuffer());
      payload[key] = null;
      continue;
    }

    payload[key] = value;
  }

  return {
    table: entry.table,
    key: entry.key,
    payload,
    blobField,
    blobType,
    blobBase64,
    // 优先用显式 scopeKey，回退到 record.fakeid（info 表 fakeid 即自身，天然满足 scope_key=自身 fakeid）。
    scopeKey: entry.scopeKey ?? (entry.record.fakeid as string | undefined) ?? null,
  };
}

export async function writeEntriesToD1(entries: D1CacheEntry[], options?: D1CacheWriteOptions): Promise<void> {
  // 仅元数据小表上云；Blob 表（html/asset/resource/debug）保持纯本地。
  const cloudEntries = entries.filter(entry => isCloudMirroredTable(entry.table));
  if (!resolveD1Toggle(options) || !import.meta.client || cloudEntries.length === 0) {
    return;
  }

  try {
    const body = {
      entries: await Promise.all(cloudEntries.map(serializeEntry)),
    };

    await $fetch('/api/web/cache/d1', {
      method: 'POST',
      body,
    });
  } catch (error) {
    if (disableD1OnUnauth(error)) return;
    console.warn('[store/v2] failed to mirror cache writes to D1', error);
  }
}

export async function writeEntryToD1(entry: D1CacheEntry, options?: D1CacheWriteOptions): Promise<void> {
  await writeEntriesToD1([entry], options);
}

// --- delete ---

export async function deleteEntriesFromD1(
  entries: D1CacheDeleteRequestEntry[],
  options?: D1CacheWriteOptions
): Promise<void> {
  const cloudEntries = entries.filter(entry => isCloudMirroredTable(entry.table));
  if (!resolveD1Toggle(options) || !import.meta.client || cloudEntries.length === 0) return;
  try {
    await $fetch('/api/web/cache/d1', { method: 'POST', body: { action: 'delete', entries: cloudEntries } });
  } catch (error) {
    if (disableD1OnUnauth(error)) return;
    console.warn('[store/v2] failed to mirror cache deletes to D1', error);
  }
}

// --- read (D1 fallback) ---

export async function fetchEntryFromD1<T>(table: D1CacheTable, key: string): Promise<T | undefined> {
  if (!isCloudMirroredTable(table)) return undefined;
  if (!resolveD1Toggle(undefined)) return undefined;
  if (!import.meta.client) return undefined;

  const cacheKey = `${table}:${key}`;
  if (isNegativeCached(cacheKey)) return undefined;

  try {
    const resp = await $fetch<{
      ok: boolean;
      found: boolean;
      payload?: Record<string, unknown>;
      blobField?: string | null;
      blobType?: string | null;
      blobBase64?: string | null;
    }>('/api/web/cache/d1', { method: 'POST', body: { action: 'read', table, key } });

    if (!resp.found) {
      setNegativeCached(cacheKey);
      return undefined;
    }

    const record = { ...resp.payload };
    if (resp.blobField && resp.blobBase64) {
      record[resp.blobField] = new Blob([base64ToArrayBuffer(resp.blobBase64)], { type: resp.blobType ?? '' });
    }

    return record as T;
  } catch (error) {
    if (disableD1OnUnauth(error)) return undefined;
    console.warn('[store/v2] D1 fallback read failed', error);
    return undefined;
  }
}

// --- list（账号级对账，不进 negativeCache） ---

export async function fetchListFromD1<T>(
  table: D1CloudTable,
  scopeKey?: string
): Promise<{ items: T[]; truncated: boolean } | undefined> {
  if (!isCloudMirroredTable(table)) return undefined;
  if (!resolveD1Toggle(undefined)) return undefined;
  if (!import.meta.client) return undefined;

  try {
    const resp = await $fetch<D1CacheListResponse>('/api/web/cache/d1', {
      method: 'POST',
      body: { action: 'list', table, scopeKey },
    });

    const records = resp.items.map(item => {
      const record = { ...item.payload };
      if (item.blobField && item.blobBase64) {
        record[item.blobField] = new Blob([base64ToArrayBuffer(item.blobBase64)], { type: item.blobType ?? '' });
      }
      return record as T;
    });

    return { items: records, truncated: resp.truncated };
  } catch (error) {
    if (disableD1OnUnauth(error)) return undefined;
    console.warn('[store/v2] D1 list read failed', error);
    return undefined;
  }
}
