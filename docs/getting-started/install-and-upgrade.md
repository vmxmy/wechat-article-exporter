# 安装与升级

`wechat-article` 的正式发行物是 GitHub Release 中的原生压缩包。普通用户不需要安装 Go、Node.js、Docker 或数据库服务。

只有出现 `wechat-article-v*` 稳定标签、目标平台压缩包、SBOM 和 `checksums.txt` 后，才表示对应版本已正式发布。首个完整本地稳定版为 `wechat-article-v2.0.0`。

## 选择发行物

| 平台 | 发行目标 |
| --- | --- |
| macOS Apple Silicon | `darwin_arm64.tar.gz` |
| macOS Intel | `darwin_amd64.tar.gz` |
| Linux x86-64 | `linux_amd64.tar.gz` |
| Linux ARM64 | `linux_arm64.tar.gz` |
| Windows x86-64 | `windows_amd64.zip` |

下载压缩包后，先用同一 Release 的 `checksums.txt` 校验 SHA-256，再解压并把二进制放入用户级 `PATH`。不要从镜像站、网盘或聊天附件安装来源不明的二进制。

## macOS

```bash
grep 'wechat-article_.*_darwin_.*\.tar\.gz' checksums.txt | shasum -a 256 -c -
tar -xzf wechat-article_<version>_darwin_<arch>.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 0755 wechat-article_<version>_darwin_<arch>/wechat-article "$HOME/.local/bin/wechat-article"
wechat-article --version
```

遇到 Gatekeeper 提示时，先重新核对 Release 来源和校验值。不要对未验证文件直接移除隔离属性。

## Linux

```bash
grep 'wechat-article_.*_linux_.*\.tar\.gz' checksums.txt | sha256sum -c -
tar -xzf wechat-article_<version>_linux_<arch>.tar.gz
mkdir -p "$HOME/.local/bin"
install -m 0755 wechat-article_<version>_linux_<arch>/wechat-article "$HOME/.local/bin/wechat-article"
wechat-article --version
```

## Windows PowerShell

```powershell
Get-FileHash .\wechat-article_<version>_windows_<arch>.zip -Algorithm SHA256
Select-String -Path .\checksums.txt -Pattern 'wechat-article_<version>_windows_<arch>.zip'
Expand-Archive .\wechat-article_<version>_windows_<arch>.zip -DestinationPath .\wechat-article
New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\Programs\wechat-article" | Out-Null
Copy-Item .\wechat-article\wechat-article_<version>_windows_<arch>\wechat-article.exe `
  "$env:LOCALAPPDATA\Programs\wechat-article\wechat-article.exe"
& "$env:LOCALAPPDATA\Programs\wechat-article\wechat-article.exe" --version
```

把 `%LOCALAPPDATA%\Programs\wechat-article` 加入用户 `PATH` 后，在新终端中运行。

## 升级

1. 查看候选版本的 release notes、数据库兼容范围和已知差异。
2. 执行 `wechat-article db backup --output <backup.zip>` 并用 `db verify` 验证。
3. 校验并替换二进制，不要删除 profile 数据目录。
4. 执行 `wechat-article status --json`、`wechat-article db integrity`。
5. 若数据库需要升级，程序会在数据库旁保留迁移前快照。

旧二进制不能打开比它更新的 SQLite schema。回滚时不要让旧二进制直接写入新 schema；使用独立 profile 或恢复升级前备份。数据库策略见 [database-compatibility.md](../architecture/database-compatibility.md)。

## 从源码开发

开发者需要 Go 1.25 或更高版本：

```bash
cd cli
go test ./...
go build -o ./bin/wechat-article ./cmd/wechat-article
./bin/wechat-article --version
```
