# Cobra、TUI、本地 MCP 与浏览器工作区

四个本地入口共用同一个 application core、active profile、SQLite、对象存储和持久化任务。它们不分别实现微信协议、下载、解析或导出规则。浏览器工作区是 loopback-only HTTP 展示适配器；MCP 仍严格使用 stdio，不监听 TCP。

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

## 本地浏览器工作区

```bash
wechat-article web
```

浏览器工作区固定使用随机 `127.0.0.1` IPv4 listener、一次性 bootstrap URL、HttpOnly session、Host/Origin/CSRF 验证与禁缓存安全头。它与其他入口共享 active profile 和持久作业；切换 profile 后重启浏览器工作区。

浏览器不接收任意绝对路径：导出使用服务端授权的默认 export root/子目录 token；生成文件通过 opaque artifact capability 流式下载，打开所选导出的输出目录需要该导出的精确确认值。Credential 字段只写入不回显。保存的文章查询、受限本地预览、bounded job detail、维护设置、GC 计划/确认执行，以及 opaque diagnostic bundle 下载均已接入共享应用层；文章/资源/评论下载及专辑遍历/批量下载使用共享持久作业。Settings 还支持一个最多 2 GiB 的恢复归档：浏览器上传后只持有私有 staged archive 的不透明 handle，prepare 生成一次性精确确认值，commit 后工作区服务器会关闭，需重新运行 `wechat-article web`。这不开放任意主机路径 API 或通用文件上传；账号 manifest、Credential 文件上传和批量导出仍使用 Cobra/TUI/MCP。请使用功能矩阵确认入口选择：[browser-capability-matrix.md](../release/browser-capability-matrix.md)。详细操作与无障碍/语言排错见[本地浏览器工作区](./browser-workspace.md)。
