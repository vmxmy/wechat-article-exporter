import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import JSZip from 'jszip';
import {
  downloadLegacyArchive,
  type LegacyArchiveDataSource,
  type LegacySourceRecord,
} from '~/composables/useLegacyArchiveExport';

function sha256(bytes: Uint8Array): string {
  return createHash('sha256').update(bytes).digest('hex');
}

function createSource(): LegacyArchiveDataSource {
  const htmlBytes = new Uint8Array([60, 104, 49, 62, 0, 255, 60, 47, 104, 49, 62]);
  const resourceBytes = new Uint8Array([0, 1, 2, 127, 128, 254, 255]);
  const assetBytes = new Uint8Array([64, 99, 104, 97, 114, 115, 101, 116]);
  const tables: Record<string, LegacySourceRecord[]> = {
    info: [
      {
        key: 'account-1',
        value: { fakeid: 'account-1', nickname: 'Archive Fixture', completed: true, count: 1, articles: 1 },
      },
    ],
    article: [
      {
        key: 'account-1:article-1',
        value: {
          fakeid: 'account-1',
          aid: 'article-1',
          link: 'https://mp.weixin.qq.com/s/article-1',
          title: 'Binary-safe archive',
          create_time: 1710000000,
        },
      },
    ],
    html: [
      {
        key: 'https://mp.weixin.qq.com/s/article-1',
        value: {
          fakeid: 'account-1',
          url: 'https://mp.weixin.qq.com/s/article-1',
          title: 'Binary-safe archive',
          commentID: 'comment-1',
          file: new Blob([htmlBytes], { type: 'text/html;charset=utf-8' }),
        },
      },
    ],
    metadata: [
      {
        key: 'https://mp.weixin.qq.com/s/article-1',
        value: {
          fakeid: 'account-1',
          url: 'https://mp.weixin.qq.com/s/article-1',
          title: 'Binary-safe archive',
          readNum: 12,
          oldLikeNum: 2,
          shareNum: 3,
          likeNum: 4,
          commentNum: 1,
        },
      },
    ],
    comment: [
      {
        key: 'https://mp.weixin.qq.com/s/article-1',
        value: {
          fakeid: 'account-1',
          url: 'https://mp.weixin.qq.com/s/article-1',
          title: 'Binary-safe archive',
          data: { elected_comment: [{ content_id: 'comment-1', content: 'hello' }] },
        },
      },
    ],
    comment_reply: [
      {
        key: 'https://mp.weixin.qq.com/s/article-1:comment-1',
        value: {
          fakeid: 'account-1',
          url: 'https://mp.weixin.qq.com/s/article-1',
          title: 'Binary-safe archive',
          contentID: 'comment-1',
          data: { reply_list: [{ content: 'world' }] },
        },
      },
    ],
    'resource-map': [
      {
        key: 'https://mp.weixin.qq.com/s/article-1',
        value: {
          fakeid: 'account-1',
          url: 'https://mp.weixin.qq.com/s/article-1',
          resources: ['https://cdn.example/resource.bin', 'https://cdn.example/missing.png'],
        },
      },
    ],
    resource: [
      {
        key: 'https://cdn.example/resource.bin',
        value: {
          fakeid: 'account-1',
          url: 'https://cdn.example/resource.bin',
          file: new Blob([resourceBytes], { type: 'application/octet-stream' }),
        },
      },
    ],
    asset: [
      {
        key: 'https://cdn.example/style.css',
        value: {
          fakeid: 'account-1',
          url: 'https://cdn.example/style.css',
          file: new Blob([assetBytes], { type: 'text/css' }),
        },
      },
    ],
  };

  return {
    databaseName: 'exporter.wxdown.online',
    schemaVersion: 3,
    async readSnapshot(tableNames) {
      const snapshot = {} as Record<(typeof tableNames)[number], LegacySourceRecord[]>;
      for (const tableName of tableNames) {
        snapshot[tableName] = tables[tableName] ?? [];
      }
      return snapshot;
    },
  };
}

async function readBytes(zip: JSZip, path: string): Promise<Uint8Array> {
  const entry = zip.file(path);
  assert.ok(entry, `missing ZIP entry: ${path}`);
  return entry.async('uint8array');
}

