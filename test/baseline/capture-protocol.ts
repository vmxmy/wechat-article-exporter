import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

type Json = null | boolean | number | string | Json[] | { [key: string]: Json };

interface BaselineCase {
  domain: string;
  name: string;
  operation: string;
  classification: string;
  request: Json;
  response: Json;
  expected: Json;
  notes?: string[];
}

const scriptDirectory = path.dirname(fileURLToPath(import.meta.url));
const repositoryRoot = path.resolve(scriptDirectory, '../..');
const fixtureRoot = path.join(repositoryRoot, 'test/fixtures/protocol');
const coveragePath = path.join(fixtureRoot, 'coverage.json');
const verifyMode = process.argv.includes('--verify');
const capturedAt = '2026-07-22T00:00:00.000Z';

function article(overrides: Record<string, Json> = {}): Record<string, Json> {
  return {
    aid: 'fixture-aid-001',
    appmsgid: 10001,
    itemidx: 1,
    fakeid: 'fixture-account-001',
    title: '离线基线文章',
    author_name: '示例作者',
    digest: '用于本地回归的脱敏摘要',
    link: 'https://mp.weixin.qq.com/s/fixture-article-001',
    cover: 'https://mmbiz.qpic.cn/mmbiz_jpg/fixture/640',
    create_time: 1767225600,
    update_time: 1767225600,
    item_show_type: 0,
    copyright_stat: 1,
    copyright_type: 1,
    is_pay_subscribe: 0,
    appmsg_album_infos: [{ album_id: 'fixture-album-001', title: '示例合集' }],
    ...overrides,
  };
}

function publishPage(items: Record<string, Json>[]): string {
  return JSON.stringify({ publish_list: [{ publish_info: JSON.stringify({ appmsgex: items }) }] });
}

const representative = article();
const exportBody = {
  title: '离线基线文章',
  account: '示例公众号',
  author: '示例作者',
  publishedAt: '2026-01-01T00:00:00.000Z',
  html:
    '<h1>一级标题</h1><p>第一段，包含 <a href="https://example.com/reference">链接</a>。</p><ul><li>列表项</li></ul><blockquote>引用</blockquote><pre><code>fmt.Println("fixture")</code></pre><table><tbody><tr><td>单元格</td></tr></tbody></table><p><img src="./assets/fixture-image.jpg" alt="示例图"></p>',
  text: '离线基线文章\n\n一级标题\n第一段，包含 链接。\n列表项\n引用\nfmt.Println("fixture")\n单元格\n[图片: 示例图]',
  markdown:
    '# 离线基线文章\n\n# 一级标题\n\n第一段，包含 [链接](https://example.com/reference)。\n\n- 列表项\n\n> 引用\n\n```\nfmt.Println("fixture")\n```\n\n| 单元格 |\n| --- |\n| 单元格 |\n\n![示例图](./assets/fixture-image.jpg)\n',
  comments: [
    {
      id: 'fixture-comment-001',
      author: '读者甲',
      content: '很有帮助',
      replies: [{ id: 'fixture-reply-001', author: '示例作者', content: '谢谢阅读' }],
    },
  ],
};

