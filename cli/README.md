# wechat-article CLI

`wechat-article` 正在迁移为本地优先的单二进制产品：

- Cobra、Bubble Tea 和后续 stdio MCP 共享 `internal/application.Application` 用例层。
- 本地模块边界已经拆分为 WeChat、SQLite library、objects、jobs、network、processor、exporter、secrets、TUI 和 MCP。
- 旧 Streamable HTTP MCP/OAuth 行为仅保留在 `legacy` 兼容入口；它不属于新的本地应用路径。
- `status` 显示本地 runtime/profile/storage 状态；旧 OAuth 状态使用 `legacy status`。

## 构建

需要 Go 1.25 或更高版本。

```bash
cd cli
go test ./...
go build -o ./bin/wechat-article ./cmd/wechat-article
```

开发时也可以直接运行：

```bash
go run ./cmd/wechat-article --version
go run ./cmd/wechat-article help
```

## 使用

```bash
# 交互首页（仅 TTY）
wechat-article

# 兼容窗口内使用旧远程授权；无浏览器服务器增加 --headless
wechat-article legacy login --server https://mptext.ziikoo.app

# 本地 runtime 状态
wechat-article status --json

# 发现服务端当前公开的工具
wechat-article legacy api list
wechat-article legacy api describe download_article

# 通用调用：三种 JSON 输入来源互斥
wechat-article api call download_article \
  --input '{"url":"https://mp.weixin.qq.com/s/example","format":"markdown"}'
wechat-article api call download_article --file ./request.json
printf '%s' '{"url":"https://mp.weixin.qq.com/s/example"}' | wechat-article api call download_article --stdin

# 高频领域别名走同一个 MCP 调用层
wechat-article article download 'https://mp.weixin.qq.com/s/example' --format markdown
wechat-article account search '公众号名称' --size 5
wechat-article article list FAKEID --begin 0 --size 10
```

行为约定：

- `api list/describe/call` 和 `mcp tools/describe/call` 是等价入口。
- 非交互结果输出 JSON；成功退出码为 `0`，运行或远端错误为 `1`，命令用法错误为 `2`。
- 长正文或敏感参数优先使用 `--file` 或 `--stdin`，避免写入 shell history。
- `--dry-run` 仅输出脱敏预览，不建立 MCP 连接；未来新增的非只读工具需要精确的 `--confirm <tool>`。
- 默认配置路径为 `~/.config/wechat-article-exporter/cli.json`；可用 `WECHAT_ARTICLE_CLI_CONFIG` 覆盖。

## 发布

GitHub Actions 的 `Release Go CLI` 工作流构建以下原生压缩包，并生成 SHA-256 校验文件：

- macOS: arm64 / amd64
- Linux: arm64 / amd64
- Windows: amd64

推送 `wechat-article-v*` 标签会创建 GitHub Release；手动运行工作流可只生成并检查构建产物。

## 本地 stdio MCP

`wechat-article` 可以作为本地 MCP server 运行。Claude、Codex、Cursor 和其他 stdio MCP 客户端都应启动同一条命令：

```bash
wechat-article mcp serve --transport stdio
```

该模式直接使用当前选中的本地 profile、SQLite library 和持久化 jobs，不启动网络监听器，也不依赖旧版 remote OAuth。

### Profile

MCP server 在进程启动时使用当前 active profile。先在终端中创建或切换 profile，再启动或重启 MCP 客户端：

```bash
wechat-article profile list
wechat-article profile use <name>
```

profile 之间的配置、数据库、对象和任务相互隔离。切换 profile 后，应重启客户端中的 `wechat-article` MCP server，使新进程加载新的 active profile。

如果图形客户端找不到 `wechat-article`，把下列示例中的 `command` 改为该二进制的绝对路径；`args` 保持不变。不要使用会向 stdout 打印启动信息的 shell wrapper。

### Claude

在 Claude Desktop 的 MCP 配置文件中加入：

