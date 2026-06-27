import type { D1CacheTable, D1CloudTable } from '~/shared/utils/d1-cache';
import { fetchEntryFromD1, fetchListFromD1 } from './d1';

// Local-first read with D1 fallback.
// If IndexedDB has the record, return it immediately.
// On miss, try D1 (respects the d1MirrorEnabled preference).
// On D1 hit, write back to local so subsequent reads are cache-warm.
export async function readWithD1Fallback<T>(
  table: D1CacheTable,
  key: string,
  localGetter: () => Promise<T | undefined>,
  localFiller: (value: T) => Promise<unknown>
): Promise<T | undefined> {
  const local = await localGetter();
  if (local !== undefined) return local;

  const remote = await fetchEntryFromD1<T>(table, key);
  if (remote !== undefined) {
    // fire-and-forget: warm the local cache; failures are silent
    localFiller(remote).catch(() => {});
  }

  return remote;
}

// 账号级对账：拉 D1 list → 逐条 upsert 进 Dexie。
// 门控由 fetchListFromD1 承担（开关关时返回 undefined → 静默跳过，行为=现状）。
// 失败由 fetchListFromD1 内部吞掉，本助手不抛；truncated 仅作可选提示，不阻断。
export async function reconcileListFromD1<T>(
  table: D1CloudTable,
  scopeKey: string | undefined,
  upsertLocal: (items: T[]) => Promise<unknown>
): Promise<void> {
  const remote = await fetchListFromD1<T>(table, scopeKey);
  if (!remote || remote.items.length === 0) return;

  if (remote.truncated) {
    console.warn(`[store/v2] D1 list for "${table}" truncated at limit; local remains authoritative`);
  }

  try {
    await upsertLocal(remote.items);
  } catch (error) {
    // 对账写本地失败（配额/约束等）不应冒泡卡住调用方的加载流程，本地仍权威。
    console.warn(`[store/v2] D1 list reconcile upsert for "${table}" failed`, error);
  }
}