const cases: BaselineCase[] = [
  {
    domain: 'searchbiz',
    name: 'success-page',
    operation: 'GET /cgi-bin/searchbiz',
    classification: 'success',
    request: { action: 'search_biz', begin: 0, count: 2, query: '示例', token: '<redacted>', lang: 'zh_CN', f: 'json', ajax: '1' },
    response: {
      base_resp: { ret: 0, err_msg: 'ok' },
      total: 3,
      list: [
        { fakeid: 'fixture-account-001', nickname: '示例公众号', alias: 'fixture_one', round_head_img: 'https://mmbiz.qpic.cn/mmbiz_png/fixture/0' },
        { fakeid: 'fixture-account-002', nickname: '示例周刊', alias: 'fixture_two', round_head_img: 'https://mmbiz.qpic.cn/mmbiz_png/fixture/1' },
      ],
    },
    expected: {
      items: [
        { fakeid: 'fixture-account-001', name: '示例公众号', alias: 'fixture_one', avatarUrl: 'https://mmbiz.qpic.cn/mmbiz_png/fixture/0' },
        { fakeid: 'fixture-account-002', name: '示例周刊', alias: 'fixture_two', avatarUrl: 'https://mmbiz.qpic.cn/mmbiz_png/fixture/1' },
      ],
      total: 3,
      offset: 0,
      limit: 2,
      hasMore: true,
    },
  },
  {
    domain: 'searchbiz',
    name: 'empty',
    operation: 'GET /cgi-bin/searchbiz',
    classification: 'success',
    request: { action: 'search_biz', begin: 0, count: 5, query: '不存在的基线账号', token: '<redacted>' },
    response: { base_resp: { ret: 0, err_msg: 'ok' }, total: 0, list: [] },
    expected: { items: [], total: 0, offset: 0, limit: 5, hasMore: false },
  },
  {
    domain: 'searchbiz',
    name: 'auth-expired',
    operation: 'GET /cgi-bin/searchbiz',
    classification: 'authentication_expired',
    request: { action: 'search_biz', begin: 0, count: 5, query: '示例', token: '<redacted>' },
    response: { base_resp: { ret: 200003, err_msg: 'session expired' } },
    expected: { errorCode: 'wechat_session_expired', retryable: false, action: 'login' },
  },
  {
    domain: 'searchbiz',
    name: 'malformed',
    operation: 'GET /cgi-bin/searchbiz',
    classification: 'protocol_error',
    request: { action: 'search_biz', begin: 0, count: 5, query: '示例', token: '<redacted>' },
    response: { base_resp: { ret: 0, err_msg: 'ok' }, list: 'unexpected-string' },
    expected: { errorCode: 'wechat_protocol_incompatible', partialWrites: 0 },
  },
  {
    domain: 'appmsgpublish',
    name: 'multi-page',
    operation: 'GET /cgi-bin/appmsgpublish',
    classification: 'success',
    request: { sub: 'list', begin: 0, count: 2, fakeid: 'fixture-account-001', token: '<redacted>', type: '101_1' },
    response: { base_resp: { ret: 0 }, total_count: 3, publish_page: publishPage([representative, article({ aid: 'fixture-aid-002', title: '第二篇基线文章', link: 'https://mp.weixin.qq.com/s/fixture-article-002' })]) },
    expected: { items: [representative, article({ aid: 'fixture-aid-002', title: '第二篇基线文章', link: 'https://mp.weixin.qq.com/s/fixture-article-002' })], total: 3, nextBegin: 2, complete: false },
  },
  {
    domain: 'appmsgpublish',
    name: 'empty',
    operation: 'GET /cgi-bin/appmsgpublish',
    classification: 'success',
    request: { sub: 'list', begin: 0, count: 5, fakeid: 'fixture-account-empty', token: '<redacted>' },
    response: { base_resp: { ret: 0 }, total_count: 0, publish_page: JSON.stringify({ publish_list: [] }) },
    expected: { items: [], total: 0, nextBegin: null, complete: true },
  },
  {
    domain: 'appmsgpublish',
    name: 'auth-expired',
    operation: 'GET /cgi-bin/appmsgpublish',
    classification: 'authentication_expired',
    request: { sub: 'list', begin: 2, count: 2, fakeid: 'fixture-account-001', token: '<redacted>' },
    response: { base_resp: { ret: 200003, err_msg: 'session expired' } },
    expected: { errorCode: 'wechat_session_expired', committedPages: 1, resumeBegin: 2 },
  },
  {
    domain: 'appmsgpublish',
    name: 'malformed',
    operation: 'GET /cgi-bin/appmsgpublish',
    classification: 'protocol_error',
    request: { sub: 'list', begin: 0, count: 5, fakeid: 'fixture-account-001', token: '<redacted>' },
    response: { base_resp: { ret: 0 }, publish_page: '{not-json' },
    expected: { errorCode: 'wechat_protocol_incompatible', partialWrites: 0 },
  },
  {
    domain: 'appmsgpublish',
    name: 'date-boundary',
    operation: 'GET /cgi-bin/appmsgpublish',
    classification: 'range_complete',
    request: { sub: 'list', begin: 4, count: 2, fakeid: 'fixture-account-001', token: '<redacted>', stopBefore: 1767225600 },
    response: { base_resp: { ret: 0 }, total_count: 8, publish_page: publishPage([article({ aid: 'fixture-aid-old', title: '边界之前', create_time: 1767139199, update_time: 1767139199, link: 'https://mp.weixin.qq.com/s/fixture-old' })]) },
    expected: { acceptedItems: [], stopReason: 'date_boundary', nextBegin: null, complete: true },
  },
  {
    domain: 'album',
    name: 'forward',
    operation: 'GET /mp/appmsgalbum',
    classification: 'success',
    request: { action: 'getalbum', __biz: 'fixture-account-001', album_id: 'fixture-album-001', is_reverse: '0', count: 2 },
    response: { base_resp: { ret: 0 }, getalbum_resp: { base_info: { title: '示例合集', nickname: '示例公众号', article_count: '3', is_reverse: '0' }, article_list: [{ msgid: '10001', itemidx: '1', title: '第一篇', url: 'https://mp.weixin.qq.com/s/fixture-album-1', create_time: '1767225600' }, { msgid: '10002', itemidx: '1', title: '第二篇', url: 'https://mp.weixin.qq.com/s/fixture-album-2', create_time: '1767139200' }], continue_flag: '1', reverse_continue_flag: '0' } },
    expected: { album: { id: 'fixture-album-001', title: '示例合集', articleCount: 3 }, order: 'forward', itemCount: 2, next: { beginMsgID: '10002', beginItemIndex: '1' } },
  },
  {
    domain: 'album',
    name: 'reverse',
    operation: 'GET /mp/appmsgalbum',
    classification: 'success',
    request: { action: 'getalbum', __biz: 'fixture-account-001', album_id: 'fixture-album-001', is_reverse: '1', count: 2 },
    response: { base_resp: { ret: 0 }, getalbum_resp: { base_info: { title: '示例合集', nickname: '示例公众号', article_count: '3', is_reverse: '1' }, article_list: [{ msgid: '10003', itemidx: '1', title: '第三篇', url: 'https://mp.weixin.qq.com/s/fixture-album-3', create_time: '1767043200' }], continue_flag: '0', reverse_continue_flag: '1' } },
    expected: { order: 'reverse', itemCount: 1, next: { beginMsgID: '10003', beginItemIndex: '1' } },
  },
  {
    domain: 'album',
    name: 'continuation',
    operation: 'GET /mp/appmsgalbum',
    classification: 'success',
    request: { action: 'getalbum', __biz: 'fixture-account-001', album_id: 'fixture-album-001', begin_msgid: '10002', begin_itemidx: '1', count: 2 },
    response: { base_resp: { ret: 0 }, getalbum_resp: { article_list: [{ msgid: '10003', itemidx: '1', title: '第三篇', url: 'https://mp.weixin.qq.com/s/fixture-album-3', create_time: '1767043200' }], continue_flag: '0', reverse_continue_flag: '0' } },
    expected: { itemCount: 1, deduplicatedCount: 1, complete: true, checkpoint: null },
  },
  {
    domain: 'album',
    name: 'empty',
    operation: 'GET /mp/appmsgalbum',
    classification: 'success',
    request: { action: 'getalbum', __biz: 'fixture-account-001', album_id: 'fixture-album-empty', count: 20 },
    response: { base_resp: { ret: 0 }, getalbum_resp: { article_list: [], continue_flag: '0', reverse_continue_flag: '0' } },
    expected: { items: [], complete: true },
  },
  {
    domain: 'album',
    name: 'duplicate',
    operation: 'GET /mp/appmsgalbum',
    classification: 'success',
    request: { action: 'getalbum', __biz: 'fixture-account-001', album_id: 'fixture-album-001', count: 20 },
    response: { base_resp: { ret: 0 }, getalbum_resp: { article_list: [{ msgid: '10001', itemidx: '1', title: '第一篇', url: 'https://mp.weixin.qq.com/s/fixture-album-1' }, { msgid: '10001', itemidx: '1', title: '第一篇', url: 'https://mp.weixin.qq.com/s/fixture-album-1' }], continue_flag: '0' } },
    expected: { received: 2, stored: 1, duplicateKeys: ['10001:1'] },
  },
  {
    domain: 'metadata',
    name: 'success',
    operation: 'GET article with credential',
    classification: 'success',
    request: { url: 'https://mp.weixin.qq.com/s/fixture-article-001', credentialRef: 'credential:fixture-account-001', cookie: '<redacted>', key: '<redacted>', pass_ticket: '<redacted>', appmsg_token: '<redacted>' },
    response: { user_info: { appmsg_bar_data: { read_num: 1200, old_like_count: 31, share_count: 17, like_count: 42, comment_count: 6 } } },
    expected: { articleId: 'fixture-article-001', readCount: 1200, oldLikeCount: 31, shareCount: 17, likeCount: 42, commentCount: 6, credentialRef: 'credential:fixture-account-001', capturedAt: capturedAt },
  },
  {
    domain: 'metadata',
    name: 'missing-credential',
    operation: 'metadata preflight',
    classification: 'credential_missing',
    request: { articleId: 'fixture-article-001', accountId: 'fixture-account-001' },
    response: { networkRequests: 0 },
    expected: { errorCode: 'credential_missing', action: 'credential import', networkRequests: 0 },
  },
  {
    domain: 'metadata',
    name: 'expired-credential',
    operation: 'GET article with credential',
    classification: 'credential_expired',
    request: { url: 'https://mp.weixin.qq.com/s/fixture-article-001', credentialRef: 'credential:fixture-account-001', cookie: '<redacted>' },
    response: { classification: 'authentication_expired', status: 200, bodyKind: 'login_page' },
    expected: { errorCode: 'credential_expired', credentialStatus: 'invalid', retryable: false },
  },
  {
    domain: 'comments',
    name: 'multi-page',
    operation: 'GET /mp/appmsg_comment?action=getcomment',
    classification: 'success',
    request: { comment_id: 'fixture-comment-stream', buffer: '', key: '<redacted>', pass_ticket: '<redacted>', appmsg_token: '<redacted>' },
    response: { pages: [{ base_resp: { ret: 0 }, continue_flag: true, buffer: 'fixture-buffer-2', elected_comment: [{ content_id: 'fixture-comment-001', nick_name: '读者甲', content: '第一条', create_time: 1767225600, like_num: 2 }] }, { base_resp: { ret: 0 }, continue_flag: false, buffer: '', elected_comment: [{ content_id: 'fixture-comment-002', nick_name: '读者乙', content: '第二条', create_time: 1767225601, like_num: 1 }] }] },
    expected: { items: [{ id: 'fixture-comment-001', author: '读者甲', content: '第一条' }, { id: 'fixture-comment-002', author: '读者乙', content: '第二条' }], pageCount: 2, complete: true },
  },
  {
    domain: 'comments',
    name: 'duplicate',
    operation: 'GET /mp/appmsg_comment?action=getcomment',
    classification: 'success',
    request: { comment_id: 'fixture-comment-stream', buffer: 'fixture-buffer-2', key: '<redacted>' },
    response: { elected_comment: [{ content_id: 'fixture-comment-001', nick_name: '读者甲', content: '第一条' }, { content_id: 'fixture-comment-001', nick_name: '读者甲', content: '第一条' }] },
    expected: { received: 2, stored: 1, duplicateKeys: ['fixture-comment-001'] },
  },
  {
    domain: 'comments',
    name: 'continuation',
    operation: 'GET /mp/appmsg_comment?action=getcomment',
    classification: 'partial',
    request: { comment_id: 'fixture-comment-stream', buffer: 'fixture-buffer-2', key: '<redacted>' },
    response: { base_resp: { ret: -1, err_msg: 'temporary upstream failure' } },
    expected: { persistedPages: 1, checkpoint: { buffer: 'fixture-buffer-2' }, retryable: true, complete: false },
  },
  {
    domain: 'replies',
    name: 'complete',
    operation: 'GET /mp/appmsg_comment?action=getcommentreply',
    classification: 'success',
    request: { content_id: 'fixture-comment-001', max_reply_id: 0, key: '<redacted>', pass_ticket: '<redacted>' },
    response: { base_resp: { ret: 0 }, reply_list: { max_reply_id: 2, reply_list: [{ reply_id: 1, nick_name: '示例作者', content: '谢谢阅读', create_time: 1767225700 }, { reply_id: 2, nick_name: '读者甲', content: '继续关注', create_time: 1767225800 }] } },
    expected: { items: [{ id: '1', author: '示例作者', content: '谢谢阅读' }, { id: '2', author: '读者甲', content: '继续关注' }], complete: true },
  },
  {
    domain: 'replies',
    name: 'partial-failure',
    operation: 'GET /mp/appmsg_comment?action=getcommentreply',
    classification: 'partial',
    request: { contentIds: ['fixture-comment-001', 'fixture-comment-002'], key: '<redacted>' },
    response: { completed: ['fixture-comment-001'], failed: [{ content_id: 'fixture-comment-002', error: 'temporary upstream failure' }] },
    expected: { completedThreads: 1, failedThreads: 1, jobState: 'partial', retryable: true },
  },
  {
    domain: 'replies',
    name: 'resume',
    operation: 'resume reply job',
    classification: 'success',
    request: { completedContentIds: ['fixture-comment-001'], pendingContentIds: ['fixture-comment-002'], checkpoint: { max_reply_id: 8 }, key: '<redacted>' },
    response: { base_resp: { ret: 0 }, content_id: 'fixture-comment-002', reply_list: { max_reply_id: 9, reply_list: [{ reply_id: 9, nick_name: '读者乙', content: '恢复成功' }] } },
    expected: { skippedThreads: ['fixture-comment-001'], completedThreads: ['fixture-comment-002'], duplicateWrites: 0, jobState: 'completed' },
  },
  {
    domain: 'exports',
    name: 'html',
    operation: 'Web Exporter HTML',
    classification: 'success',
    request: { format: 'html', includeComments: true, strictResources: true, articleIds: ['fixture-article-001'] },
    response: { article: exportBody, resourceMap: { 'https://mmbiz.qpic.cn/mmbiz_jpg/fixture/640': './assets/fixture-image.jpg' } },
    expected: { extension: '.html', mediaType: 'text/html; charset=utf-8', offline: true, requiredSelectors: ['#js_article', '#js_content'], forbiddenSelectors: ['script', '#js_top_ad_area'], localResources: ['./assets/fixture-image.jpg'], includesComments: true },
  },
  {
    domain: 'exports',
    name: 'text',
    operation: 'Web Exporter text',
    classification: 'success',
    request: { format: 'text', metadataHeader: true, articleIds: ['fixture-article-001'] },
    response: { article: exportBody },
    expected: { extension: '.txt', encoding: 'utf-8', exactText: exportBody.text },
  },
  {
    domain: 'exports',
    name: 'markdown',
    operation: 'Web Exporter Markdown',
    classification: 'success',
    request: { format: 'markdown', frontMatter: true, allowUnsafeHTML: false, articleIds: ['fixture-article-001'] },
    response: { article: exportBody },
    expected: { extension: '.md', encoding: 'utf-8', exactMarkdown: exportBody.markdown, unsafeHTML: false },
  },
  {
    domain: 'exports',
    name: 'json',
    operation: 'Web Exporter JSON',
    classification: 'success',
    request: { format: 'json', includeContent: true, includeMetrics: true, includeComments: true, articleIds: ['fixture-article-001'] },
    response: { article: representative, rendered: exportBody, metrics: { readCount: 1200, likeCount: 42, commentCount: 6 } },
    expected: { schemaVersion: 1, articleCount: 1, requiredFields: ['schemaVersion', 'articles', 'provenance'], includes: ['content', 'metrics', 'comments', 'replies', 'albums'] },
  },
  {
    domain: 'exports',
    name: 'xlsx',
    operation: 'Web Exporter ExcelJS',
    classification: 'success',
    request: { format: 'xlsx', includeContent: true, articleIds: ['fixture-article-001'] },
    response: { article: representative, renderedText: exportBody.text, metrics: { readCount: 1200, oldLikeCount: 31, shareCount: 17, likeCount: 42, commentCount: 6 } },
    expected: { extension: '.xlsx', mediaType: 'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet', sheets: [{ name: 'Sheet1', rowCount: 2, columns: ['公众号', 'ID', '链接', '标题', '封面', '摘要', '创建时间', '发布时间', '阅读', '点赞', '分享', '喜欢', '留言', '作者', '是否原创', '文章类型', '所属合集', '文章内容'] }], streamingRequired: true },
  },
  {
    domain: 'exports',
    name: 'docx',
    operation: 'Web Exporter html-docx-js',
    classification: 'success',
    request: { format: 'docx', includeComments: true, articleIds: ['fixture-article-001'] },
    response: { article: exportBody },
    expected: { extension: '.docx', mediaType: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', requiredPackageEntries: ['[Content_Types].xml', '_rels/.rels', 'word/document.xml'], structures: ['heading', 'paragraph', 'hyperlink', 'list', 'quote', 'code', 'table', 'image', 'comments'] },
  },
  {
    domain: 'exports',
    name: 'pdf',
    operation: 'Web Exporter PDF',
    classification: 'success',
    request: { format: 'pdf', browser: 'chromium', includeComments: true, articleIds: ['fixture-article-001'] },
    response: { selfContainedHtml: true, article: exportBody },
    expected: { extension: '.pdf', mediaType: 'application/pdf', magic: '%PDF-', pageFormat: 'A4', printBackground: true, remoteRequests: 0, localChromiumRequired: true, includesComments: true },
  },
];

function serialize(value: Json): string {
  return `${JSON.stringify(value, null, 2)}\n`;
}

function writeOrVerify(filePath: string, value: Json): void {
  const rendered = serialize(value);
  if (verifyMode) {
    assert.equal(fs.existsSync(filePath), true, `missing baseline file: ${path.relative(repositoryRoot, filePath)}`);
    assert.equal(fs.readFileSync(filePath, 'utf8'), rendered, `stale baseline file: ${path.relative(repositoryRoot, filePath)}`);
    return;
  }
  fs.mkdirSync(path.dirname(filePath), { recursive: true });
  fs.writeFileSync(filePath, rendered, 'utf8');
}

for (const fixture of cases) {
  const caseRoot = path.join(fixtureRoot, fixture.domain, fixture.name);
  writeOrVerify(path.join(caseRoot, 'case.json'), {
    schemaVersion: 1,
    domain: fixture.domain,
    name: fixture.name,
    operation: fixture.operation,
    classification: fixture.classification,
    provenance: 'sanitized-synthetic-shape-derived-from-current-web-implementation',
    capturedAt,
    sanitization: ['tokens replaced', 'cookies omitted', 'private identifiers pseudonymized', 'article body synthetic'],
    notes: fixture.notes || [],
  });
  writeOrVerify(path.join(caseRoot, 'request.sanitized.json'), fixture.request);
  writeOrVerify(path.join(caseRoot, 'response.sanitized.json'), fixture.response);
  writeOrVerify(path.join(caseRoot, 'expected.normalized.json'), fixture.expected);
}

const coverage = JSON.parse(fs.readFileSync(coveragePath, 'utf8')) as {
  schemaVersion: number;
  domains: Record<string, { status: string; required: string[] }>;
};
assert.equal(coverage.schemaVersion, 1);
for (const [domain, definition] of Object.entries(coverage.domains)) {
  assert.equal(definition.status, 'captured', `${domain}: coverage status must be captured`);
  const names = new Set(cases.filter(item => item.domain === domain).map(item => item.name));
  for (const required of definition.required) {
    assert.equal(names.has(required), true, `${domain}: missing required case ${required}`);
  }
}

const extra = cases.filter(item => !coverage.domains[item.domain]);
assert.deepEqual(extra, [], `fixture cases use undeclared domains: ${extra.map(item => `${item.domain}/${item.name}`).join(', ')}`);

console.log(`${verifyMode ? 'verified' : 'captured'} ${cases.length} sanitized protocol/export baseline cases`);
