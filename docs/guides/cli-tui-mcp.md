# Cobra、TUI 与本地 MCP

三个入口共用同一个 application core、active profile、SQLite、对象存储和持久化任务。它们不分别实现微信协议、下载、解析或导出规则。

## Cobra

```bash
wechat-article status --json
wechat-article account search OpenAI
wechat-article sync account ACCOUNT_ID --follow
wechat-article download article --url 'https://mp.weixin.qq.com/s/...' --follow
wechat-article export start --url 'https://mp.weixin.qq.com/s/...' --format html --output ./exports --follow
```

`--json` 模式 stdout 只输出一个版本化 JSON 文档，进度写 stderr。退出码：成功 `0`，运行失败 `1`，用法错误 `2`。

## Bubble Tea

在 stdin/stdout 均为 TTY 时，不带子命令运行进入终端工作区：

```bash
wechat-article
```

非 TTY 环境不会自动启动交互程序。工作区提供账号、文章、合集、下载、任务、导出、Credential、代理、存储和诊断入口；富 HTML 预览需要显式交给本地浏览器。

## stdio MCP

```bash
wechat-article mcp serve --transport stdio
```

server 不监听 TCP，不需要 remote OAuth。stdout 专用于 JSON-RPC/MCP，日志写 stderr；不要使用 `2>&1`。进程启动时读取 active profile，切换 profile 后应重启 MCP server。

通用配置：

```json
{
  "command": "wechat-article",
  "args": ["mcp", "serve", "--transport", "stdio"]
}
```

Claude Desktop 和 Cursor 把它放在 `mcpServers.wechat-article`；Codex 使用：

```toml
[mcp_servers.wechat_article]
command = "wechat-article"
args = ["mcp", "serve", "--transport", "stdio"]
```

## MCP 权限策略

profile 配置中的 `mcp.readOnly`、`allow` 和 `deny` 控制工具权限，deny 优先。破坏性工具还要求精确确认参数；策略放行不会绕过确认。自动化 profile 建议默认只读并使用最小 allow-list。
