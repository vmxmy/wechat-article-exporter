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

本机浏览器工作区使用当前 active profile，并且只绑定随机 IPv4 loopback 端口：

```bash
./bin/wechat-article web
./bin/wechat-article web --no-open
```

stdout 只输出一次性 `127.0.0.1` 地址，不能与 `--json` 合用。首次打开会建立 HttpOnly 本地 session 并清除地址栏 token；关闭进程即失效。它不提供 LAN/public host 选项、任意主机路径或项目运营的网络服务。使用边界、目录 token、上传/恢复限制、可访问性及中英文切换见[本地浏览器工作区](../docs/guides/browser-workspace.md)。

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
./bin/wechat-article export start --account ACCOUNT_ID --format html --html-resource-policy strict --html-batch-archive articles.zip --output ./exports --follow
./bin/wechat-article export verify --root ./exports --manifest export-EXPORT_ID-manifest.json --json
```

`--json` 保证 stdout 为一个版本化 JSON 文档。成功、运行错误、用法错误退出码分别为 `0`、`1`、`2`。
导出校验不通过时退出码为 `1`，唯一的 JSON error envelope 在 `data` 中携带完整校验报告。HTML 支持 strict/best-effort 本地资源策略和 portable batch ZIP。

Bubble Tea 的文章页支持 compound filter、saved query、多选和 resolved-count 确认；任务和导出页自动刷新。导出 manifest/verify/open 必须选择稳定 export ID，不会回退到最新记录，并展示完整 provenance generation/state/error。detached worker 通过 SQLite leased permits 跨进程共享并发上限；评论回复部分失败会持久化为 `partial`，retry 只继续未完成 thread。

无可用系统凭据库时，显式初始化并验证加密 vault；passphrase file 必须仅当前用户可读：

```bash
chmod 600 ./vault-passphrase
./bin/wechat-article vault init --passphrase-file ./vault-passphrase
WECHAT_ARTICLE_SECRET_BACKEND=vault \
WECHAT_ARTICLE_VAULT_PASSPHRASE_FILE=./vault-passphrase \
  ./bin/wechat-article status --json
```

Credential 支持 JSON、环境变量和隐藏终端输入：

```bash
./bin/wechat-article credential import --interactive
./bin/wechat-article credential validate CREDENTIAL_ID --json
./bin/wechat-article diagnostics bundle --output ./diagnostics.zip
```

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

`exports.start` 的输出目录必须位于 active profile 数据目录、`preferences.export.root` 或 profile 配置的 `mcp.allowedOutputRoots` 绝对路径内；路径穿越与 symlink 逃逸会被拒绝。

浏览器的文件模型不同：它只可授权默认 export root 或其下子目录，并把不透明 directory token 交回服务端；不会获得绝对路径。浏览器支持受控导出、manifest/verification、以 opaque artifact capability 流式下载生成文件，以及输入精确确认值后打开所选导出的输出目录；还支持文章/资源/评论下载和专辑遍历/批量下载。账号 manifest 可下载，也可上传一个受限 JSON 文件到私有 staging 后导入；Credential 可上传一个受限 JSON 文件并直接导入，文件名和 secret 不会回显。维护页面已接入备份创建、验证和一次性 ZIP 下载、完整性、GC 计划/确认执行、诊断与 opaque diagnostic bundle 下载，以及单个 restore archive 的上传、私有 staging、prepare 和 commit。restore 上传限制为 2 GiB，工作区同时只接纳一个归档，页面只会收到不透明 staging handle；prepare 返回的精确确认值只能用于一次 commit。commit 成功后服务器关闭，重新启动 `web` 再继续操作。浏览器仍不提供任意主机路径或通用文件上传 API：用户为恢复选择的备份归档走既有 restore archive staging，浏览器生成的备份则只能一次性下载；批量导出也仍使用 Cobra/TUI/MCP。完整对照见[浏览器能力矩阵](../docs/release/browser-capability-matrix.md)。

## 测试与发布

```bash
gofmt -w $(find . -name '*.go' -type f)
go test ./...
go test -race ./...
go vet ./...
go run honnef.co/go/tools/cmd/staticcheck@v0.6.1 -checks='SA*,S1*,QF*' ./...
go test -count=1 ./internal/corpus
go test -count=1 -run '^TestTrackedSamplesHaveGoldenOrReviewedExclusion$' ./internal/processor
```

原生系统凭据库 smoke 使用 `integration` build tag 且必须显式 opt-in；默认测试不会访问真实凭据服务，也不会输出测试 secret：

```bash
WECHAT_ARTICLE_KEYRING_INTEGRATION=1 \
  go test -tags=integration -count=1 -v \
  -run '^TestPlatformKeyringIntegration$' ./internal/secrets
```

测试日志提供 `KEYRING_SMOKE_PASS` 或 `KEYRING_SMOKE_SKIP` receipt。发布工作流在 macOS、Linux 和 Windows 原生 runner 上执行该命令；macOS/Windows 必须完成 Set/Get/Delete round trip，Linux runner 没有 Secret Service 时允许带 `reason=credential-service-unavailable` 的明确 skip。

`Release Go CLI` 工作流构建 macOS arm64/amd64、Linux arm64/amd64、Windows amd64 的 `CGO_ENABLED=0` 压缩包、CycloneDX SBOM 和 `checksums.txt`。