```json
{
  "mcpServers": {
    "wechat-article": {
      "command": "wechat-article",
      "args": ["mcp", "serve", "--transport", "stdio"]
    }
  }
}
```

保存配置后完全退出并重新启动 Claude Desktop。如果配置文件已有其他 server，只添加 `wechat-article` 条目，不要覆盖现有的 `mcpServers`。

### Codex

在用户级 `~/.codex/config.toml` 或受信任项目的 `.codex/config.toml` 中加入：

```toml
[mcp_servers.wechat_article]
command = "wechat-article"
args = ["mcp", "serve", "--transport", "stdio"]
```

Codex CLI 与 IDE 扩展使用相同的配置层。修改配置后启动新会话；可用 `codex mcp list` 检查 server 是否已被加载。

### Cursor

在项目级 `.cursor/mcp.json` 或用户级 `~/.cursor/mcp.json` 中加入：

```json
{
  "mcpServers": {
    "wechat-article": {
      "type": "stdio",
      "command": "wechat-article",
      "args": ["mcp", "serve", "--transport", "stdio"]
    }
  }
}
```

保存后在 Cursor 中重新加载 MCP server 或重启 Cursor。

### 通用 stdio MCP 客户端

支持本地进程型 MCP server 的客户端应使用以下进程配置：

```json
{
  "name": "wechat-article",
  "transport": "stdio",
  "command": "wechat-article",
  "args": ["mcp", "serve", "--transport", "stdio"]
}
```

如果客户端只接受单个命令字符串，填写：

```text
wechat-article mcp serve --transport stdio
```

客户端必须保持 server 的 stdin、stdout 和 stderr 为独立流，并在 stdin 关闭后允许 server 正常退出。

### Read-only 与 allow/deny 策略

每个 profile 的 `config.json` 都有独立的 `mcp` 策略。下面只展示相关字段；保留文件中的其他现有字段：

```json
{
  "mcp": {
    "readOnly": true,
    "allow": ["articles.query", "jobs.get", "jobs.query", "runtime.status"],
    "deny": ["jobs.cancel"]
  }
}
```

- `readOnly: true` 在执行阶段拒绝所有会修改本地状态的工具；查询和状态工具仍可调用。客户端仍可能看到带 mutating annotation 的工具，但调用会被 server 拒绝。
- `allow` 是工具名 allow-list。数组非空时，只允许明确列出的工具；`"*"` 表示全部工具。
- `deny` 是工具名 deny-list，并且优先于 `allow`；即使工具同时出现在两个列表中，也会被拒绝。
- destructive 工具还要求调用方提供精确确认值。确认值格式为 `confirm:<tool-name>`；例如 `jobs.cancel` 需要参数 `"confirm": "confirm:jobs.cancel"`。策略允许不会绕过该确认。
- 敏感凭据操作默认受额外限制；read-only、allow-list 或 destructive confirmation 都不会自动授予读取或传出 secrets 的权限。

建议自动化客户端默认使用 `readOnly: true` 和最小 `allow` 列表，仅在确有需要时为特定 profile 放开写操作。修改 profile 配置后重启 MCP server。

### stdout 隔离与 stderr 日志

stdio transport 把 stdout 当作协议通道：

- stdout 只输出换行分隔的 JSON-RPC/MCP 消息，不输出 banner、进度、诊断文本或普通日志。
- 运行日志和协议错误摘要写入 stderr；客户端应单独捕获或展示 stderr。
- 不要使用 `2>&1`，也不要把 stderr 重定向到 stdout，否则日志会污染协议流并导致客户端解析失败。
- 客户端若报告 malformed JSON，先检查启动 wrapper、shell profile 或进程管理器是否向 stdout 写了额外文本。

本地 stdio server 不需要 URL、bearer token 或 OAuth 回调配置。客户端与 server 的生命周期由子进程 stdin/stdout 管理。
