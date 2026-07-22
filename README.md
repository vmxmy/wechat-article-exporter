<p align="center">
  <img src="./assets/logo.svg" alt="wechat-article-exporter logo">
</p>

# wechat-article-exporter

![GitHub stars]
![GitHub forks]
![GitHub License]

`wechat-article` 是本项目的新主产品：一个本地优先的原生二进制，用于登录微信公众号后台、同步文章、保存本地资料库，并导出 HTML、Markdown、TXT、JSON、Excel、DOCX 和 PDF。

正常的本地工作流由同一套应用模块驱动，并通过三种入口使用：

- Cobra 命令行，适合脚本、批处理和 CI；
- Bubble Tea TUI，适合在终端中交互操作；
- 本地 stdio MCP，适合 Claude、Codex、Cursor 等客户端调用。

文章元数据保存在本机 SQLite 中，正文与资源进入本地对象存储，登录会话和文章 Credential 使用操作系统凭据库。正常工作流不依赖本项目运营的 Web、Cloudflare KV/D1 或远程 MCP 服务。

## 发布与兼容期状态

> 文档状态日期：**2026-07-22**。首个完整本地兼容版 [`wechat-article-v2.0.0`] 已发布，包含 5 个原生平台压缩包、对应 CycloneDX SBOM 和 `checksums.txt`。现有 `v2.3.x` Web 版本与本地二进制版本号相互独立。

Web 和 remote MCP 当前仍保留在兼容期内，供旧用户继续使用、导出数据和回滚。它们已进入弃用流程，**计划最早退役日期为 2026-12-31**；退役还必须同时满足：

1. 已发布至少一个完整本地稳定版；
2. 强制功能对等矩阵全部通过；
3. 浏览器本地数据导出与 CLI 导入/核验路径可用；
4. 最后一个 Web-capable 版本和运维回滚材料已归档。

若任一门槛未满足，退役日期只能向后调整，并通过新的带日期公告通知。兼容期说明、迁移步骤和回滚边界见 [Web 与 remote MCP 兼容退役计划](./docs/compatibility-retirement.md)。

## 安装

正式 Release 安装不要求 Go、Node.js、Docker 或数据库服务。请只从项目 [GitHub Releases] 下载，并在安装前核对 `checksums.txt`。

支持的发布目标：

| 系统 | 架构 | 计划中的资产名 |
| --- | --- | --- |
| macOS | Apple Silicon | `wechat-article_<version>_darwin_arm64.tar.gz` |
| macOS | Intel | `wechat-article_<version>_darwin_amd64.tar.gz` |
| Linux | x86-64 | `wechat-article_<version>_linux_amd64.tar.gz` |
| Linux | ARM64 | `wechat-article_<version>_linux_arm64.tar.gz` |
| Windows | x86-64 | `wechat-article_<version>_windows_amd64.zip` |

### macOS

1. 在 Release 页面下载与处理器匹配的 `.tar.gz` 和 `checksums.txt`。
2. 校验 SHA-256，解压并安装到个人可执行目录：

```bash
grep 'wechat-article_.*_darwin_.*\.tar\.gz' checksums.txt | shasum -a 256 -c -
tar -xzf wechat-article_<version>_darwin_<arch>.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 0755 wechat-article_<version>_darwin_<arch>/wechat-article "$HOME/.local/bin/wechat-article"
wechat-article --version
```

如果 `$HOME/.local/bin` 不在 `PATH`，请把它加入 shell 配置后重新打开终端。遇到 Gatekeeper 拦截时，先再次确认下载来源和校验值；不要对来源不明的文件直接移除隔离属性。

### Linux

```bash
grep 'wechat-article_.*_linux_.*\.tar\.gz' checksums.txt | sha256sum -c -
tar -xzf wechat-article_<version>_linux_<arch>.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 0755 wechat-article_<version>_linux_<arch>/wechat-article "$HOME/.local/bin/wechat-article"
wechat-article --version
```

### Windows PowerShell

```powershell
Get-FileHash .\wechat-article_<version>_windows_amd64.zip -Algorithm SHA256
Select-String -Path .\checksums.txt -Pattern 'wechat-article_<version>_windows_amd64.zip'
Expand-Archive .\wechat-article_<version>_windows_amd64.zip -DestinationPath .\wechat-article
New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\Programs\wechat-article" | Out-Null
Copy-Item .\wechat-article\wechat-article_<version>_windows_amd64\wechat-article.exe "$env:LOCALAPPDATA\Programs\wechat-article\wechat-article.exe"
& "$env:LOCALAPPDATA\Programs\wechat-article\wechat-article.exe" --version
```

把 `%LOCALAPPDATA%\Programs\wechat-article` 加入用户 `PATH` 后，可在新终端中直接运行 `wechat-article`。完整安装、校验、升级和回滚步骤见 [安装与升级](./docs/getting-started/install-and-upgrade.md)。