async function readJson<T>(zip: JSZip, path: string): Promise<T> {
  const bytes = await readBytes(zip, path);
  return JSON.parse(new TextDecoder().decode(bytes)) as T;
}

async function run() {
  let downloadCount = 0;
  let downloadedFilename = '';
  let downloadedBlob: Blob | undefined;
  let fetchCount = 0;
  const previousFetch = globalThis.fetch;

  globalThis.fetch = (async () => {
    fetchCount++;
    throw new Error('legacy archive export must not use fetch');
  }) as typeof fetch;

  try {
    const result = await downloadLegacyArchive({
      source: createSource(),
      now: () => new Date('2026-07-22T08:09:10.000Z'),
      download(blob, filename) {
        downloadCount++;
        downloadedBlob = blob;
        downloadedFilename = filename;
      },
    });

    assert.equal(fetchCount, 0, 'export performed a network fetch');
    assert.equal(downloadCount, 1);
    assert.equal(downloadedBlob, result.blob);
    assert.equal(downloadedFilename, 'wechat-legacy-archive-20260722-080910.zip');
    assert.equal(result.blob.type, 'application/zip');

    const zip = await JSZip.loadAsync(await result.blob.arrayBuffer());
    const manifest = await readJson<any>(zip, 'manifest.json');
    const checksums = await readJson<any>(zip, 'checksums.json');

    assert.equal(manifest.format, 'wechat-article-exporter-legacy-archive');
    assert.equal(manifest.schemaVersion, 1);
    assert.equal(manifest.source.dexieSchemaVersion, 3);
    assert.equal(manifest.status, 'partial');
    assert.equal(manifest.counts.accounts, 1);
    assert.equal(manifest.counts.articles, 1);
    assert.equal(manifest.counts.html, 1);
    assert.equal(manifest.counts.metadata, 1);
    assert.equal(manifest.counts.comments, 1);
    assert.equal(manifest.counts.replies, 1);
    assert.equal(manifest.counts.resourceMaps, 1);
    assert.equal(manifest.counts.resources, 1);
    assert.equal(manifest.counts.assets, 1);
    assert.equal(manifest.missingResources.length, 1);
    assert.equal(manifest.missingResources[0].resourceUrl, 'https://cdn.example/missing.png');
    assert.ok(manifest.tables.html.path.startsWith('records/'));

    assert.equal(checksums.algorithm, 'sha256');
    assert.equal(checksums.scope, 'all archive files except checksums.json');
    assert.ok(checksums.files.some((file: any) => file.path === 'manifest.json'));
    assert.ok(!checksums.files.some((file: any) => file.path === 'checksums.json'));

    for (const expected of checksums.files) {
      const bytes = await readBytes(zip, expected.path);
      assert.equal(bytes.byteLength, expected.bytes, `size mismatch for ${expected.path}`);
      assert.equal(sha256(bytes), expected.sha256, `SHA-256 mismatch for ${expected.path}`);
    }

    const htmlRecords = await readJson<any[]>(zip, manifest.tables.html.path);
    const resourceRecords = await readJson<any[]>(zip, manifest.tables.resources.path);
    const assetRecords = await readJson<any[]>(zip, manifest.tables.assets.path);

    for (const binaryRecord of [htmlRecords[0], resourceRecords[0], assetRecords[0]]) {
      assert.match(binaryRecord.value.content.path, /^objects\/(html|resources|assets)\/sha256\/[0-9a-f]{2}\/[0-9a-f]{2}\/[0-9a-f]{64}$/);
      assert.ok(!Object.hasOwn(binaryRecord.value, 'file'));
    }

    assert.deepEqual(
      [...(await readBytes(zip, htmlRecords[0].value.content.path))],
      [60, 104, 49, 62, 0, 255, 60, 47, 104, 49, 62]
    );
    assert.deepEqual(
      [...(await readBytes(zip, resourceRecords[0].value.content.path))],
      [0, 1, 2, 127, 128, 254, 255]
    );
    assert.deepEqual(
      [...(await readBytes(zip, assetRecords[0].value.content.path))],
      [64, 99, 104, 97, 114, 115, 101, 116]
    );

    console.log('legacy archive export regression checks passed');
  } finally {
    globalThis.fetch = previousFetch;
  }
}

run();
