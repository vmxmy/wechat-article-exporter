/**
 * Wechat Article Exporter — MCP Server (Cloudflare Worker)
 * v2: OAuth 2.1 (workers-oauth-provider) + stateless MCP (createMcpHandler)
 *
 * 角色：
 *   - Resource Server: POST /mcp（Streamable HTTP，无状态；Bearer 由库校验）
 *   - Authorization Server: /authorize（微信扫码同意页）+ /token + /register（库实现）
 * 凭证流转：/authorize 完成后把微信会话/凭证作为加密 props 绑到 token；
 *           工具内用 getMcpAuthContext().props 读取，零服务端会话存储。
 *
 * 部署前：npm install（按需 pin 版本）→ 建 OAUTH_KV → 填 wrangler.toml 的 id。
 * 注意：本文件是 feat/mcp-oauth 分支脚手架，需 preview 部署 + 扫码联调后再上正式。
 */
import OAuthProvider from '@cloudflare/workers-oauth-provider';
import { authHandler } from './auth';
import { MCP_ROUTE } from './constants';
import { mcpApiHandler } from './mcp';

export default new OAuthProvider({
  // /mcp 之外的请求（/authorize、根路径等）走 authHandler
  apiHandlers: {
    [MCP_ROUTE]: mcpApiHandler,
  },
  defaultHandler: authHandler,
  authorizeEndpoint: '/authorize',
  tokenEndpoint: '/token',
  clientRegistrationEndpoint: '/register', // RFC 7591 DCR，库实现
  scopesSupported: ['wechat.read'],
});