## 快速开始

首次运行会创建一个本地 `default` profile。也可以主动创建隔离的工作 profile：

```bash
wechat-article profile create work
wechat-article profile use work
wechat-article status
```

本地扫码登录并下载、导出一篇文章：

```bash
wechat-article login --qr-output ./wechat-login.png
wechat-article download article --url 'https://mp.weixin.qq.com/s/ARTICLE' --follow
wechat-article export start \
  --url 'https://mp.weixin.qq.com/s/ARTICLE' \
  --format html \
  --output ./exports \
  --follow
```

不带参数并在 TTY 中运行会进入 TUI：

```bash
wechat-article
```

自动化调用可增加 `--json`。stdout 只输出一个机器可读结果，进度和诊断写入 stderr：

```bash
wechat-article article list --account ACCOUNT_ID --limit 50 --json
```

退出码约定：成功为 `0`，运行错误为 `1`，命令用法错误为 `2`。

## 核心能力

- 本地微信扫码登录、会话状态与退出；
- 公众号搜索、保存、导入导出和文章同步；
- 文章、合集、本地过滤、分页与排序；
- 正文、图片及其他资源下载，支持持久化任务、恢复、取消和重试；
- 评论、回复、阅读量等数据的本地 Credential 管理；
- HTML、Markdown、TXT、JSON、XLSX、DOCX、PDF 导出；
- SQLite 与内容寻址对象存储、完整性检查、备份、恢复和安全 GC；
- 直接连接或显式配置的受信任 URL-wrapper 代理；
- Cobra、Bubble Tea TUI 和本地 stdio MCP 共用同一 profile 与资料库。

## 本地数据与 profile

每个 profile 隔离自己的配置、SQLite 数据库、对象、任务、会话和 Credential。当前生效路径以以下命令输出为准：

```bash
wechat-article status --json
wechat-article db status --json
```

默认根目录如下；`<profile-id>` 是 profile 的稳定 ID：

| 系统 | 配置 | 数据 | 缓存 | 状态 |
| --- | --- | --- | --- | --- |
| macOS | `~/Library/Application Support/wechat-article-exporter` | `~/.local/share/wechat-article-exporter` | `~/Library/Caches/wechat-article-exporter` | `~/.local/state/wechat-article-exporter` |
| Linux | `${XDG_CONFIG_HOME:-~/.config}/wechat-article-exporter` | `${XDG_DATA_HOME:-~/.local/share}/wechat-article-exporter` | `${XDG_CACHE_HOME:-~/.cache}/wechat-article-exporter` | `${XDG_STATE_HOME:-~/.local/state}/wechat-article-exporter` |
| Windows | `%AppData%\wechat-article-exporter` | `%AppData%\wechat-article-exporter` | `%LocalAppData%\wechat-article-exporter` | `%AppData%\wechat-article-exporter` |

主要文件位于：

- profile 配置：`<config>/profiles/<profile-id>/config.json`
- SQLite：`<data>/profiles/<profile-id>/library.sqlite3`
- 对象：`<data>/profiles/<profile-id>/objects/`
- profile 状态：`<state>/profiles/<profile-id>/`

不要在程序运行时手工复制 SQLite/WAL 文件。需要迁移或归档时使用内置备份命令。更完整的数据、权限和 profile 说明见 [本地数据与安全](./docs/operations/local-data-security.md)。

## 安全与 Credential

- 微信会话、文章 Credential 和代理授权默认写入操作系统凭据库，不写入 SQLite，也不会出现在普通 status、JSON 输出或备份包中。
- 备份包包含资料库、对象和 profile 配置，但不包含操作系统凭据库中的 secret；恢复后可能需要重新扫码或重新导入 Credential。
- 调试日志、诊断和错误会脱敏，但仍应在分享前人工检查。
- 不要把 Cookie、`key`、`pass_ticket`、`appmsg_token`、OAuth token 或代理授权放入 issue、截图、shell history、版本库或云同步目录。
- `credential import --file` 的输入文件应使用仅当前用户可读的权限，导入成功后安全删除。
- 删除 profile、Credential、任务、恢复备份和授予代理敏感权限等操作要求精确确认值。

查看不含 secret 的 Credential 元数据：

```bash
wechat-article credential status --json
```

## 网络路径与受信任代理

默认网络路径是：

```text
wechat-article 本地进程 → mp.weixin.qq.com / 微信资源域名
```

项目运营域名不在正常本地路径中。只有显式运行 `wechat-article legacy ...` 才会进入兼容期 remote OAuth/MCP 路径。

代理必须由用户显式添加。默认 `public-only` 代理只能处理公开正文和公开资源；评论、指标、管理会话、付费内容等敏感请求只能直连，或经过用户明确标记并精确确认的 `credential-trusted` 代理。

