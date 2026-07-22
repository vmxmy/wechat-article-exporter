import type { IndexableType } from 'dexie';
import { saveAs } from 'file-saver';
import type JSZip from 'jszip';
import { readonly, ref, shallowRef } from 'vue';
import {
  LEGACY_ARCHIVE_CHECKSUM_ALGORITHM,
  LEGACY_ARCHIVE_FORMAT,
  LEGACY_ARCHIVE_SCHEMA_VERSION,
  LEGACY_ARCHIVE_SOURCE_TABLES,
  type LegacyArchiveBinaryRecordValue,
  type LegacyArchiveChecksumFile,
  type LegacyArchiveExportProgress,
  type LegacyArchiveExportResult,
  type LegacyArchiveLogicalTable,
  type LegacyArchiveManifest,
  type LegacyArchiveMissingResource,
  type LegacyArchiveRecord,
  type LegacyArchiveSourceTable,
  type LegacyArchiveTableManifest,
  type LegacyArchiveWarning,
} from '../shared/migration/legacy-archive';

export interface LegacySourceRecord {
  key: string;
  value: Record<string, unknown>;
}

export type LegacySourceSnapshot = Record<LegacyArchiveSourceTable, LegacySourceRecord[]>;

export interface LegacyArchiveDataSource {
  databaseName: string;
  schemaVersion: number;
  readSnapshot(tableNames: readonly LegacyArchiveSourceTable[]): Promise<LegacySourceSnapshot>;
}

export interface LegacyArchiveExportOptions {
  source?: LegacyArchiveDataSource;
  now?: () => Date;
  download?: (blob: Blob, filename: string) => void;
  onProgress?: (progress: LegacyArchiveExportProgress) => void;
}

interface ArchiveFile {
  path: string;
  bytes: Uint8Array;
}

interface BlobLike {
  arrayBuffer(): Promise<ArrayBuffer>;
  size: number;
  type: string;
}

const encoder = new TextEncoder();

const TABLE_LAYOUT: Record<
  LegacyArchiveLogicalTable,
  { sourceTable: LegacyArchiveSourceTable; path: string; binaryDirectory?: string }
> = {
  accounts: { sourceTable: 'info', path: 'records/accounts.json' },
  articles: { sourceTable: 'article', path: 'records/articles.json' },
  html: { sourceTable: 'html', path: 'records/html.json', binaryDirectory: 'objects/html' },
  metadata: { sourceTable: 'metadata', path: 'records/metadata.json' },
  comments: { sourceTable: 'comment', path: 'records/comments.json' },
  replies: { sourceTable: 'comment_reply', path: 'records/replies.json' },
  resourceMaps: { sourceTable: 'resource-map', path: 'records/resource-maps.json' },
  resources: { sourceTable: 'resource', path: 'records/resources.json', binaryDirectory: 'objects/resources' },
  assets: { sourceTable: 'asset', path: 'records/assets.json', binaryDirectory: 'objects/assets' },
};

export function useLegacyArchiveExport() {
  const exporting = ref(false);
  const progress = ref<LegacyArchiveExportProgress>(createProgress('idle', '等待导出', 0, 1));
  const result = shallowRef<LegacyArchiveExportResult>();
  const error = shallowRef<Error>();

  async function exportArchive() {
    exporting.value = true;
    result.value = undefined;
    error.value = undefined;

    try {
      const exported = await downloadLegacyArchive({
        onProgress(nextProgress) {
          progress.value = nextProgress;
        },
      });
      result.value = exported;
      return exported;
    } catch (caught) {
      const normalized = caught instanceof Error ? caught : new Error(String(caught));
      error.value = normalized;
      progress.value = createProgress('error', normalized.message, 0, 1);
      throw normalized;
    } finally {
      exporting.value = false;
    }
  }

  return {
    exporting: readonly(exporting),
    progress: readonly(progress),
    result: readonly(result),
    error: readonly(error),
    exportArchive,
  };
}

