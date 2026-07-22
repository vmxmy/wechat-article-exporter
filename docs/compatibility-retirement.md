# Web 与 remote MCP 兼容退役计划

文档状态：2026-07-22。Nuxt Web、Nitro APIs 和 remote MCP/OAuth 当前仍在兼容期内，供旧用户导出浏览器数据和紧急回滚。兼容版 `wechat-article-v2.0.0` 已发布；最终 Web-capable 归档、云资源关闭和源码退役尚未完成。

## 门禁

退役只有在以下条件全部满足后才能开始：

1. 发布至少一个包含完整本地功能和迁移工具的稳定 `wechat-article-v*` 兼容版；
2. mandatory parity matrix 全绿并由发布负责人签署；
3. Web 本地导出、CLI import/verify 和旧 remote 配置迁移均可用；
4. 最后一个 Web-capable tag、sanitized fixtures、schema 和回滚操作手册已归档；
5. 用户已获得明确、带日期的迁移窗口和远程 OAuth 截止公告。

当前 parity 报告见 [parity-report.md](./release/parity-report.md)，兼容版见 [GitHub Release](https://github.com/vmxmy/wechat-article-exporter/releases/tag/wechat-article-v2.0.0)。最终 Web-capable 归档完成前，不删除 Web、Nitro、Worker、Cloudflare binding 或 secret。

## 公告时间线

计划最早退役日期为 2026-12-31，但它是门禁约束下的最早日期，不是无条件承诺。若 compatibility release、签署或迁移窗口晚于计划，日期必须向后调整并重新公告。

## 用户迁移

1. 在兼容期 Web 的设置/迁移页面导出版本化 ZIP；该过程只读取浏览器 IndexedDB，不上传内容。
2. 本地执行 `wechat-article migration inspect <archive.zip>`。
3. 创建并选择 profile，执行 `migration import`，使用命令要求的精确确认值。
4. 执行 `migration verify <archive.zip>`，核对记录、对象和缺失资源。
5. 扫码建立新的本地微信会话。remote OAuth token 不会变成本地微信凭据。
6. 在本地完成同步、下载、导出和备份 smoke 后，再删除浏览器站点数据。

## remote OAuth grace period

Worker 支持可选 `REMOTE_OAUTH_DISABLE_AFTER`。在真正公告截止日期前不配置该值，remote OAuth 保持兼容行为；截止后 `/authorize` 返回 HTTP 410 和本地迁移 URL。不得在仓库中写入虚构的生产截止时间。

## 归档与回滚

最后一个 Web-capable release 必须保留可复现源码、sanitized fixtures、Web archive schema、Cloudflare binding 清单和不包含 secret 的部署步骤。兼容期内的紧急回滚只恢复已归档 Web/MCP 服务，不修改或降级用户本地数据库。

归档内容、历史基础设施清单和无 secret 回滚步骤见 [final-web-capable-release.md](./archive/final-web-capable-release.md)。

## 最终关闭

关闭阶段包括停止新授权、让旧客户端收到迁移响应、按政策到期 OAuth material、缩短并清理日志、删除 Pages/Worker/KV/D1 bindings 与 secret、验证无用户文章或凭据超期保留，最后才从生产代码移除 Web 和 Worker。
