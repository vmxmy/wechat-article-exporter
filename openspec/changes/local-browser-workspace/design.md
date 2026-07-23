## Context

当前产品是单个本地 Go 二进制：Cobra、Bubble Tea 和 stdio MCP 都通过同一 `application.Application`、profile-scoped SQLite、对象存储与持久任务工作。历史 Nuxt/Nitro/Cloudflare 产品已经退役，且仓库规则明确禁止重新引入项目运营的网络服务。

本变更新增的是第四个本地展示适配器，而不是“恢复 Web 端”。它必须能与 TUI、Cobra、MCP 同时读取同一个 profile，并可观察由 detached worker 执行的任务。浏览器自身不能安全地获得任意主机文件路径，因此导出、备份、恢复及凭据文件需要不同于 TUI 的交互设计。

规模估算以一名熟悉 Go 与前端的工程师为基准：

| 阶段 | 范围 | 估算 |
| --- | --- | --- |
| P0 | server、令牌安全、嵌入式壳、只读仪表盘和任务观察 | 2–3 人周 |
| P1 | 登录、公众号/文章/专辑、筛选、下载、任务控制、导出 | 4–6 人周 |
| P2 | 设置、凭据/代理、备份恢复、诊断、文件策略、全部 E2E 与发布验证 | 4–6 人周 |
| 合计 | 生产可用的完整功能浏览器工作台 | **10–15 人周** |

若两名工程师并行（一人 API/runtime，一人 SPA），日历时间通常为 6–9 周；仍需要 1–2 周联调、真实浏览器测试和跨平台发布验证。MVP 不是完整替代，约 2–3 人周；不应把它误报为完整功能交付。

## Goals / Non-Goals

**Goals:**

- 从一个本地二进制启动仅限本机访问的浏览器 UI，并自动复用当前 profile、SQLite、对象库、会话和持久任务。
- 在浏览器中完成当前 TUI/Cobra 的用户级工作流，而非只提供只读文章列表。
- 维持单一业务核心、SQLite 的跨进程一致性、任务可恢复性和既有 secret/network policy。
- 让本地 Web 访问默认 fail-closed：不能被局域网、恶意网页或本机其他进程无令牌调用。
- 保持发布端不需要 Node、SQLite CLI、数据库服务或云基础设施。

**Non-Goals:**

- 不恢复 `mp.ziikoo.app`、`mptext.ziikoo.app`、Nuxt/Nitro/Cloudflare、远程 OAuth、远程 MCP、多租户或同步到云端。
- 不监听 `0.0.0.0`、IPv6 wildcard 或用户指定的非 loopback 地址；不支持局域网共享。
- 不从浏览器任意选择/写入主机绝对路径，也不以 Web UI 绕过现有确认、导出授权、凭据和网络策略。
- 第一版不实现 PWA 离线缓存、浏览器扩展、远程手机控制或实时多用户协作。

## Decisions

### 1. 一个核心，四个本地适配器

```text
Cobra ───────┐
Bubble Tea ──┼── application / ProfileRuntime ── SQLite + objects + jobs
stdio MCP ───┤
Local Web ───┘
```

HTTP handler 只负责 HTTP 输入校验、认证、映射与返回序列化。前端只负责呈现状态、表单和轮询/事件消费。所有登录、同步、下载、导出、任务、备份和恢复继续调用共享 use case 或现有 presentation-safe extension；任何新增领域能力先落到 application seam。

替代方案“前端直接读 SQLite”会绕开迁移、事务、密钥和任务规则，拒绝采用。替代方案“浏览器调用 stdio MCP”会不必要地引入协议桥和授权歧义，也拒绝采用。

### 2. `web` 生命周期与网络边界

`wechat-article web` 使用 `net.Listen("tcp4", "127.0.0.1:0")` 取得随机端口，只公布形如 `http://127.0.0.1:<port>/?token=<opaque>` 的 URL。服务拒绝非精确 loopback Host，token 使用 `crypto/rand` 生成、只保存在进程内存；初始导航后将 token 换成 HttpOnly 本地会话 cookie 并通过 `history.replaceState` 去除 URL token。

API 对不带有效会话的请求返回 401；所有 state-changing 请求还必须验证同源 Origin 与 CSRF header。响应设置 CSP、`X-Content-Type-Options: nosniff`、`Referrer-Policy: no-referrer` 和禁止缓存的敏感响应头。日志绝不打印 token、cookie、QR 数据、凭据或完整敏感 URL。

命令默认自动调用已有本地浏览器打开能力，可提供 `--no-open`；stdout 只输出可复制的 URL，运行日志走 stderr。`SIGINT`、context 取消、server error 和命令退出都执行有界 `Shutdown` 并使令牌失效。第一期不提供 host/port 自定义 flag。

### 3. 前端构建并嵌入 Go：React + Vite + Astryx + TanStack Query/Table

前端栈已确定为 **React + TypeScript + Vite + Astryx + TanStack Query + TanStack Table**。`webui/` 使用锁定包管理器与可重复构建脚本生成 hashed 静态资源，Go 使用 `//go:embed` 服务资源；最终用户只获取 Go release archive，不需要 Node 或任何前端运行时。