export async function downloadLegacyArchive(options: LegacyArchiveExportOptions = {}): Promise<LegacyArchiveExportResult> {
  const source = options.source ?? (await createDexieLegacyArchiveSource());
  const now = options.now ?? (() => new Date());
  const download = options.download ?? ((blob, filename) => saveAs(blob, filename));
  const emitProgress = options.onProgress ?? (() => {});
  const exportedAt = now();

  emitProgress(createProgress('reading', '正在读取浏览器本地数据库', 0, LEGACY_ARCHIVE_SOURCE_TABLES.length));
  const snapshot = await source.readSnapshot(LEGACY_ARCHIVE_SOURCE_TABLES);
  emitProgress(
    createProgress(
      'reading',
      '本地数据库读取完成',
      LEGACY_ARCHIVE_SOURCE_TABLES.length,
      LEGACY_ARCHIVE_SOURCE_TABLES.length
    )
  );

  const files: ArchiveFile[] = [];
  const warnings: LegacyArchiveWarning[] = [];
  const records = {} as Record<LegacyArchiveLogicalTable, LegacyArchiveRecord[]>;
  const logicalTables = Object.keys(TABLE_LAYOUT) as LegacyArchiveLogicalTable[];

  for (let index = 0; index < logicalTables.length; index++) {
    const logicalTable = logicalTables[index];
    const layout = TABLE_LAYOUT[logicalTable];
    const sourceRecords = snapshot[layout.sourceTable] ?? [];
    const exportedRecords: LegacyArchiveRecord[] = [];

    for (let recordIndex = 0; recordIndex < sourceRecords.length; recordIndex++) {
      const sourceRecord = sourceRecords[recordIndex];
      const exportedValue = { ...sourceRecord.value };

      if (layout.binaryDirectory) {
        const blob = asBlobLike(exportedValue.file);
        delete exportedValue.file;

        if (blob) {
          try {
            const bytes = new Uint8Array(await blob.arrayBuffer());
            const digest = await sha256Hex(bytes);
            const objectPath = `${layout.binaryDirectory}/sha256/${digest.slice(0, 2)}/${digest.slice(2, 4)}/${digest}`;
            if (!files.some(file => file.path === objectPath)) files.push({ path: objectPath, bytes });
            (exportedValue as LegacyArchiveBinaryRecordValue).content = {
              path: objectPath,
              bytes: bytes.byteLength,
              sha256: digest,
              mediaType: blob.type || 'application/octet-stream',
            };
          } catch (caught) {
            warnings.push({
              code: 'unreadable-blob',
              table: layout.sourceTable,
              key: sourceRecord.key,
              message: `${layout.sourceTable} 记录的 Blob 字节读取失败：${errorMessage(caught)}`,
            });
          }
        } else {
          warnings.push({
            code: 'missing-blob',
            table: layout.sourceTable,
            key: sourceRecord.key,
            message: `${layout.sourceTable} 记录缺少可导出的 Blob 字节`,
          });
        }
      }

      exportedRecords.push({ key: sourceRecord.key, value: exportedValue });
    }

    records[logicalTable] = exportedRecords;
    emitProgress(createProgress('packing', `正在整理 ${logicalTable}`, index + 1, logicalTables.length + 2));
  }

  const missingResources = findMissingResources(records.resourceMaps, records.resources, warnings);

  for (const logicalTable of logicalTables) {
    const tableBytes = jsonBytes(records[logicalTable]);
    files.push({ path: TABLE_LAYOUT[logicalTable].path, bytes: tableBytes });
  }

  const tables = {} as Record<LegacyArchiveLogicalTable, LegacyArchiveTableManifest>;
  const counts = {} as Record<LegacyArchiveLogicalTable, number>;
  for (const logicalTable of logicalTables) {
    const layout = TABLE_LAYOUT[logicalTable];
    const recordCount = records[logicalTable].length;
    counts[logicalTable] = recordCount;
    tables[logicalTable] = {
      sourceTable: layout.sourceTable,
      path: layout.path,
      records: recordCount,
    };
  }

  const manifest: LegacyArchiveManifest = {
    format: LEGACY_ARCHIVE_FORMAT,
    schemaVersion: LEGACY_ARCHIVE_SCHEMA_VERSION,
    createdAt: exportedAt.toISOString(),
    status: warnings.length === 0 && missingResources.length === 0 ? 'complete' : 'partial',
    source: {
      application: 'wechat-article-exporter-web',
      dexieDatabase: source.databaseName,
      dexieSchemaVersion: source.schemaVersion,
    },
    counts,
    tables,
    missingResources,
    warnings,
    checksumFile: 'checksums.json',
  };
  files.push({ path: 'manifest.json', bytes: jsonBytes(manifest) });

  const checksums: LegacyArchiveChecksumFile = {
    algorithm: LEGACY_ARCHIVE_CHECKSUM_ALGORITHM,
    scope: 'all archive files except checksums.json',
    files: [],
  };
  for (const file of [...files].sort((left, right) => left.path.localeCompare(right.path))) {
    checksums.files.push({ path: file.path, bytes: file.bytes.byteLength, sha256: await sha256Hex(file.bytes) });
  }
  files.push({ path: 'checksums.json', bytes: jsonBytes(checksums) });

  emitProgress(createProgress('packing', '正在生成 ZIP 文件', logicalTables.length + 1, logicalTables.length + 2));
  const blob = await createZipBlob(files, emitProgress, logicalTables.length + 2);
  const filename = createArchiveFilename(exportedAt);

  emitProgress(createProgress('downloading', '正在打开浏览器下载', 1, 1));
  download(blob, filename);
  emitProgress(createProgress('complete', manifest.status === 'complete' ? '导出完成' : '导出完成（部分数据缺失）', 1, 1));

  return { blob, filename, manifest, checksums };
}

