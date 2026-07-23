# 本地数据与安全

`wechat-article` 默认把用户数据保存在本机。项目运营的 Web、remote MCP、KV 和 D1 已退役；网络流量直接访问微信，或访问用户显式配置的代理。

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
- 历史 remote OAuth token 不被当前 CLI 读取，也不会导入本地微信会话。

加密 vault 必须显式初始化。passphrase 不支持 `--passphrase` 明文参数，避免进入 shell history；交互式命令使用隐藏输入，自动化使用仅当前用户可读的文件：

```bash
chmod 600 ./vault-passphrase
wechat-article vault init --passphrase-file ./vault-passphrase
WECHAT_ARTICLE_SECRET_BACKEND=vault \
WECHAT_ARTICLE_VAULT_PASSPHRASE_FILE=./vault-passphrase \
  wechat-article status --json
```

兼容 CI 的 `WECHAT_ARTICLE_VAULT_PASSPHRASE` 环境变量仍可解锁，但更容易被进程环境或诊断工具观察，优先使用 passphrase file。

平台凭据库的发布 smoke 是测试专用且默认关闭。只有同时使用 `integration` build tag 和 `WECHAT_ARTICLE_KEYRING_INTEGRATION=1` 才会创建唯一命名的临时凭据，完成 Set/Get/Delete 后清理；测试值不会写入日志。原生 release runner 必须输出 `KEYRING_SMOKE_PASS`；仅 Linux runner 没有 Secret Service 时允许输出带 `reason=credential-service-unavailable` 的 `KEYRING_SMOKE_SKIP` receipt：

```bash
cd cli
WECHAT_ARTICLE_KEYRING_INTEGRATION=1 \
  go test -tags=integration -count=1 -v \
  -run '^TestPlatformKeyringIntegration$' ./internal/secrets
```

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
wechat-article diagnostics bundle --output ./diagnostics.zip
```

诊断 ZIP 权限为 `0600`，默认只包含系统元数据、脱敏配置、schema 版本、最近任务和完整性结果；正文、会话、Credential、代理 authorization 与 vault 均不进入归档。命令拒绝覆盖已有文件。

## PDF

PDF 使用本地 Chrome、Chromium、Edge 或 Brave 渲染自包含 HTML，不会上传到远程 PDF 服务。没有兼容浏览器时，PDF 会返回依赖错误，其他格式不受影响。
