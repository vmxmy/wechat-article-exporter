export const D1_CACHE_TABLES = [
  'article',
  'asset',
  'comment',
  'comment_reply',
  'debug',
  'html',
  'info',
  'metadata',
  'resource',
  'resource-map',
] as const;

export type D1CacheTable = (typeof D1_CACHE_TABLES)[number];

// 上云镜像的表：仅结构化元数据小表。
// 含 Blob 的表（html/asset/resource/debug）保持纯本地，避免 D1 单值 2MB / 单库 10GB 限额。
export const D1_CLOUD_TABLES = ['article', 'info', 'metadata', 'comment', 'comment_reply', 'resource-map'] as const;

export type D1CloudTable = (typeof D1_CLOUD_TABLES)[number];

export function isCloudMirroredTable(table: string): table is D1CloudTable {
  return (D1_CLOUD_TABLES as readonly string[]).includes(table);
}

export interface D1CacheWriteOptions {
  writeToD1?: boolean;
}

// 账号级 list 对账与服务端 scope 过滤共享的上限语义（避免 magic number 两处漂移）。
export const D1_LIST_LIMIT = 5000;

export interface D1CacheWriteRequestEntry {
  table: D1CacheTable;
  key: string;
  payload: Record<string, unknown>;
  blobField?: string | null;
  blobType?: string | null;
  blobBase64?: string | null;
  // 该条目所属账号 fakeid，用于服务端 scope 过滤（不进主键）。
  scopeKey?: string | null;
}

export interface D1CacheDeleteRequestEntry {
  table: D1CacheTable;
  key: string;
}

export interface D1CacheListRequestEntry {
  table: D1CloudTable;
  scopeKey?: string;
}

export interface D1CacheListItem {
  key: string;
  payload: Record<string, unknown>;
  blobField?: string | null;
  blobType?: string | null;
  blobBase64?: string | null;
}

export interface D1CacheListResponse {
  ok: boolean;
  items: D1CacheListItem[];
  truncated: boolean;
}
