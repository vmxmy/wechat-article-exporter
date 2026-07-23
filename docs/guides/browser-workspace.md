# 本地浏览器工作区

`wechat-article web` 在当前 active profile 上启动浏览器工作区。它是与 Cobra、Bubble Tea TUI 和 stdio MCP 并列的第四个本地适配器，不是恢复旧的托管 Web 产品。

```bash
wechat-article profile use work
wechat-article web
```

命令会在 stdout 输出一个一次性的 `http://127.0.0.1:<随机端口>/?token=...` 地址，并默认尝试打开系统浏览器。自动化或远程终端中使用 `--no-open`；打开地址后，令牌会被换成本进程内存中的 HttpOnly session，浏览器地址栏会移除令牌。不要把首次地址复制到工单、截图、shell history 或聊天记录中。

```bash
wechat-article web --no-open
```

`web` 持续运行到按下 `Ctrl-C` 或命令上下文结束。它不支持 `--json`，因为 stdout 只保留给可复制的本地地址；运行日志写 stderr。

## 本地性与隐私

工作区固定绑定随机 IPv4 loopback 地址 `127.0.0.1`，不提供 host 或 port 参数，不能监听 `0.0.0.0`、局域网或公网地址，也不支持手机/其他电脑访问。每次启动都会生成新的高熵 bootstrap token；关闭后该 token 和所有浏览器 session 都会失效。

浏览器只请求同源的本地 `/api/v1`；它不会加载 CDN、远程字体、analytics、项目运营的 Web 服务或 remote OAuth。响应设置 CSP、`Referrer-Policy: no-referrer`、`X-Content-Type-Options: nosniff`、禁止 frame 与敏感响应禁缓存。变更操作要求有效本地 session、精确 loopback Host、同源 Origin 和 CSRF token。

当前 profile 仍是数据与权限边界：配置、SQLite、对象、任务、微信会话和 Credential 都与 Cobra、TUI、MCP 共享。启动工作区前可用以下命令确认所选 profile 和本机路径：

```bash
wechat-article status --json
wechat-article profile list
```

切换 active profile 后，重启浏览器工作区；已打开的工作区不会自动换到另一个 profile。多个本地适配器可以访问同一 profile，持久任务和 SQLite lease 是唯一事实来源。

## 目录、下载与上传

浏览器不能把任意主机绝对路径交给服务端。导出页先授权 profile 的导出根（已设置时使用该根，否则使用本机默认 Downloads export 根），再可创建其下的子目录。页面只获得不透明 directory token，不会显示或接收完整主机路径；服务端仍会执行输出根、路径穿越与 symlink 逃逸检查。

导出是持久任务。选择文章、格式和已授权目录后，工作区会返回 job ID；在 Jobs 或 Exports 中查看进度、manifest 和离线校验结果。生成的文件只能通过 manifest 中的不透明 artifact capability 流式下载，不接受路径参数；打开所选导出的输出目录也必须输入该导出的精确确认值。浏览器不会接收任意主机路径。

凭据字段在浏览器中是仅写入的：导入后会清空，列表只显示安全元数据。维护页面已接入备份创建和验证（使用不透明 backup ID）、完整性检查、GC 的计划/一次性确认执行、诊断读取，以及以不透明一次性 handle 下载的脱敏诊断包。恢复归档的上传/staging 流程仍未在当前浏览器工作区交付；请使用经确认的本地 CLI：

```bash
wechat-article db restore ./backups/work.zip \
  --conflict refuse \
  --confirm 'restore-backup:./backups/work.zip'
```

不要上传真实 session、Cookie、vault passphrase 或未脱敏的文章归档到 issue、截图或第三方文件服务。

## 可访问性与语言

浏览器工作区提供 English 与简体中文。右上角的语言切换会保存在浏览器本地首选项；Settings 的 **Display language** 会同时保存 profile 的 `display.language`（仅允许 `en` 或 `zh-CN`），供 TUI 和新的工作区会话复用。

所有主导航、表格选择、表单、确认和任务控制都应可用键盘操作：使用 `Tab`/`Shift+Tab` 移动焦点，`Enter` 或 `Space` 触发聚焦控件。页面为控件提供可访问名称、可见焦点状态和状态/错误 live announcement；窄窗口会改为单列布局。遇到焦点丢失、读屏未宣告或键盘无法完成某项操作，请通过 `wechat-article diagnostics bundle --output ./diagnostics.zip` 收集脱敏诊断后报告问题。

## 排错

| 现象 | 处理 |
| --- | --- |
| 浏览器没有自动打开 | 使用 stdout 输出的地址手动打开；或下次使用 `wechat-article web --no-open`。 |
| 地址显示 401 | 一次性地址已经用过、工作区已关闭或 session 已过期；重新运行 `wechat-article web` 获取新地址。 |
| 其他设备无法连接 | 这是预期安全边界；工作区只允许同机 `127.0.0.1`。 |
| 页面无法加载或 API 不可用 | 保持 `web` 进程运行，确认地址仍是 `127.0.0.1`，再运行 `wechat-article status --json` 与 `wechat-article diagnostics status --json`。 |
| 看不到另一入口创建的内容/任务 | 确认两个入口使用同一 active profile；切换 profile 后需要重启浏览器工作区。 |
| 导出目录不可用 | 从导出页重新授权默认目录或创建其下子目录；不要尝试粘贴绝对路径。 |
| 恢复归档或任意文件上传不可用 | 这是当前已知限制，使用 Cobra 的 `db restore` 等受确认命令。 |

## 当前浏览器范围

工作区已经覆盖本地 session、账号/文章/专辑的分页查询、账号搜索与同步、单 URL 导入、保存的文章查询、受限本地预览、带边界的作业详情与允许的控制、受控目录导出、导出 manifest/verification、opaque artifact 下载、精确确认后的输出目录打开、凭据/代理/安全偏好、备份创建/验证、完整性、GC 和诊断（包括 opaque diagnostic bundle 下载）。完整逐项对照见 [能力矩阵](../release/browser-capability-matrix.md)。

未交付的浏览器能力不会被标记为 parity：恢复归档上传、任意主机文件选择和批量导出仍使用 Cobra 或 TUI。浏览器的 artifact 下载和输出目录打开受到 opaque capability 与精确确认的约束，不能成为任意文件或目录访问接口。文章预览及文章/元数据/评论/资源下载、单专辑遍历和批量下载会复用同一套持久作业；stdio MCP 则有意不提供 GUI、浏览器 session 或网络监听，只通过 stdio 和 profile policy 面向自动化。