async function createDexieLegacyArchiveSource(): Promise<LegacyArchiveDataSource> {
  if (typeof indexedDB === 'undefined') {
    throw new Error('旧数据导出只能在支持 IndexedDB 的浏览器中运行');
  }

  const { db } = await import('~/store/v2/db');
  await db.open();

  return {
    databaseName: db.name,
    schemaVersion: db.verno,
    async readSnapshot(tableNames) {
      const existingTableNames = new Set(db.tables.map(table => table.name));
      const availableTableNames = tableNames.filter(tableName => existingTableNames.has(tableName));
      const snapshot = emptySnapshot();

      if (availableTableNames.length === 0) return snapshot;

      return db.transaction('r', availableTableNames, async transaction => {
        for (const tableName of availableTableNames) {
          const table = transaction.table<Record<string, unknown>, IndexableType>(tableName);
          await table.each((value, cursor) => {
            snapshot[tableName].push({ key: serializeDexieKey(cursor.primaryKey), value });
          });
        }
        return snapshot;
      });
    },
  };
}

async function createZipBlob(
  files: ArchiveFile[],
  emitProgress: (progress: LegacyArchiveExportProgress) => void,
  progressTotal: number
): Promise<Blob> {
  const { default: JSZipConstructor } = await import('jszip');
  const zip: JSZip = new JSZipConstructor();
  for (const file of files) zip.file(file.path, file.bytes);

  const bytes = await zip.generateAsync(
    { type: 'uint8array', compression: 'DEFLATE', compressionOptions: { level: 6 }, platform: 'UNIX' },
    metadata => {
      emitProgress({
        phase: 'packing',
        message: metadata.currentFile ? `正在压缩 ${metadata.currentFile}` : '正在生成 ZIP 文件',
        completed: progressTotal - 1,
        total: progressTotal,
        percent: Math.min(99, Math.round(metadata.percent)),
      });
    }
  );
  return new Blob([toArrayBuffer(bytes)], { type: 'application/zip' });
}

