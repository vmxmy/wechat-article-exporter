# wechat-article CLI

`wechat-article` 是微信文章导出器的 remote-only 命令行客户端：

- Cobra 提供稳定的命令、参数校验、帮助和 shell completion。
- Bubble Tea 提供无参数启动时的交互入口，以及登录和远端调用的终端反馈。
- 官方 Go MCP SDK 负责 Streamable HTTP `/mcp` 通信。
- OAuth 2.1 access/refresh token 保存在本地 `0600` 配置文件，不会出现在 status、错误或 dry-run 输出中。

## 构建

需要 Go 1.24 或更高版本。

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

# 首次授权；无浏览器服务器增加 --headless
wechat-article login --server https://mptext.ziikoo.app

# 发现服务端当前公开的工具
wechat-article api list
wechat-article api describe download_article

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
