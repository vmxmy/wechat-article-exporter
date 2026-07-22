export const LEGACY_ARCHIVE_FORMAT = 'wechat-article-exporter-legacy-archive' as const;
export const LEGACY_ARCHIVE_SCHEMA_VERSION = 1 as const;
export const LEGACY_ARCHIVE_CHECKSUM_ALGORITHM = 'sha256' as const;
export const LEGACY_ARCHIVE_SOURCE_TABLES = [
  'info',
  'article',
  'html',
  'metadata',
  'comment',
  'comment_reply',
  'resource-map',
  'resource',
  'asset',
] as const;

export type LegacyArchiveSourceTable = (typeof LEGACY_ARCHIVE_SOURCE_TABLES)[number];
export type LegacyArchiveLogicalTable =
  | 'accounts'
  | 'articles'
  | 'html'
  | 'metadata'
  | 'comments'
  | 'replies'
  | 'resourceMaps'
  | 'resources'
  | 'assets';
export type LegacyArchiveStatus = 'complete' | 'partial';

export interface LegacyArchiveContentReference {
  path: string;
  bytes: number;
  sha256: string;
  mediaType: string;
}

export interface LegacyArchiveRecord<T = Record<string, unknown>> {
  key: string;
  value: T;
}

export interface LegacyArchiveBinaryRecordValue extends Record<string, unknown> {
  content: LegacyArchiveContentReference;
}

export interface LegacyArchiveTableManifest {
  sourceTable: LegacyArchiveSourceTable;
  path: string;
  records: number;
}

export interface LegacyArchiveMissingResource {
  articleUrl: string;
  resourceUrl: string;
  reason: 'missing-resource-record' | 'missing-resource-bytes' | 'unreadable-resource-bytes';
}

export interface LegacyArchiveWarning {
  code: 'missing-resource' | 'missing-blob' | 'unreadable-blob';
  table: LegacyArchiveSourceTable;
  key: string;
  message: string;
}

export interface LegacyArchiveManifest {
  format: typeof LEGACY_ARCHIVE_FORMAT;
  schemaVersion: typeof LEGACY_ARCHIVE_SCHEMA_VERSION;
  createdAt: string;
  status: LegacyArchiveStatus;
  source: {
    application: 'wechat-article-exporter-web';
    dexieDatabase: string;
    dexieSchemaVersion: number;
  };
  counts: Record<LegacyArchiveLogicalTable, number>;
  tables: Record<LegacyArchiveLogicalTable, LegacyArchiveTableManifest>;
  missingResources: LegacyArchiveMissingResource[];
  warnings: LegacyArchiveWarning[];
  checksumFile: 'checksums.json';
}

export interface LegacyArchiveChecksumFile {
  algorithm: typeof LEGACY_ARCHIVE_CHECKSUM_ALGORITHM;
  scope: 'all archive files except checksums.json';
  files: Array<{
    path: string;
    bytes: number;
    sha256: string;
  }>;
}

export type LegacyArchiveExportPhase = 'idle' | 'reading' | 'packing' | 'downloading' | 'complete' | 'error';

export interface LegacyArchiveExportProgress {
  phase: LegacyArchiveExportPhase;
  message: string;
  completed: number;
  total: number;
  percent: number;
}

export interface LegacyArchiveExportResult {
  blob: Blob;
  filename: string;
  manifest: LegacyArchiveManifest;
  checksums: LegacyArchiveChecksumFile;
}
