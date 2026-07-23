## Why

当前本地优先产品已具备完整的 Cobra、Bubble Tea 和 stdio MCP 功能，但非技术用户在筛选、批量选择、任务观察和设置管理上仍会更习惯浏览器界面。需要在不恢复线上 Web、远程 OAuth 或多租户服务的前提下，让同一份本地资料库可由浏览器完整操作。

## What Changes

- 增加 `wechat-article web` Cobra 命令：仅在 loopback 地址启动本地 HTTP 服务、输出并可选自动打开本地 URL，进程退出时优雅关闭。
- 将经构建的浏览器静态资源通过 `go:embed` 打入现有 Go 二进制；用户不需要安装 Node、前端运行时、SQLite 或额外服务。
- 新增同源、本地令牌保护的 JSON API 与事件流，复用既有 `application.Application`、`ProfileRuntime` 和持久任务引擎；不得把业务规则复制到 HTTP handler 或前端。
- 提供覆盖当前本地产品功能的浏览器工作台：会话/扫码登录、公众号与文章、专辑、下载与导出、任务与日志、预览、凭据和代理、偏好设置、备份恢复、完整性、垃圾回收和诊断。
- 为路径、文件和危险操作提供本地受控交互：预设目录与服务端校验、上传/下载、一次性确认，而不是允许网页任意读写主机路径。
- **BREAKING** 保持此前“无项目运营的线上 Web 运行时”退役决定不变；`mp.ziikoo.app`、`mptext.ziikoo.app`、公网监听、局域网共享、远程认证和多租户模式均不属于该功能。

## Capabilities

### New Capabilities

- `local-browser-workspace`: 嵌入式本机浏览器 UI、loopback 生命周期与认证、共享应用 API、文件安全边界、完整功能工作流和可访问性。

### Modified Capabilities

- 无。该能力是已退役线上 Web 之外的新本地展示适配器；既有 Cobra、TUI、MCP 的规范行为不改变。

## Impact

- Go：`cli/internal/app/` 增加 `web` 命令和 server 生命周期；新增 `cli/internal/web/` 或等价的仅展示层包；复用 `application`、`library`、`jobs`、`profiles`、`secrets` 既有模块。
- 前端：采用 React + TypeScript + Vite + Astryx + TanStack Query/Table，构建嵌入式静态 SPA、资源指纹和 `go:embed` 流程；最终发布物仍是单个 `wechat-article` 二进制。
- 安全：新增 loopback listener、每启动实例随机高熵令牌、Origin/Host/CSRF 策略、响应安全头、敏感日志脱敏、路径授权及关闭时令牌失效。
- 测试与发布：增加 handler/协议/浏览器 E2E、跨适配器契约、安全负例和 native release receipt 证据；预计二进制体积增加约 1–4 MB（取决于前端资源）。
