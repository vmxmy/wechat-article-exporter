# 本地 Go CLI 迁移与 Web 退役策略

本项目正在把 Nuxt Web、Nitro API、Cloudflare D1/KV、远程 MCP/OAuth 和 remote-only Go CLI 合并为一个本地优先的 Go 二进制。目标架构由 Cobra、Bubble Tea 和本地 stdio MCP 三个适配器共享同一套应用模块；正常使用不再依赖 `mp.ziikoo.app` 或 `mptext.ziikoo.app`。

## 不会立即删除 Web

迁移按兼容窗口执行：

1. 先冻结并自动校验功能对等矩阵。
2. 实现本地配置、SQLite、对象存储、任务系统和安全凭据。
3. 实现本地微信扫码登录、公众号发现、文章同步、下载、解析和导出。
4. 实现完整 Bubble Tea 工作区和本地 stdio MCP。
5. 提供浏览器 Dexie 数据导出和 CLI 导入工具。
6. 至少发布一个完整本地版本，同时保留 Web 和远程 MCP 作为迁移与回滚窗口。
7. 只有强制对等项全部通过，才允许删除 Web、Nitro、Cloudflare 和远程 MCP 代码。

## 可执行退役门槛

机器可读矩阵位于 `test/parity/matrix.json`。普通校验检查结构、分类和代码入口：

```bash
npx vite-node --script test/parity/validate.ts
```

退役 gate 会在任一强制能力未通过时失败：

```bash
npx vite-node --script test/parity/validate.ts -- --gate
```

`tasks.md` 中 17.3–17.8 的删除任务不得在 gate 通过前开始。矩阵的 `passed` 必须有对应测试、fixture、产物或人工验收记录，不能只凭代码存在来修改。

## 分类口径

- `mandatory-parity`：本地产品必须保留且验收通过。
- `migration-only`：只为旧数据或旧客户端迁移保留，迁移窗口结束后可删除。
- `intentional-retirement`：Web 托管形态特有能力，不迁移为本地产品功能。
- `dev-only`：开发演示或调试页面，不是产品能力。

托管公共代理监控、Cloudflare D1 多租户同步、Web 内嵌 API 文档、支持/赞助页面和 `pages/dev/*` 属于明确退役项。它们的本地替代分别是本机代理健康、SQLite/备份、Cobra/MCP schema 文档、静态 README 链接和 Go 测试工具。

## 用户数据与凭据

- 浏览器 IndexedDB 不能在未经用户操作的情况下安全、通用地自动读取。
- 最终 Web 兼容版本必须提供版本化导出包，CLI 在本地验证、暂存、导入并生成对账报告。
- 旧 remote-only CLI 的 OAuth token 不会转换为本地微信会话；配置将保留用于回滚，同时引导用户建立 profile 并重新扫码。
- 迁移过程不会上传文章、Cookie、Credential、代理认证或数据库备份。

## 回滚边界

兼容窗口内的回滚只会重新部署已归档的 Web/MCP 版本，不会降级或修改用户的本地数据库。最终退役前必须保存最后一个 Web-capable Git tag、脱敏 fixtures、数据格式说明和运维恢复步骤。
