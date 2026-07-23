# Cobra、TUI 与本地 MCP

三个入口共用同一个 application core、active profile、SQLite、对象存储和持久化任务。它们不分别实现微信协议、下载、解析或导出规则。

## Cobra

```bash
wechat-article status --json
wechat-article account search OpenAI
wechat-article sync account ACCOUNT_ID --follow
wechat-article download article --url 'https://mp.weixin.qq.com/s/...' --follow
wechat-article export start --url 'https://mp.weixin.qq.com/s/...' --format html --output ./exports --follow
wechat-article export verify --root ./exports --manifest export-EXPORT_ID-manifest.json --json
```

`--json` 模式 stdout 只输出一个版本化 JSON 文档，进度写 stderr。退出码：成功 `0`，运行失败 `1`，用法错误 `2`。
`export verify` 发现文件缺失、大小或 SHA-256 不一致时返回 `1`；error envelope 的 `data` 保留完整 verification report、affected article IDs、expected 和 actual 值，不会输出第二份 JSON。

需要加密 vault 或文章 Credential 时：

```bash
wechat-article vault init --passphrase-file ./vault-passphrase
wechat-article credential import --interactive
wechat-article credential validate CREDENTIAL_ID --json
wechat-article diagnostics bundle --output ./diagnostics.zip
```

## Bubble Tea

在 stdin/stdout 均为 TTY 时，不带子命令运行进入终端工作区：

```bash
wechat-article
```

非 TTY 环境不会自动启动交互程序。工作区提供账号、文章、合集、下载、任务、导出、Credential、代理、存储和诊断入口；富 HTML 预览需要显式交给本地浏览器。文章页支持 compound filter 和 saved query；下载前展示 resolved stable-ID 数量。任务与导出页自动刷新持久化进度而不关闭 modal、清空输入或改变 selection。导出操作必须选中稳定 export ID，不会隐式回退到“最新一次”；manifest 视图展示 provenance state/path/SHA-256/generation/error，verification 展示 affected IDs 和 expected/actual 差异。

下载、同步和导出 worker 使用同一 profile SQLite 中的 leased scheduler permits，global、per-operation、per-host 和 credential-sensitive 限制在 detached worker 与多个 CLI 进程间共享。评论 reply thread 部分失败时保留已完成 thread，job 进入 `partial`，retry 只继续未完成 checkpoint。

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

MCP 文件输出只能落在 active profile 数据目录、`preferences.export.root` 或 `mcp.allowedOutputRoots` 中。额外目录必须使用绝对路径；`..` 与 symlink 逃逸会被拒绝：

```json
{
  "mcp": {
    "readOnly": false,
    "allowedOutputRoots": ["/absolute/path/to/exports"]
  }
}
```
