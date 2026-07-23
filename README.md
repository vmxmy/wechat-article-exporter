<p align="center">
  <img src="./assets/logo.svg" alt="wechat-article-exporter logo">
</p>

# wechat-article

`wechat-article` 是本项目唯一的产品形态：一个 Go 编写的本地优先二进制，用于登录微信公众号后台、同步和保存文章、下载正文与资源，并导出 HTML、Markdown、TXT、JSON、XLSX、DOCX 和 PDF。

同一套应用核心提供四种本地入口：

- Cobra 命令行，适合脚本、批处理和 CI；
- Bubble Tea TUI，适合终端交互；
- 本地 stdio MCP，适合 Claude、Codex、Cursor 等客户端。
- 本地浏览器工作区，适合在同机浏览器中查看资料库、任务和受控导出。

项目不再提供 Nuxt Web、Nitro API、远程 MCP、远程 OAuth、Cloudflare KV/D1 或托管 PDF 服务。文章与索引保存在本机 SQLite 和内容寻址对象存储中；会话、文章 Credential 与代理授权保存在操作系统凭据库或用户显式初始化的加密 vault 中。

## 安装

普通用户不需要 Go。请从 [GitHub Releases](https://github.com/vmxmy/wechat-article-exporter/releases) 下载对应平台的压缩包，并使用同一 Release 中的 `checksums.txt` 校验 SHA-256。

| 系统 | 架构 | 资产 |
| --- | --- | --- |
| macOS | Apple Silicon | `wechat-article_<version>_darwin_arm64.tar.gz` |
| macOS | Intel | `wechat-article_<version>_darwin_amd64.tar.gz` |
| Linux | x86-64 | `wechat-article_<version>_linux_amd64.tar.gz` |
| Linux | ARM64 | `wechat-article_<version>_linux_arm64.tar.gz` |
| Windows | x86-64 | `wechat-article_<version>_windows_amd64.zip` |

macOS / Linux 示例：

```bash
tar -xzf wechat-article_<version>_<os>_<arch>.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 0755 wechat-article_<version>_<os>_<arch>/wechat-article "$HOME/.local/bin/wechat-article"
wechat-article --version
```

Windows PowerShell 示例：

```powershell
Expand-Archive .\wechat-article_<version>_windows_amd64.zip -DestinationPath .\wechat-article
New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\Programs\wechat-article" | Out-Null
Copy-Item .\wechat-article\wechat-article_<version>_windows_amd64\wechat-article.exe "$env:LOCALAPPDATA\Programs\wechat-article\wechat-article.exe"
```

完整校验、安装、升级和数据库兼容策略见 [安装与升级](./docs/getting-started/install-and-upgrade.md)。

## 快速开始

```bash
wechat-article profile create work
wechat-article profile use work
wechat-article login --qr-output ./wechat-login.png
wechat-article account search '公众号名称' --json
wechat-article sync account ACCOUNT_ID --follow
wechat-article download article --url 'https://mp.weixin.qq.com/s/ARTICLE' --follow
wechat-article export start \
  --url 'https://mp.weixin.qq.com/s/ARTICLE' \
  --format html \
  --output ./exports \
  --follow
```

在 TTY 中不带子命令运行会进入完整工作区：

```bash
wechat-article
```

也可以启动仅本机可访问的浏览器工作区：

```bash
wechat-article web
```

它固定绑定随机 `127.0.0.1` 端口，stdout 输出一次性地址；打开后地址栏会移除 token。不会监听局域网或公网，也不需要 Node.js、前端开发服务器或远程服务。详细的 profile 共享、目录 token、上传限制、无障碍、语言切换与排错说明见[本地浏览器工作区](./docs/guides/browser-workspace.md)。

自动化调用增加 `--json` 后，stdout 只包含一个版本化 JSON 文档；进度和诊断写 stderr。退出码：成功 `0`，运行错误 `1`，用法错误 `2`。

## 本地数据与安全

每个 profile 隔离配置、数据库、对象、任务、会话和 Credential。以实际状态输出为准：

```bash
wechat-article status --json
wechat-article db status --json
```

主要文件：

- profile 配置：`<config>/profiles/<profile-id>/config.json`
- SQLite：`<data>/profiles/<profile-id>/library.sqlite3`
- 对象：`<data>/profiles/<profile-id>/objects/`
- 状态：`<state>/profiles/<profile-id>/`

不要手工复制运行中的 SQLite/WAL 文件；使用内置备份、校验和恢复：

```bash
wechat-article db backup --output ./backups/work.zip
wechat-article db verify ./backups/work.zip
wechat-article db integrity
wechat-article db restore ./backups/work.zip \
  --conflict refuse \
  --confirm 'restore-backup:./backups/work.zip'
```

当系统凭据库不可用时，可显式初始化加密 vault。passphrase 不作为命令行参数传递；自动化使用权限为 `0600` 的文件，交互使用隐藏输入：

```bash
wechat-article vault init --passphrase-file ./vault-passphrase
export WECHAT_ARTICLE_SECRET_BACKEND=vault
export WECHAT_ARTICLE_VAULT_PASSPHRASE_FILE=./vault-passphrase
wechat-article vault verify --passphrase-file ./vault-passphrase
```

文章 Credential 可通过 JSON、环境变量或终端隐藏输入导入；`credential validate` 会在敏感路由策略下直接验证并更新状态：

```bash
wechat-article credential import --interactive
wechat-article credential validate CREDENTIAL_ID --json
```

HTML 导出支持 `--html-resource-policy strict|best-effort` 与 `--html-batch-archive articles.zip`。每次 terminal export 都记录 fenced provenance generation；可离线校验输出：

```bash
wechat-article export verify \
  --root ./exports \
  --manifest export-EXPORT_ID-manifest.json \
  --json
```

校验失败返回退出码 `1`，唯一 JSON error envelope 的 `data` 包含 affected article IDs、expected/actual checksum 或 size。TUI 导出操作必须选择稳定 export ID；任务与导出进度会自动刷新。SQLite leased scheduler permits 在 detached workers 和多个 CLI 进程之间共同执行 global、operation、host 与 sensitive 并发限制。

默认网络路径是本地进程直连微信。代理必须由用户显式配置；包含 Cookie、Credential、评论、指标或付费授权的请求只能直连或通过用户明确确认的 `credential-trusted` 代理。详见 [本地数据与安全](./docs/operations/local-data-security.md) 和 [备份恢复](./docs/operations/backup-restore.md)。

浏览器工作区与 Cobra、TUI、MCP 共享当前 profile，但不会接收任意主机路径：导出通过默认根或其下子目录的 opaque token 完成，生成产物只能以 opaque artifact capability 流式下载；打开某个导出输出目录必须输入该导出的精确确认值。账号页支持下载、上传并导入单个受限 JSON manifest；Credential 在页面中只写入不回显，也可上传单个受限 JSON 文件直接导入。维护入口已接入备份创建、验证和一次性 ZIP 下载、完整性、GC 计划/确认执行、诊断与诊断包下载，以及单个恢复归档的上传、私有 staging、prepare 和 commit。恢复上传最多为 2 GiB，工作区同一时刻只保留一个归档；页面只拿到不透明 handle，commit 需要该 prepare 返回的一次性精确确认值。成功恢复后本地浏览器服务器会关闭，须重新运行 `wechat-article web`。这不是任意主机路径或通用文件访问 API；恢复所选的备份归档走现有 restore archive staging，而浏览器生成的备份只能通过一次性下载取得。批量导出仍使用其他本地入口。当前逐项支持状态见[浏览器能力矩阵](./docs/release/browser-capability-matrix.md)。

## PDF

PDF 使用本机已安装的 Google Chrome、Chromium、Microsoft Edge 或 Brave 渲染，不上传文章。没有可用浏览器时，PDF 返回可操作的依赖错误，其他格式不受影响：

```bash
wechat-article diagnostics status --json
wechat-article diagnostics bundle --output ./diagnostics.zip
```

## 本地 stdio MCP

```bash
wechat-article mcp serve --transport stdio
```

该命令不监听 TCP，不需要 OAuth，直接使用启动时的 active profile。stdout 专用于 JSON-RPC/MCP，日志只写 stderr。建议为自动化客户端使用独立 profile，并配置 `readOnly` 和最小 allow-list。客户端配置见 [CLI、TUI 与 stdio MCP](./docs/guides/cli-tui-mcp.md)。

## 旧 Web 数据迁移

最后一个 Web-capable 版本已经归档。若你在退役前导出了版本化 ZIP，可继续完全离线导入：

```bash
wechat-article migration inspect ./legacy.zip
wechat-article migration import ./legacy.zip \
  --confirm 'import-legacy:./legacy.zip'
wechat-article migration verify ./legacy.zip
```

浏览器 IndexedDB 无法由 CLI 静默读取；没有事先导出的 ZIP 时，请保留原浏览器 profile，并参考 [迁移说明](./docs/migration/local-cli-transition.md) 和 [最终 Web 归档](./docs/archive/final-web-capable-release.md)。历史 OAuth token 不会成为本地微信会话，必须重新扫码。

## 从源码开发

需要 Go 1.25 或更高版本：

```bash
cd cli
go test ./...
go test -race ./...
go vet ./...
go build -o ./bin/wechat-article ./cmd/wechat-article
./bin/wechat-article --version
```

贡献流程见 [CONTRIBUTING.md](./CONTRIBUTING.md)。

[GitHub stars]: https://img.shields.io/github/stars/wechat-article/wechat-article-exporter
[GitHub forks]: https://img.shields.io/github/forks/wechat-article/wechat-article-exporter
[GitHub License]: https://img.shields.io/github/license/wechat-article/wechat-article-exporter
