# wechat-article CLI

`wechat-article` 是本地优先的单二进制产品。Cobra、Bubble Tea 和本地 stdio MCP 共用 `internal/application.Application`，正常运行只访问微信或用户显式配置的代理。

## 开发启动

需要 Go 1.25 或更高版本：

```bash
cd cli
go mod download
go test ./...
go run ./cmd/wechat-article help
go run ./cmd/wechat-article status --json
go run ./cmd/wechat-article
```

不带子命令且 stdin/stdout 是 TTY 时进入 Bubble Tea；非 TTY 时打印帮助，不阻塞。

构建本地二进制：

```bash
go build -trimpath -o ./bin/wechat-article ./cmd/wechat-article
./bin/wechat-article --version
```

## 本地流程

```bash
./bin/wechat-article profile create work
./bin/wechat-article profile use work
./bin/wechat-article login --qr-output ./wechat-login.png
./bin/wechat-article account search '公众号名称' --json
./bin/wechat-article sync account ACCOUNT_ID --follow
./bin/wechat-article download article --url 'https://mp.weixin.qq.com/s/ARTICLE' --follow
./bin/wechat-article export start --url 'https://mp.weixin.qq.com/s/ARTICLE' --format markdown --output ./exports --follow
```

`--json` 保证 stdout 为一个版本化 JSON 文档。成功、运行错误、用法错误退出码分别为 `0`、`1`、`2`。

## stdio MCP

```bash
./bin/wechat-article mcp serve --transport stdio
```

server 不监听网络，不使用 OAuth。stdout 只包含换行分隔的 JSON-RPC/MCP 消息，日志写 stderr。profile 的 `config.json` 可设置：

```json
{
  "mcp": {
    "readOnly": true,
    "allow": ["articles.query", "jobs.get", "jobs.query", "runtime.status"],
    "deny": ["jobs.cancel"]
  }
}
```

破坏性工具需要 `confirm:<tool-name>` 精确确认值；敏感 Credential 操作还需要独立启用与确认。

## 测试与发布

```bash
gofmt -w $(find . -name '*.go' -type f)
go test ./...
go test -race ./...
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 -checks='SA*,S1*,QF*' ./...
```

`Release Go CLI` 工作流构建 macOS arm64/amd64、Linux arm64/amd64、Windows amd64 的 `CGO_ENABLED=0` 压缩包、CycloneDX SBOM 和 `checksums.txt`。