Astryx 负责设计 token、主题、可访问基础组件和一致的管理工作台视觉。应用入口必须配置 `ThemeProvider`、`LinkProvider`，并按 `reset → astryx → theme` 的 CSS layer 顺序导入其预构建 CSS。TanStack Query 管理本地 API 的缓存、轮询、变更失效和任务状态；TanStack Table 管理服务端分页、排序、列显示和多选，但不得把整库文章加载到浏览器。React Router 或等价轻量客户端路由仅管理本地 SPA 路由。

实施时必须先通过 Astryx CLI 查询模板及每个实际使用组件的 dense contract；优先复用组件，只有现有组件无法满足时才 swizzle。不得复制已退役 Nuxt 源，也不得复用线上依赖、analytics 或远端字体/CDN。

### 4. API 形态和任务观察

API 版本为 `/api/v1`。读取请求使用明确分页、限制、过滤和稳定 JSON schema；变更请求返回共享 domain result 或 job ID。长任务仅创建或控制持久任务，不在 HTTP request 生命周期内执行。浏览器使用 SSE（首选）或受限轮询读取 job/event snapshot；断线重连后从 SQLite 重新查询，SSE 不作为状态事实来源。

初始资源分区：runtime/session、accounts、articles/query/saved queries、albums、jobs/logs、exports/preview、credentials/proxies/preferences、storage/backup/restore/integrity/gc、diagnostics。每个 endpoint 都要有与 Cobra/TUI/MCP 的 cross-adapter contract test。

### 5. 文件与危险操作的本地安全模型

浏览器从服务获得受控 directory token，而不是任意绝对路径。默认可选导出根为配置的 `export.root` 或 `~/Downloads/wechat-article-exports`；用户可通过 API 请求服务端创建/验证的子目录，现有 `normalizeExportOutputRoot`、输出授权和路径 traversal 规则仍为最终裁决。

导入或恢复使用浏览器 multipart upload，写入私有 staging 目录后交给既有验证/restore use case；文件下载通过单次授权的 streamed response。凭据导入不回显 secret。删除、恢复、GC、代理信任和 credential 操作沿用 exact confirmation，并在 API 中明示范围与可恢复性。

### 6. 完整功能的交付切片

“完整”表示能完成当前用户可用 TUI/Cobra 工作流，不表示每个 CLI flag 都必须出现在页面。按风险排序：

1. server 安全壳、状态、登录、账户/文章/任务只读；
2. 搜索、同步、单篇导入、筛选、预览、下载、任务控制；
3. 专辑、全格式导出、manifest、输出目录、浏览器本地 HTML；
4. 凭据、代理、偏好设置（含中英文）、备份/恢复、完整性/GC、诊断；
5. 可访问性、国际化、E2E、发布/receipt 与文档。

## Risks / Trade-offs

- [本机网页被其他本机进程或恶意页面访问] → 随机 loopback 端口、内存 token、Host/Origin/CSRF 三重校验、短会话和敏感响应头。
- [浏览器文件系统权限与 TUI 不同] → server-side directory token/staging/streaming，所有最终路径仍走现有授权。
- [HTTP API 与三个既有 adapter 漂移] → application-first endpoint、契约测试和能力矩阵；禁止 handler 内复制业务规则。
- [大文章库/任务日志导致 UI 慢] → SQLite 分页、严格请求上限、虚拟列表、SSE 只推增量提示、完整详情按需加载。
- [React/Astryx/TanStack 令二进制膨胀或供应链复杂] → 锁定依赖、最小导入、禁用远端资源、构建可重现性、资源预算和嵌入产物检查；组件必须由 Astryx contract 驱动而非随意引入。
- [QR 和 session 等敏感状态泄漏到网页历史/日志] → 首跳 token cookie 化并清 URL、禁 referrer、敏感字段从 JSON/log 中剔除、专门负例测试。
- [完整范围被低估] → 明确 P0/P1/P2 门槛；任何未实现的工作流不得宣传为完整替代。

## Migration Plan

1. 先以 feature branch/实验命令交付 P0，不改变默认无子命令进入 TUI 的行为。
2. 在 CI 建立嵌入资源、API contract、loopback security 和 Playwright/浏览器 E2E 基线。
3. 逐切片发布，`web` 命令在 stable release 前保持明确 beta 标签；同一 profile 中 TUI/Cobra/MCP 必须持续可用。
4. 全部 capability matrix 通过后，更新安装、隐私、故障排除和 agent/MCP 文档，将浏览器工作台标为稳定。
5. 回滚只需停止使用 `web` 命令或降级二进制；数据库/API 不引入仅供 Web 的不可逆 schema。若前端嵌入资源发生问题，既有本地 adapter 仍可完整操作资料库。

## Open Questions

- 目录选择器是否需要 OS native dialog helper，还是先限定预设根与手工创建的安全子目录；默认采用后者。
- SSE 是否在首版承载 QR/任务刷新，或仅做轮询后再增量引入；默认先保证 1 秒轮询正确，再增加 SSE。
- 是否允许同一用户启动多个 `web` 实例；默认允许独立只读/控制会话，但数据库、job lease 和 profile lock 是唯一事实来源。
