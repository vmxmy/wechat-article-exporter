# ADR 0001：统一 HTML/script 解析基础设施

## 状态

已接受（2026-08，随 wayfinder 图 [#6](https://github.com/vmxmy/wechat-article-exporter/issues/6) 全部实现落地）。

## 背景

2026-08 微信文章页改版，follow bar 从静态 DOM 改为 script 渲染，`internal/wechat` 依赖单一锚点正则的解析全线失效。当时以三级锚点链热修（commit `308d637`）止血，但暴露出三类结构性问题：

1. **解析面分裂**：`internal/wechat`（discovery 锚点链、`wechat.go` cgiData）与 `internal/processor`（手写 319 行 HTML 树 + 字节扫描 extract）各自维护解析逻辑，同一类漂移要修多处；
2. **错误语义错位**：`wechat.go` 的 cgiData 是单锚点，miss 时把版式漂移误报为登录失效（`Authentication: true`），驱赶用户重新登录；
3. **失效不可见**：锚点链一旦上线，主锚点何时失效、fallback 何时开始兜底，没有任何观测手段——上一次是全链 miss 才发现问题，代价是线上事故。

本 ADR 记录对该事故的架构化根治：以 `golang.org/x/net/html` 为底座的共享解析包、锚点链可观测契约与现网巡检工具。决策链路完整保留在图 [#6](https://github.com/vmxmy/wechat-article-exporter/issues/6) 及其子票中。

## 决策

### 决策一：以 `golang.org/x/net/html` 为解析底座（[#7](https://github.com/vmxmy/wechat-article-exporter/issues/7)）

差异面研究（完整报告见分支 `research/xnethtml-vs-handrolled-tree` 的 [docs/research/xnethtml-vs-handrolled-tree.md](https://github.com/vmxmy/wechat-article-exporter/blob/research/xnethtml-vs-handrolled-tree/docs/research/xnethtml-vs-handrolled-tree.md)）结论为**迁移整体可行、无单点阻断项，但必须分层做**。关键证据：

- **树形状**：x/net 是完整 WHATWG tree construction（foster parenting、adoption agency、注释/doctype 真实节点、svg/math namespace），手写树只有两条局部纠错规则；畸形输入下节点路径系统性不同。用 `ParseFragment` 规避 html/head/body 骨架补全。
- **实体解码是修正而非回归**：手写树用 `html.UnescapeString`（attribute=false 语义）会错误解码 URL 里的 `&copy=` 类参数；x/net 按 spec 的 attribute 特例不解码，与浏览器一致。
- **script 原文保真是硬边界**：x/net 的 Parse 树对 raw text 做 `\r`→`\n` 与 NUL→U+FFFD 转换，且 `html.Node` 无字节偏移，`ProcessError.Offset` 契约无法维持；只有 Tokenizer 层的 `Raw()` 保证 token 原文逐字节分区、可精确重建偏移。**extract 迁移只能走 Tokenizer，不能走树。**
- **有界解析需外层强制**：x/net 仅内置不可配置的 512 open-element 深度硬上限（CVE-2025-47911 修复引入，比本仓库默认 256 宽松），`MaxHTMLNodes` 无对应物，fail-fast 语义必须以 `LimitedReader` + parse 后 walk 校验在外层实现。
- **健壮性权衡**：x/net/html 漏洞史贯穿 2018–2026（无限循环、非线性 DoS、Render XSS、重复属性 sanitizer 绕过等，最新修复在 v0.55.0），但修复响应活跃；而"parser 与浏览器不对齐 → sanitizer 被绕过"这类 bug 恰是零外部审计的手写解析器的固有高危面。

**随之而来的供应链义务**：x/net/html 钉 **≥ v0.55.0**（三个最新漏洞修复所在版本，实际落地 v0.58.0），release 检查纳入 `govulncheck`。

### 决策二：`cli/internal/htmlx` 双层架构（[#8](https://github.com/vmxmy/wechat-article-exporter/issues/8)）

共享解析包 `cli/internal/htmlx`，与 wechat/processor 平级的深模块，零仓库内依赖（只依赖 x/net/html + 标准库，保持 `CGO_ENABLED=0`）。两层各守各的硬边界：

- **树层**：直接暴露 `*html.Node` 加查询辅助（`FindByID`/`FindByClass`/`FindByTag`/`Attr`/`Text`），不做自有 Node 包装——包装层只会复述 x/net 的类型而不增加深度；
- **extract 层**：Tokenizer+`Raw()` 重写（`Document.Scripts()`、`FindBalancedObject`），不经过树——这是字节保真与 offset 的硬边界（决策一第三条证据），顺带修正了朴素扫描对字符串内 `</script>` 的误判；
- **Limits 外层强制**：`htmlx.Limits`（MaxInputBytes/MaxScriptBytes/MaxHTMLDepth/MaxHTMLNodes）由 `Parse` 以 `LimitedReader` + 后置 walk 实现，不依赖 x/net 内置上限；
- **错误边界**：htmlx 只报输入违规（`ErrInputTooLarge`/`ErrTooDeep`/`ErrTooManyNodes`/`ErrScriptTooLarge`）；版式漂移、内容缺失等领域语义由调用方映射——**漂移 ≠ 登录失效**，杜绝 `wechat.go` 把锚点 miss 报成 `Authentication: true` 的旧错。

### 决策三：锚点链——新版式优先、纯返回值、命中可观测（[#8](https://github.com/vmxmy/wechat-article-exporter/issues/8)、[#9](https://github.com/vmxmy/wechat-article-exporter/issues/9)）

核心原则一句话：**fallback 静默兜底必须可见**。

- **链序即版式序**：锚点链新版式锚点在前、旧版式与 script 变量兜底在后（如 `article_account_name` 链为 `js_name` → `wx_follow_nickname` → `nickname-var`），主锚点失效时链仍能产出结果，但这一事实不允许悄无声息；
- **纯返回值**：`Chain.Resolve(d)` 返回 `(value, anchorName, matched)`，htmlx 内不嵌任何 Recorder，上报是调用方的事（[#8](https://github.com/vmxmy/wechat-article-exporter/issues/8) 定案）；
- **命中上报**（[#9](https://github.com/vmxmy/wechat-article-exporter/issues/9) 定案）：SQLite 聚合表 `anchor_stats`（migration 010，STRICT，每解析面 × 锚点一行 `hit_count`+`last_hit_at`，行数有界无 GC）；`application.AnchorRecorder` 接口由 library 实现为 UPSERT，**best-effort**——上报失败只记 debug 日志，绝不影响解析结果；只记标识符与计数，零隐私面；
- **漂移判定**：`diagnostics status` 输出锚点命中数据表；主锚点 `last_hit_at` 落后于任一后位锚点，或主锚点 `hit_count=0` 而后位有命中 → 标 ⚠️ 疑似版式漂移（`--json` 含 `driftSuspected`）。全链 miss 本就是响亮的协议错误，不入表。

### 决策四：现网巡检为开发者工具，不进 CI（[#14](https://github.com/vmxmy/wechat-article-exporter/issues/14)）

- **形态**：`go run ./tools/patrol`（`cli/tools/patrol` 独立 main 包），不进产品二进制、天然不进 CI，复用 htmlx 与 wechat 的链定义；
- **链接源**：本地文件（每行一条 URL），`-urls` 显式指定，**不进 repo**——阅读列表属敏感信息，repo 只写格式说明；
- **信号**：任一 URL 的主锚点未命中 → exit 1；报告输出每 URL（脱敏为序号）× 每锚点命中表 + 汇总；
- **人工回流**：开 issue、重采语料由人工决定。实测已见过上游单次返回无锚点变体页（重试即恢复），自动开 issue 只会制造噪声——**"单次失败 ≠ 漂移"** 写进报告头；
- **覆盖面**：只巡检公开文章页解析面（`wechat.article_account_name`）；home 侧解析面需登录会话，不属于无凭据巡检。

## 后果

### 迁移实录（[#10](https://github.com/vmxmy/wechat-article-exporter/issues/10)、[#11](https://github.com/vmxmy/wechat-article-exporter/issues/11)）

- **wechat 全迁**（commit `3534413`）：discovery 三级链 + cgiData 三条链全部走 htmlx；主页身份锚点 miss 从登录失效改为协议错误；中途修正新增 `ByScriptVarRaw`——URL/ID 值不做实体解码，规避 `&copy=` 腐蚀，人类文本值走 `ByScriptVar` 解码。
- **processor 全迁**（commits `314d216`+`6512ae4`）：extract 走 Tokenizer、render/resources 走 `*html.Node`，**手写树 319 行删除**。
- **golden 的实际漂移远小于研究预估**：41 页语义/样本 golden **零重生成**（净化后元素按属性名排序构造，保持了退役序列化器的确定性输出）；exporter 两个 digest 经新旧双树逐字节 diff 确认**仅一处插入一个自闭合斜杠字节**（`html.Render` 对 void 元素写 `/`，HTML5 语义等价）后重生成。
- **fuzz 移植的意外收获**：种子 `<img srC="0 onerror=">` 迁移后失败——旧手写解析器从不小写化属性键，`srC` 查 `src` 落空、恶意值被巧合丢弃；x/net 按规范小写化后该值存活进输出，揪出被旧解析器掩盖的净化缺陷。修复落在正确高度：`normalizeURL` 拒绝含裸空白/引号/尖括号的值。两个 fuzz 目标（种子 + 各 15s 实跑，40 万+ 执行）全过。

### 正面

- 三处解析面共享一套锚点链与树查询，版式漂移只需在一处响应；
- 解析行为与浏览器对齐（WHATWG spec 实现），sanitizer 绕过风险显著降低；
- fallback 兜底从静默变为可观测：`diagnostics status` 随时暴露主锚点是否已失效；
- 巡检工具让"发现漂移"从被动事故变为主动例行。

### 负面 / 持续义务

- 新增 `golang.org/x/net` 供应链项：钉 ≥ v0.55.0、release 加 `govulncheck`、跟随其活跃的 CVE 修复节奏；
- limits 语义从建树期 fail-fast 降级为 parse 后校验（内存由 `MaxInputBytes` 兜底）；
- 锚点链新增或调整时须同步解析面标识符（`anchor_stats` 聚合粒度）与 diagnostics 链序；
- `samples/` 语料刷新策略尚未定（图 [#6](https://github.com/vmxmy/wechat-article-exporter/issues/6) "Not yet specified"），待巡检报告积累后具体化。
