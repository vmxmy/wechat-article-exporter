/**
 * MCP 工具层：createMcpHandler（无状态，纯 Worker，不要 Durable Object）。
 * 鉴权改为 OAuth：去掉旧的 auth_key 入参，需鉴权的工具从 getMcpAuthContext().props 取凭证。
 */
import { McpServer } from '@modelcontextprotocol/sdk/server/mcp.js';
import { createMcpHandler, getMcpAuthContext } from 'agents/mcp';
import { z } from 'zod';
import { MCP_ROUTE } from './constants';

export interface Env {
  EXPORTER_BASE_URL: string;
}

// /authorize 完成时写入的 props（见 auth.ts）
export interface WxProps {
  apiToken: string; // exporter 的 X-Auth-Key（Phase 1 最小改动；Phase 2 改为内嵌 {token,cookies}）
  name?: string;
}

function buildServer(env: Env): McpServer {
  const server = new McpServer({ name: 'wechat-article-exporter', version: '2.0.0' });
  const base = env.EXPORTER_BASE_URL;

  const call = async (path: string, params: Record<string, unknown>, needAuth = false): Promise<string> => {
    const url = new URL(base + path);
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== '') url.searchParams.set(k, String(v));
    }
    const headers: Record<string, string> = {};
    if (needAuth) {
      const props = getMcpAuthContext()?.props as WxProps | undefined;
      if (!props?.apiToken) throw new Error('未授权：缺少微信会话，请重新完成 OAuth 授权');
      headers['X-Auth-Key'] = props.apiToken;
    }
    const res = await fetch(url, { headers });
    if (!res.ok) throw new Error(`上游请求失败: ${res.status} ${res.statusText}`);
    return res.text();
  };
  const text = (t: string) => ({ content: [{ type: 'text' as const, text: t }] });

  // ── 需鉴权（用 OAuth props 的 apiToken）─────────────────────────────
  server.tool(
    'download_article',
    '下载微信公众号文章内容，返回指定格式（markdown / text / html / json）。',
    {
      url: z.string().describe('微信文章链接 https://mp.weixin.qq.com/s/...'),
      format: z.enum(['markdown', 'text', 'html', 'json']).default('markdown'),
    },
    async ({ url, format }) => text(await call('/api/public/v1/download', { url, format }, true))
  );

  server.tool(
    'search_accounts',
    '按关键词搜索微信公众号，返回匹配列表（含 fakeid）。',
    {
      keyword: z.string(),
      begin: z.number().int().min(0).default(0),
      size: z.number().int().min(1).max(20).default(5),
    },
    async ({ keyword, begin, size }) => text(await call('/api/public/v1/account', { keyword, begin, size }, true))
  );

  server.tool(
    'list_articles',
    '获取指定公众号的文章列表，支持关键词过滤和分页。',
    {
      fakeid: z.string(),
      keyword: z.string().optional(),
      begin: z.number().int().min(0).default(0),
      size: z.number().int().min(1).max(20).default(5),
    },
    async ({ fakeid, keyword, begin, size }) =>
      text(await call('/api/public/v1/article', { fakeid, keyword, begin, size }, true))
  );

  // ── 无需鉴权 ──────────────────────────────────────────────────────
  server.tool(
    'get_account_by_url',
    '从微信文章链接提取公众号信息（含 fakeid）。',
    { url: z.string() },
    async ({ url }) => text(await call('/api/public/v1/accountbyurl', { url }))
  );

  server.tool(
    'get_account_details',
    '公众号详细资料：简介、微信号、认证、历史名称、IP 归属地等。',
    { fakeid: z.string() },
    async ({ fakeid }) => text(await call('/api/public/beta/aboutbiz', { fakeid }))
  );

  server.tool('get_author_info', '获取公众号作者/机构元数据。', { fakeid: z.string() }, async ({ fakeid }) =>
    text(await call('/api/public/beta/authorinfo', { fakeid }))
  );

  server.tool(
    'list_album',
    '获取公众号指定合集（专辑）的文章列表，支持分页。',
    {
      fakeid: z.string(),
      album_id: z.string(),
      count: z.number().int().min(1).max(20).default(10),
      begin_msgid: z.string().optional(),
      begin_itemidx: z.string().optional(),
    },
    async a => text(await call('/api/web/misc/appmsgalbum', a))
  );

  server.tool(
    'get_account_name',
    '从微信文章链接快速获取公众号名称，无需 fakeid。',
    { url: z.string() },
    async ({ url }) => text(await call('/api/web/misc/accountname', { url }))
  );

  return server;
}

// OAuthProvider 把已鉴权请求路由到这里；createMcpHandler 走 Streamable HTTP（单一 /mcp 端点，
// POST 下行 + GET 事件流 + DELETE 关会话），props 由库注入、经 getMcpAuthContext() 读取。
export const mcpApiHandler = {
  fetch(request, env, ctx) {
    return createMcpHandler(buildServer(env), { route: MCP_ROUTE })(request, env, ctx);
  },
} satisfies ExportedHandler<Env>;