function findMissingResources(
  resourceMaps: LegacyArchiveRecord[],
  resources: LegacyArchiveRecord[],
  warnings: LegacyArchiveWarning[]
): LegacyArchiveMissingResource[] {
  const resourcesByUrl = new Map<string, LegacyArchiveRecord>();
  for (const record of resources) {
    const url = stringValue(record.value.url) || record.key;
    resourcesByUrl.set(url, record);
  }

  const missing: LegacyArchiveMissingResource[] = [];
  for (const resourceMap of resourceMaps) {
    const articleUrl = stringValue(resourceMap.value.url) || resourceMap.key;
    const resourceUrls = Array.isArray(resourceMap.value.resources) ? resourceMap.value.resources : [];
    for (const candidate of resourceUrls) {
      if (typeof candidate !== 'string') continue;
      const resource = resourcesByUrl.get(candidate);
      const reason = !resource
        ? 'missing-resource-record'
        : isContentReference(resource.value.content)
          ? undefined
          : warnings.some(warning => warning.code === 'unreadable-blob' && warning.table === 'resource' && warning.key === resource.key)
            ? 'unreadable-resource-bytes'
            : 'missing-resource-bytes'
      if (!reason) continue;

      missing.push({ articleUrl, resourceUrl: candidate, reason });
      warnings.push({
        code: 'missing-resource',
        table: 'resource-map',
        key: resourceMap.key,
        message: `文章 ${articleUrl} 引用的资源 ${candidate} 缺失`,
      });
    }
  }
  return missing;
}

function emptySnapshot(): LegacySourceSnapshot {
  const snapshot = {} as LegacySourceSnapshot;
  for (const tableName of LEGACY_ARCHIVE_SOURCE_TABLES) snapshot[tableName] = [];
  return snapshot;
}

function serializeDexieKey(key: IndexableType): string {
  if (key instanceof Date) return key.toISOString();
  if (Array.isArray(key)) return JSON.stringify(key);
  return String(key);
}

function jsonBytes(value: unknown): Uint8Array {
  return encoder.encode(`${JSON.stringify(value, null, 2)}\n`);
}

async function sha256Hex(bytes: Uint8Array): Promise<string> {
  if (!globalThis.crypto?.subtle) throw new Error('当前浏览器不支持 SHA-256 校验');
  const digest = await globalThis.crypto.subtle.digest('SHA-256', toArrayBuffer(bytes));
  return [...new Uint8Array(digest)].map(byte => byte.toString(16).padStart(2, '0')).join('');
}

function asBlobLike(value: unknown): BlobLike | undefined {
  if (!value || typeof value !== 'object') return undefined;
  const candidate = value as Partial<BlobLike>;
  if (typeof candidate.arrayBuffer !== 'function' || typeof candidate.size !== 'number') return undefined;
  return candidate as BlobLike;
}

function isContentReference(value: unknown): value is LegacyArchiveBinaryRecordValue['content'] {
  if (!value || typeof value !== 'object') return false;
  const candidate = value as Partial<LegacyArchiveBinaryRecordValue['content']>;
  return typeof candidate.path === 'string' && typeof candidate.sha256 === 'string' && typeof candidate.bytes === 'number';
}

function stringValue(value: unknown): string | undefined {
  return typeof value === 'string' ? value : undefined;
}

function createArchiveFilename(date: Date): string {
  const compact = date.toISOString().replace(/[-:]/g, '').replace('T', '-').slice(0, 15);
  return `wechat-legacy-archive-${compact}.zip`;
}

function errorMessage(caught: unknown): string {
  return caught instanceof Error ? caught.message : String(caught);
}

function toArrayBuffer(bytes: Uint8Array): ArrayBuffer {
  const copy = new Uint8Array(bytes.byteLength);
  copy.set(bytes);
  return copy.buffer;
}

function createProgress(
  phase: LegacyArchiveExportProgress['phase'],
  message: string,
  completed: number,
  total: number
): LegacyArchiveExportProgress {
  return {
    phase,
    message,
    completed,
    total,
    percent: total > 0 ? Math.round((completed / total) * 100) : 0,
  };
}