```bash
wechat-article proxy add public-cache \
  --endpoint 'https://proxy.example/wrap' \
  --trust public-only \
  --classes public_content,public_resource
wechat-article proxy test public-cache
wechat-article proxy list --json
```

授予 `credential-trusted` 意味着代理可能收到 Cookie、文章 Credential 或其他微信会话数据。只对你管理、审计并信任的 HTTPS 服务使用该级别。详细威胁模型见 [本地数据与安全](./docs/operations/local-data-security.md)。

## 备份、恢复与回滚

```bash
wechat-article db backup --output ./backups/work-2026-07-22.zip
wechat-article db verify ./backups/work-2026-07-22.zip
wechat-article db integrity
```

恢复会替换当前 active profile 的资料库，因此必须先验证并提供精确确认值：

```bash
wechat-article db restore ./backups/work-2026-07-22.zip \
  --conflict refuse \
  --confirm 'restore-backup:./backups/work-2026-07-22.zip'
```

升级前先备份。若新版本触发数据库迁移，程序还会在数据库旁创建迁移前快照。二进制回滚不得用旧程序直接打开超出其支持范围的新 schema；优先修复升级，或恢复升级前备份到独立 profile。详见 [备份、恢复与升级回滚](./docs/operations/backup-restore.md)。

## PDF 浏览器要求

PDF 在本机使用 Chromium 系浏览器渲染，不会把文章发送到远程 PDF 服务。支持自动发现：

- Google Chrome
- Chromium
- Microsoft Edge
- Brave

如果系统没有可用浏览器，其他导出格式仍可使用，PDF 会明确失败。通过以下命令检查发现结果：

```bash
wechat-article diagnostics status --json
```

## 本地 stdio MCP

启动命令：

```bash
wechat-article mcp serve --transport stdio
```

该进程不监听 TCP 端口，不需要 remote OAuth，直接使用启动时的 active profile。MCP stdout 专用于协议消息，日志只写 stderr。切换 profile 后应重启 MCP 客户端中的 server 进程。

建议自动化客户端为专用 profile 配置 `readOnly: true` 和最小 allow-list。Claude、Codex、Cursor 配置示例及工具策略见 [CLI、TUI 与 stdio MCP](./docs/guides/cli-tui-mcp.md)。

## 从 Web 或旧 remote CLI 迁移

- 浏览器 IndexedDB 不能由 CLI 静默、可靠地自动读取，必须由用户在兼容期 Web 中显式导出版本化归档，再由本地 CLI 验证、暂存、导入并对账。
- 在 CLI 导入命令和 Web 导出入口随兼容版正式提供前，不要删除浏览器站点数据；当前文档不声称这条导入路径已经发布。
- 旧 remote-only CLI 配置会保留用于兼容期回滚，但其中 OAuth token 不会复制为本地微信会话。迁移时创建/选择本地 profile，并重新扫码登录。
- 本地备份、文章数据、Cookie、Credential 和代理 secret 不需要上传到项目服务完成迁移。

完整迁移顺序见 [本地 CLI 迁移指南](./docs/migration/local-cli-transition.md)。

## 从源码开发

普通用户不需要 Go。仅开发者从源码构建时需要 Go 1.25 或更高版本：

```bash
cd cli
go test ./...
go build -o ./bin/wechat-article ./cmd/wechat-article
```

Nuxt 兼容期 Web 的开发命令和仓库约定见 [AGENTS.md](./AGENTS.md)。CLI 的完整命令参考见 [cli/README.md](./cli/README.md)。

## 许可与使用声明

MIT，详见 [LICENSE](./LICENSE)。

本程序不会把你的公众号登录状态作为公共账号池。通过本程序获取的公众号文章内容版权归原作者所有，请在法律、平台规则和授权范围内合理使用。

## Star 历史

[![Star History Chart]][Star History Chart Link]

<!-- Definitions -->

[GitHub stars]: https://img.shields.io/github/stars/wechat-article/wechat-article-exporter?style=social&label=Star
[GitHub forks]: https://img.shields.io/github/forks/wechat-article/wechat-article-exporter?style=social&label=Fork
[GitHub License]: https://img.shields.io/github/license/wechat-article/wechat-article-exporter?label=License
[GitHub Releases]: https://github.com/wechat-article/wechat-article-exporter/releases
[`wechat-article-v2.0.0`]: https://github.com/vmxmy/wechat-article-exporter/releases/tag/wechat-article-v2.0.0
[Star History Chart]: https://api.star-history.com/svg?repos=wechat-article/wechat-article-exporter&type=Timeline
[Star History Chart Link]: https://star-history.com/#wechat-article/wechat-article-exporter&Timeline
