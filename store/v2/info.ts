import type { D1CacheWriteOptions } from '~/shared/utils/d1-cache';
import { writeEntryToD1 } from './d1';
import { db } from './db';
import { reconcileListFromD1 } from './read';

export interface MpAccount {
  fakeid: string;
  completed: boolean;
  count: number;
  articles: number;

  // 公众号昵称
  nickname?: string;
  // 公众号头像
  round_head_img?: string;

  // 公众号文章总数
  total_count: number;
  create_time?: number;
  update_time?: number;

  // 最后更新时间
  last_update_time?: number;
}

/**
 * 更新 account 缓存
 * @param mpAccount
 */
export async function updateInfoCache(mpAccount: MpAccount, options?: D1CacheWriteOptions): Promise<boolean> {
  let persistedInfo: MpAccount | undefined;

  const result = await db.transaction('rw', 'info', async () => {
    let currentInfo = await db.info.get(mpAccount.fakeid);
    if (currentInfo) {
      if (mpAccount.completed) {
        currentInfo.completed = mpAccount.completed;
      }
      currentInfo.count += mpAccount.count;
      currentInfo.articles += mpAccount.articles;
      currentInfo.nickname = mpAccount.nickname;
      currentInfo.round_head_img = mpAccount.round_head_img;
      currentInfo.total_count = mpAccount.total_count;
      currentInfo.update_time = Math.round(Date.now() / 1000);
    } else {
      currentInfo = {
        fakeid: mpAccount.fakeid,
        completed: mpAccount.completed,
        count: mpAccount.count,
        articles: mpAccount.articles,
        nickname: mpAccount.nickname,
        round_head_img: mpAccount.round_head_img,
        total_count: mpAccount.total_count,
        create_time: Math.round(Date.now() / 1000),
        update_time: Math.round(Date.now() / 1000),
      };
    }

    await db.info.put(currentInfo);
    persistedInfo = currentInfo;
    return true;
  });

  if (persistedInfo) {
    await writeEntryToD1(
      {
        table: 'info',
        key: persistedInfo.fakeid,
        record: persistedInfo as unknown as Record<string, unknown>,
      },
      options
    );
  }

  return result;
}

export async function updateLastUpdateTime(fakeid: string, options?: D1CacheWriteOptions): Promise<boolean> {
  let persistedInfo: MpAccount | undefined;

  const result = await db.transaction('rw', 'info', async () => {
    let currentInfo = await db.info.get(fakeid);
    if (currentInfo) {
      currentInfo.last_update_time = Math.round(Date.now() / 1000);
      await db.info.put(currentInfo);
    }

    persistedInfo = currentInfo;
    return true;
  });

  if (persistedInfo) {
    await writeEntryToD1(
      {
        table: 'info',
        key: persistedInfo.fakeid,
        record: persistedInfo as unknown as Record<string, unknown>,
      },
      options
    );
  }

  return result;
}

/**
 * 获取 info 缓存
 * @param fakeid
 */
export async function getInfoCache(fakeid: string): Promise<MpAccount | undefined> {
  return db.info.get(fakeid);
}

export async function getAllInfo(): Promise<MpAccount[]> {
  return db.info.toArray();
}

/**
 * 账号列表对账：拉本 owner 在 D1 的全部 info 行 → upsert 进 Dexie。
 * info 表 scope_key=自身 fakeid，但对账整张账号列表时不传 scopeKey（拉全部 info 行）。
 * info 主键为 inbound `fakeid`，bulkPut 无需显式传 keys。
 * 门控由 reconcileListFromD1 → fetchListFromD1 承担（开关关时静默跳过，行为=现状）。
 * 在 getAllInfo 前由 refresh() 调用。
 */
export async function reconcileAllInfoFromD1(): Promise<void> {
  await reconcileListFromD1<MpAccount>('info', undefined, async items => {
    // 增量对账：仅补本地缺失的账号，绝不覆盖既有本地行（count/articles 为本地累加值，覆盖会回退进度）。
    const existing = new Set(await db.info.toCollection().primaryKeys());
    const fresh = items.filter(account => !existing.has(account.fakeid));
    if (fresh.length === 0) return;
    await db.info.bulkPut(fresh);
  });
}

// 获取公众号的名称
export async function getAccountNameByFakeid(fakeid: string): Promise<string | null> {
  const account = await getInfoCache(fakeid);
  if (!account) {
    return null;
  }

  return account.nickname || null;
}

// 批量导入公众号
export async function importMpAccounts(mpAccounts: MpAccount[], options?: D1CacheWriteOptions): Promise<void> {
  for (const mpAccount of mpAccounts) {
    // 导入时需要把相关数量置空
    mpAccount.completed = false;
    mpAccount.count = 0;
    mpAccount.articles = 0;
    mpAccount.total_count = 0;
    mpAccount.create_time = undefined;
    mpAccount.update_time = undefined;
    mpAccount.last_update_time = undefined;
    await updateInfoCache(mpAccount, options);
  }
}
