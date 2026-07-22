# CLAUDE.md

本仓库的产品是 `cli/` 下的本地 Go 二进制。Nuxt、Nitro、远程 MCP/OAuth、Cloudflare Pages/Worker 和 Web Docker 已退役，不应恢复为默认架构。

## 常用命令

```bash
cd cli
go mod download
go test ./...
go test -race ./...
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 -checks='SA*,S1*,QF*' ./...
go build -o ./bin/wechat-article ./cmd/wechat-article
go run ./cmd/wechat-article status --json
```

## 架构

Cobra、Bubble Tea 和本地 stdio MCP 都调用 `internal/application.Application`。微信协议、SQLite、对象存储、任务、网络策略、解析、导出和 secret 管理分别位于对应的 `internal` 深模块中。适配器只处理输入输出，不复制业务规则。

SQLite 是元数据权威，正文和资源使用 SHA-256 内容寻址对象存储。长任务必须进入持久化 job state machine。会话和 Credential 使用 OS keyring 或显式加密 vault。PDF 只允许本地 Chromium，不提供远程回退。

## 约束

- Go 1.25+，提交前运行 `gofmt`、测试和 vet；
- 保留跨平台 build tags 和纯 Go / `CGO_ENABLED=0` 发布能力；
- 不引入远程 OAuth、HTTP MCP listener 或项目托管运行时依赖；
- MCP stdout 只输出协议；
- fixture 和日志必须脱敏；
- 修改数据库 schema 时增加 migration、backup/restore 和兼容 baseline 测试；
- 修改 processor/exporter 时更新 fixture/golden 和结构验证；
- 不提交本地 profile、数据库、凭据、导出文章或 agent 状态。
