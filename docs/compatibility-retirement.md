# Web 与 remote MCP 退役状态

文档状态：2026-07-22。

本地 `wechat-article` 已成为唯一产品。兼容版 `wechat-article-v2.0.0`、签署的 mandatory parity 报告和最终 Web-capable 归档均已发布；Nuxt、Nitro、Cloudflare Pages、Worker MCP/OAuth、远程 CLI 兼容包和 Web Docker 源码已从主线移除。

## 用户影响

- 新安装和正常使用只需要本地二进制；
- 账号搜索、同步、下载等联网功能从本机访问微信，或访问用户显式配置的代理；
- 本地查询、导出、备份、完整性检查可在所需内容已经缓存时离线运行；
- 历史 OAuth token 不会转换为本地微信会话，用户必须通过二维码登录；
- 已在 Web 退役前导出的版本化 ZIP 仍可由 `migration inspect/import/verify` 导入。

## 历史材料

最终 Web-capable tag、源代码归档、sanitized fixtures、schema 与无 secret 回滚说明见 [final-web-capable-release.md](./archive/final-web-capable-release.md)。兼容版发布证据见 [compatibility-release-v2.0.0.md](./release/compatibility-release-v2.0.0.md)，功能对等签署见 [parity-report.md](./release/parity-report.md)。

历史服务恢复不属于正常用户支持路径。若运维应急恢复归档服务，不得修改、降级或导入用户的本地 SQLite；本地数据库继续由当前二进制的兼容窗口管理。

## 云端关闭证据

项目关闭远程资源时必须保留不含 secret 和用户内容的操作收据：关闭前/后资源清单、删除响应、域名负向检查、时间戳、日志保留策略以及 KV/D1 不再保存用户文章或 Credential 的核验。具体证据记录在最终退役运维报告中，而不是在生产配置中保留 binding 或 secret。
