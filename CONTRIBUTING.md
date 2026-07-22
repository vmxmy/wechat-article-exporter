# 贡献指南

本仓库维护 `wechat-article` 本地 Go 产品。请先阅读 [行为准则](./CODE_OF_CONDUCT.md)，并在提交前搜索现有 Issue。

## 开发环境

- Go 1.25 或更高版本；
- Git；
- 可选：本机 Chromium 系浏览器，用于 PDF 集成验证；
- 可选：LibreOffice Writer，用于 DOCX 打开验证。

```bash
git clone git@github.com:wechat-article/wechat-article-exporter.git
cd wechat-article-exporter/cli
go mod download
go test ./...
go build -o ./bin/wechat-article ./cmd/wechat-article
```

本地启动：

```bash
cd cli
go run ./cmd/wechat-article help
go run ./cmd/wechat-article status --json
go run ./cmd/wechat-article
```

最后一条命令需要真实 TTY，进入 Bubble Tea 工作区。开发时可通过 `--debug` 打开脱敏的详细日志。

## 代码与模块

- `cli/internal/application`：Cobra、TUI、MCP 共用的用例层；
- `wechat`、`network`：微信协议与路由策略；
- `library`、`objects`、`jobs`：SQLite、对象存储和持久化任务；
- `processor`、`exporter`：解析、渲染和所有导出格式；
- `profiles`、`secrets`：profile 配置和凭据；
- `tui`、`mcp`、`app`：三个产品适配器与组合根。

业务规则应进入共享模块，不要在 Cobra、Bubble Tea 或 MCP 适配器中重复实现。

## 提交前验证

至少运行：

```bash
cd cli
gofmt -w $(find . -name '*.go' -type f)
go test ./...
go vet ./...
```

涉及并发、任务、数据库或资源生命周期时再运行：

```bash
go test -race ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 -checks='SA*,S1*,QF*' ./...
```

涉及解析和导出时，更新 `samples/` 或对应 `internal/*/testdata/` fixture，并运行相关 golden/结构测试。涉及数据库 schema 时，必须增加顺序 migration 和所有受支持 baseline 的升级测试。

## 安全要求

- 不提交微信 Cookie、token、Credential、代理授权、文章私有数据或真实数据库；
- fixture 必须脱敏；
- 新日志、错误、JSON 和诊断字段必须经过统一 redaction；
- 敏感请求不得默认经过未受信任代理；
- MCP stdout 不得输出日志、banner 或进度；
- 破坏性操作必须保留精确确认值和可恢复性说明。

## Pull Request

PR 请说明用户可见变化、测试命令、配置/迁移影响、已知缺口。提交信息保持窄范围，例如：

```text
fix: resume comment replies from saved checkpoint
feat: add deterministic markdown batch manifests
```

不要在同一提交中混入无关格式化或生成文件。
