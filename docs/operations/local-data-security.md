# 本地数据与安全

`wechat-article` 默认把用户数据保存在本机。正常功能不依赖 `mp.ziikoo.app`、`mptext.ziikoo.app`、Cloudflare KV 或 D1；网络流量直接访问微信，或访问用户显式配置的代理。

## Profile 隔离

每个 profile 隔离配置、SQLite、对象、任务、偏好、微信会话和文章 Credential。以 `wechat-article status --json` 输出的实际路径为准。

```bash
wechat-article profile create work
wechat-article profile use work
wechat-article status --json
```

平台默认根目录遵循 macOS Application Support/Caches、Linux XDG 和 Windows AppData/LocalAppData 约定。便携模式只有在显式指定并验证根目录时启用。

## Secret

- 微信会话、文章 Credential 和代理 authorization 存入操作系统凭据库。
- Linux 无可用 Secret Service 时，必须显式初始化并解锁加密 vault；不会静默回退到明文 JSON。
- SQLite、普通配置、日志、诊断包和默认备份不保存 secret 字节。
- 旧 remote OAuth token 只为兼容期回滚保留，不会导入本地微信会话。

不要把 Cookie、`key`、`pass_ticket`、`appmsg_token`、authorization 或 OAuth token 放入 issue、截图、shell history、版本库和云同步目录。

## 网络与代理信任

默认是 direct-first：

```text
wechat-article 本地进程 → 微信域名
```

公开文章和公开资源可使用 `public-only` 代理。Cookie、评论、指标、付费内容等敏感请求只能直连，或使用用户明确标记并确认的 `credential-trusted` 代理。该授权意味着代理可能接触微信会话或文章 Credential，应只授予你运营、审计和信任的 HTTPS 服务。

```bash
wechat-article proxy add cache \
  --endpoint https://proxy.example/wrap \
  --trust public-only \
  --classes public_content,public_resource
wechat-article proxy test cache
wechat-article proxy list --json
```

## 日志与诊断

调试日志会包含请求 ID、重试和路由决策，但 cookies、token、authorization 和敏感查询参数会统一脱敏。诊断包默认排除正文和 secret；分享前仍应人工检查。

```bash
wechat-article diagnostics status --json
```

## PDF

PDF 使用本地 Chrome、Chromium、Edge 或 Brave 渲染自包含 HTML，不会上传到远程 PDF 服务。没有兼容浏览器时，PDF 会返回依赖错误，其他格式不受影响。
