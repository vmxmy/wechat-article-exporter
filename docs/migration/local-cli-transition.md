# 从旧 Web / remote CLI 迁移到本地 Go CLI

项目已完成从 Nuxt/Nitro/Cloudflare/remote OAuth 架构到本地 Go 二进制的迁移。主线不再包含可部署的旧 Web 或 Worker 服务；历史实现位于最终 Web-capable tag 和归档中。

## 已有 Web 导出 ZIP

如果你在服务退役前使用迁移页面生成了版本化 ZIP：

```bash
wechat-article migration inspect ./legacy.zip
wechat-article profile create imported
wechat-article profile use imported
wechat-article migration import ./legacy.zip \
  --confirm 'import-legacy:./legacy.zip'
wechat-article migration verify ./legacy.zip
```

`inspect` 在写入前验证 schema、manifest、记录和对象；`import` 使用 staging、对象去重和冲突策略；`verify` 比较源 manifest 与本地记录/对象计数和校验值。导入不会上传文章、资源、Cookie、Credential 或备份。

## 没有 Web 导出 ZIP

浏览器 IndexedDB 不能由本地 CLI 安全、通用地静默读取。若没有事先导出的 ZIP：

1. 不要清理仍保存旧站点数据的浏览器 profile；
2. 使用最后一个 Web-capable 归档在隔离环境中恢复仅供本地导出的历史前端；
3. 按 [最终 Web 归档](../archive/final-web-capable-release.md) 的 immutable source、binding 清单和无 secret 回滚边界操作；
4. 生成 ZIP 后立即回到当前 CLI 完成本地 inspect/import/verify；
5. 不要把历史 Web 数据部署到公共多租户环境。

历史恢复是数据救援流程，不是恢复项目运营服务的授权。

## 旧 remote CLI 配置

远程 MCP/OAuth 命令和兼容包已经移除。旧配置文件可以离线归档用于审计，但其 server、access token、refresh token 或 OAuth client 信息都不会被当前 CLI 读取或导入。

```bash
wechat-article profile create local
wechat-article profile use local
wechat-article login --qr-output ./wechat-login.png
```

重新扫码后，会话保存在该 profile 的本地 secret store。不要把旧 OAuth token 复制到 profile 配置、SQLite 或 Credential 导入文件。

## 迁移后验证

```bash
wechat-article status --json
wechat-article db status --json
wechat-article db integrity
wechat-article article list --limit 20 --json
wechat-article db backup --output ./backups/imported.zip
wechat-article db verify ./backups/imported.zip
```

再完成一项联网同步、一篇正文及资源下载、所有所需格式导出和离线复查后，才删除旧浏览器 profile 或历史配置。PDF 还需分别验证“本机浏览器存在”和“浏览器缺失时返回明确依赖错误”两条路径。

## 回滚边界

二进制升级回滚只遵守本地数据库兼容策略。历史 Web/MCP 归档即使被应急恢复，也不得修改或降级本地数据库。升级前使用 `db backup`；旧二进制遇到更新 schema 时必须拒绝写入。
