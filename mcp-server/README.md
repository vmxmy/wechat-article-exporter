# wechat-article-exporter MCP Server

Cloudflare Workers 上的 [MCP](https://modelcontextprotocol.io/) 服务器，将微信公众号文章导出能力暴露给 AI 助手（Claude、Cursor 等）。

## 前提条件

| 条件 | 说明 |
|---|---|
| 已部署 wechat-article-exporter | 需要一个可访问的实例（Cloudflare Pages / Docker / 本地） |
| Cloudflare 账号 | 用于部署 Worker |
| [Wrangler CLI](https://developers.cloudflare.com/workers/wrangler/) | `npm i -g wrangler` |

### 服务端环境变量

实际生效变量以 `wrangler.toml [vars]` / `[env.preview.vars]` 为准：`EXPORTER_BASE_URL`（exporter 实例基址）。鉴权走 OAuth 2.1，不再支持静态 `MCP_API_KEY`。

## 部署

默认先部署到 preview，避免误覆盖正式 Worker：

```bash
cd mcp-server
COREPACK_ENABLE_PROJECT_SPEC=0 pnpm install
# [env.preview] 已配置独立 OAUTH_KV；如需重建，执行后替换 wrangler.toml 中的 preview id
# wrangler kv namespace create OAUTH_KV --env preview
wrangler deploy --env preview
```

preview 部署成功后 Worker URL 格式为 `https://wechat-article-mcp-preview.<your-subdomain>.workers.dev`。确认 OAuth 与 MCP 工具验收通过后，再执行正式部署：

```bash
wrangler deploy
```

## 工具列表

### 无需鉴权

| 工具 | 说明 |
|---|---|
| `get_account_by_url` | 从文章链接提取公众号信息（含 fakeid） |
| `get_account_details` | 获取公众号详情（需服务端配置 `NUXT_WECHAT_ABOUT_BIZ_*` 环境变量） |
| `get_author_info` | 获取公众号主体元数据 |
| `get_account_name` | 从文章链接快速获取公众号名称 |
| `list_album` | 获取公众号合集文章列表。**需要 exporter 服务端有有效的微信登录会话** |

### 需要 OAuth 授权

首次连接客户端时会打开 OAuth 同意页。Phase 1 仍需要用户在同意页粘贴从 exporter「设置」页复制的 `auth_key`，授权完成后客户端使用 OAuth access token；MCP 工具参数中不再传 `auth_key`。

| 工具 | 说明 |
|---|---|
| `download_article` | 下载文章内容，支持 markdown / text / html / json 格式 |
| `search_accounts` | 按关键词搜索公众号 |
| `list_articles` | 获取指定公众号的文章列表（支持关键词过滤与分页） |

### 工具参数速查

<details>
<summary>download_article</summary>

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `url` | string | 是 | 微信文章链接 `https://mp.weixin.qq.com/s/...` |
| `format` | string | 否 | `markdown`（默认）/ `text` / `html` / `json` |
</details>

<details>
<summary>get_account_by_url / get_account_name</summary>

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `url` | string | 是 | 该公众号发布的任意文章链接 |
</details>

<details>
<summary>get_account_details / get_author_info</summary>

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `fakeid` | string | 是 | 公众号内部 ID（从 get_account_by_url 获取） |
</details>

<details>
<summary>search_accounts</summary>

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `keyword` | string | 是 | 搜索关键词 |
| `begin` | integer | 否 | 分页偏移（默认 0） |
| `size` | integer | 否 | 返回数量（默认 5，最大 20） |
</details>

<details>
<summary>list_articles</summary>

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `fakeid` | string | 是 | 公众号内部 ID |
| `keyword` | string | 否 | 标题关键词过滤 |
| `begin` | integer | 否 | 分页偏移（默认 0） |
| `size` | integer | 否 | 返回数量（默认 5，最大 20） |
</details>

<details>
<summary>list_album</summary>

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `fakeid` | string | 是 | 公众号内部 ID |
| `album_id` | string | 是 | 合集 ID |
| `count` | integer | 否 | 返回数量（默认 10，最大 20） |
| `begin_msgid` | string | 否 | 分页起始消息 ID |
| `begin_itemidx` | string | 否 | 分页起始索引 |
</details>

## Transport

本服务走 **Streamable HTTP**（`POST /mcp` 下行 JSON-RPC + `GET /mcp` 事件流 + `DELETE /mcp` 关会话），由 `agents/mcp` 的 `createMcpHandler` 默认实现。不提供 legacy SSE 端点。

## 客户端配置

鉴权采用 OAuth 2.1（`workers-oauth-provider`）：首次连接会在浏览器弹出同意页，授权后令牌缓存在客户端。

### Claude Desktop / Cursor（原生支持 Streamable HTTP + OAuth）

preview 验收时使用 preview 地址；正式发布后再替换为 `https://mptext.ziikoo.app/mcp`。

```json
{
  "mcpServers": {
    "wechat-article": {
      "url": "https://wechat-article-mcp-preview.<your-subdomain>.workers.dev/mcp"
    }
  }
}
```

### 客户端不支持 HTTP-only MCP 时的回退（mcp-remote 桥接）

```json
{
  "mcpServers": {
    "wechat-article": {
      "command": "npx",
      "args": ["mcp-remote", "https://wechat-article-mcp-preview.<your-subdomain>.workers.dev/mcp"]
    }
  }
}
```

## 开发

```bash
cd mcp-server
COREPACK_ENABLE_PROJECT_SPEC=0 pnpm install
wrangler dev   # 本地调试，监听 localhost:8787/mcp
```
