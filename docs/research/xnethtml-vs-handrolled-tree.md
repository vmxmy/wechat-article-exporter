# `golang.org/x/net/html` vs 手写 HTML 树的行为差异面

研究 ticket：[#7](https://github.com/vmxmy/wechat-article-exporter/issues/7)（图：[#6](https://github.com/vmxmy/wechat-article-exporter/issues/6)）。
调查日期：2026-08-13。对照的 x/net/html 版本：pkg.go.dev 当前发布 v0.58.0 与 golang/net `master` 源码。

## 对照基线（本仓库）

手写实现全部在 `cli/internal/processor/`：

- `html_tree.go` — `parseHTMLFragment` 单遍线性扫描建树：只识别 element/text/document 三种节点；`<!-- -->`、`<!...>`、`<?...>` 直接跳过不建节点；只有两条局部纠错规则（`p` 内遇 block 元素闭合 `p`、`li` 内遇 `li` 闭合前一个）；`script`/`style` 内容按 raw 子串 `</` + tag 截断，text 原样保留；text 与属性值经 std `html.UnescapeString` 解码；`serializeHTMLNode` 输出属性按 key 排序、空值省略 `=""`、void 元素无 `/`、所有 text（含 style 的 CSS 文本）经 `html.EscapeString` 转义。
- `extract.go` — `scanScripts`/`findTagEnd`/`findBalancedObject`/`findPayloadCandidate` 直接在原始 `[]byte` 上扫描，所有 `ProcessError.Offset` 都是**原输入的字节偏移**。
- `limits.go` — `MaxHTMLDepth`（默认 256）、`MaxHTMLNodes`（默认 250 000）在建树过程中即时生效，超限立即返回 `ErrorLimit`。
- `fuzz_test.go` — 端到端 fuzz：`Process` 不 panic、`Render` 输出不含可执行内容。

x/net/html 自述实现的就是 WHATWG HTML 解析算法：godoc 明言 "It implements the HTML5 parsing algorithm (https://html.spec.whatwg.org/multipage/syntax.html#tree-construction), which is very complicated."（[pkg.go.dev/golang.org/x/net/html#Parse](https://pkg.go.dev/golang.org/x/net/html#Parse)）。

---

## 1. 树形状（tree construction）

### 差异

| 行为 | x/net/html | 手写树 |
|---|---|---|
| 文档骨架 | `Parse` 按 WHATWG tree construction 自动补全 `html`/`head`/`body`；godoc 示例：单独一个 text 节点 round-trip 后变成含 `<html><head><body>` 的树（[Render godoc](https://pkg.go.dev/golang.org/x/net/html#Render)；[render.go L39-44](https://github.com/golang/net/blob/master/html/render.go)） | 不补全，document 节点下即输入内容 |
| Fragment | `ParseFragment(r, context)` 以 context 元素（如 `body`/`div`）做 innerHTML 解析，返回子节点列表，不补骨架，但仍执行全部重排规则（[godoc](https://pkg.go.dev/golang.org/x/net/html#ParseFragment)） | `parseHTMLFragment` 天然就是 fragment 语义 |
| 表格畸形嵌套 | foster parenting：table 内的非法子节点被移出到 table 之前（parse.go `fosterParent`，注释标注 WHATWG "foster parenting" 节；godoc 示例 `"<p><table><a>"` 中 `<a>` 被 reparent 到 table 的父节点）（[parse.go L254-256](https://github.com/golang/net/blob/master/html/parse.go)；[WHATWG §13.2.6.1 foster parenting](https://html.spec.whatwg.org/multipage/parsing.html#foster-parent)） | 无任何重排，`<table><a>` 中 `a` 留在 `table` 里 |
| 格式化元素错嵌套 | 完整 adoption agency 算法（`"<b><i></b></i>"` 拆分重排）（[parse.go L1250](https://github.com/golang/net/blob/master/html/parse.go)；[WHATWG adoption agency](https://html.spec.whatwg.org/multipage/parsing.html#adoption-agency-algorithm)） | 无；结束标签只沿祖先链找同名元素弹栈 |
| 隐式闭合 | 全套规则：`p` 在 button scope 内被大量块级开始标签闭合、`li`/`dd`/`dt`/`option` 等互相闭合、`</p>` 无 open `p` 时**创建**空 `p`（[WHATWG §13.2.6.4.7 "in body"](https://html.spec.whatwg.org/multipage/parsing.html#parsing-main-inbody)） | 只有 `p`+block、`li`+`li` 两条 |
| 注释 | 保留为 `CommentNode`（[node.go](https://github.com/golang/net/blob/master/html/node.go)），bogus comment（`<?...>`、`<!...>` 非 doctype）也按 spec 变成 CommentNode | 全部丢弃，不占节点 |
| doctype | `DoctypeNode`，Render 时还原 | 跳过 |
| 显式标签被丢弃 | godoc 明言 "explicit `<tag>`s in r's data can be silently dropped, with no corresponding node in the resulting tree"（[Parse godoc](https://pkg.go.dev/golang.org/x/net/html#Parse)），如 body 内再出现的 `<html>`/`<body>` 只合并属性（parse.go `copyAttributes`） | 未知标签一律保留为普通元素 |
| foreign content | `svg`/`math` 有 namespace、integration point、breakout 规则（parse.go `parseForeignContent`） | 无 namespace 概念，`svg` 与 `div` 同等对待 |
| 重复属性 | tokenizer 层丢弃后出现的同名属性（首个生效），2026-03 起为修 CVE-2026-27136 加入，对应 WHATWG §13.2.5.33（[commit "html: ignore duplicate attributes during tokenization"](https://go-review.googlesource.com/c/net/+/781685)；[token.go readTag](https://github.com/golang/net/blob/master/html/token.go)） | 相同：`parseHTMLTag` 首个生效 |

### 对 API / 迁移的影响

- `render.go`（sanitize 遍历）、`resources.go`（资源发现）都假设"输入什么形状树就是什么形状"。换 x/net 后，畸形输入（微信正文里常见未闭合 `<td>`、`<section>` 嵌套滥用、svg 音频卡片）会产生**不同的节点路径与兄弟顺序**，sanitize 白名单逻辑本身兼容（按 tag 判断），但输出结构会变。
- 应使用 `ParseFragment`（context=`body` 或 `div`）而非 `Parse`，否则每篇文章都被包上 `html/head/body` 骨架。
- 注释节点开始存在于树中：sanitize 遍历必须显式处理 `CommentNode`（丢弃），否则 Render 会把注释带进输出。

### 建议

用 `ParseFragmentWithOptions(r, bodyContext)` 做等价入口；sanitize 的 switch 增加 `CommentNode`/`DoctypeNode` 丢弃分支；针对 fixture 里真实微信文章跑 A/B 树 diff，重点验证 table 与 svg 卡片的资源定位不回归。

---

## 2. 实体解码（时机与范围）

### 差异

x/net 在 **tokenizer 层**解码，且区分 text 与 attribute 两种上下文：

- text token：`Tokenizer.Text()` 对非 raw 文本调用 `unescape(s, false)`（[token.go L1200-1218](https://github.com/golang/net/blob/master/html/token.go)）。
- attribute 值：`TagAttr()` 调用 `unescape(convertNewlines(val), true)`（token.go L1247）。`attribute=true` 触发 WHATWG 命名引用的特例：命名实体**无分号**且后一字符是 `=` 或字母数字时不解码（[escape.go L150](https://github.com/golang/net/blob/master/html/escape.go)，实现 [WHATWG named character reference state](https://html.spec.whatwg.org/multipage/parsing.html#named-character-reference-state) 的 "historical reasons" 条款）。即 `<a href="x?a=1&copy=2">` 中 `&copy` **不**被解码。
- 数字引用做 Windows-1252 兼容映射（`0x80-0x9F` 映射表）、`NUL`/代理区/超界码点替换为 `U+FFFD`（escape.go L17-52、L120-126，注释直接引用 [WHATWG consume-a-character-reference](https://html.spec.whatwg.org/multipage/syntax.html#consume-a-character-reference)）。
- 所有 text/attr 值统一做 `\r`、`\r\n` → `\n` 归一化（token.go `convertNewlines` L1163-1193）。

手写树对 text 与属性值一律用 std `html.UnescapeString`（等价 `unescape(s, attribute=false)` 语义，见 [escape.go UnescapeString](https://github.com/golang/net/blob/master/html/escape.go)）：

- **无 attribute 特例**：`&copy=2` 会被解码成 `©=2`，与浏览器/x/net 行为不一致。
- **无换行归一化**：`\r\n` 原样保留。
- 解码时机同为建树期（`html_tree.go` L62、L234），`htmlNodeText` 只是拼接已解码 text，这一点两边等价。

### 对 API / 迁移的影响

- URL 属性里含裸 `&copy=`、`&reg=` 之类 query 参数时，手写树会错误解码，x/net 不会——迁移后**资源 URL 的取值可能变化**，`resources.go` 的 URL 归一化与 golden 需要复核。
- text 中 `\r\n` 消失会影响 `Text`/`Markdown` 输出的 golden。

### 建议

把这一条视为**修正而非回归**（x/net 行为与 WHATWG/浏览器一致）；迁移时用 fixture 全量 diff 确认只有实体/换行类差异，并同步更新 golden。

---

## 3. `<script>`/`<style>` raw text 保真（迁移可行性关键）

### 差异

- x/net tokenizer 对 `script` 走完整 script data 状态机（`readScript`，含 `<!--<script>` double-escaped 状态，[token.go L394+](https://github.com/golang/net/blob/master/html/token.go)），`style` 走 RAWTEXT（`readRawOrRCDATA`）。raw text **不做实体解码**（`textIsRaw=true` 时跳过 `unescape`，token.go L1212-1214），这一点与手写一致。
- 但 raw text **不是逐字节保真**：`Text()` 无条件做 `convertNewlines`（`\r`/`\r\n`→`\n`），且 raw text token 的 `convertNUL=true` 会把 `NUL`→`U+FFFD`（token.go L1053-1056、L1208-1211）。
- 结束标签判定：x/net `readRawEndTag` 要求 `</script` 之后必须是空白、`/` 或 `>`（token.go L367-387）；手写 `parseHTMLFragment`/`scanScripts` 只找子串 `</script`，因此 `</scriptxyz>` 会被手写实现当成闭合而提前截断，x/net 不会。`<script>` 内含 `<!--<script>...</script>-->` 时 x/net 按 spec 延迟闭合，手写在第一个 `</script` 处截断。
- **字节偏移**：`html.Node` 不携带任何位置信息；只有 tokenizer 层保证 "The token stream's raw bytes partition the byte stream"，可用 `Tokenizer.Raw()` 自行累计偏移拿到未修改的原文（[token.go Raw() L1152-1161](https://github.com/golang/net/blob/master/html/token.go)）。
- 手写 `extract.go` 在原始 `[]byte` 上工作：script 原文逐字节保真，`ProcessError.Offset` 是精确输入偏移。

### 对 API / 迁移的影响

- 若 `scanScripts`/`findBalancedObject` 改从 **x/net 解析树**取 script 文本：payload 原文经过 `\r`→`\n` 与 NUL 替换，且丢失输入偏移——`ProcessError.Offset` 契约（错误定位到原输入）无法维持。JSON 语义大概率不受换行归一化影响，但不再是逐字节保真。
- 若改用 **Tokenizer + `Raw()`**：原文可逐字节取回、偏移可精确重建，等价替换 `scanScripts`，还免费获得正确的 `</scriptxyz>`/double-escape 语义。

### 建议

迁移 extract 层时**只考虑 Tokenizer + `Raw()` 方案，不要走 Parse 树**；或者保持 `extract.go` 现状字节扫描不动、仅迁移建树/渲染侧（两者本就解耦——`extractPayload` 与 `parseHTMLFragment` 互不依赖）。

---

## 4. 有界解析（limits.go 语义能否对等表达）

### 差异

| limits.go 语义 | x/net/html 对应物 |
|---|---|
| `MaxInputBytes` | 无内置；可用 `io.LimitedReader` 外包（Parse 接受 `io.Reader`） |
| `MaxHTMLDepth`（默认 256，建树中即时报错） | 内置**不可配置**的 512 open-element-stack 硬上限，超限 panic 后被 `parse()` 的 recover 转成 error（[parse.go insertOpenElement L235-240、parse() L2223-2228](https://github.com/golang/net/blob/master/html/parse.go)；godoc："Parse will reject HTML that is nested deeper than 512 elements."）。该上限是 2025-10 为修 CVE-2025-47911 加入的，commit 明言"未来可能通过 ParseOption 做成可配置"，目前没有（[commit "html: impose open element stack size limit"](https://go-review.googlesource.com/c/net/+/709876)） |
| `MaxHTMLNodes`（默认 250 000，建树中即时报错） | **无对应机制**；只能 parse 完成后遍历统计——内存已经消耗 |
| `MaxScriptBytes` 等单块上限 | `Tokenizer.SetMaxBuf(n)` 限制单 token 缓冲，超限返回 `ErrBufferExceeded`（[token.go L276-285、L1276-1280](https://github.com/golang/net/blob/master/html/token.go)；[godoc SetMaxBuf](https://pkg.go.dev/golang.org/x/net/html#Tokenizer.SetMaxBuf)）。但 `Parse`/`ParseFragment` 内部自建 tokenizer，`ParseOption` 只有 `ParseOptionEnableScripting`，**无法注入 SetMaxBuf**（parse.go L2274-2285） |

### 对 API / 迁移的影响

- `limits.go` 的"超限即 fail-fast、不产生超限内存"语义**无法在 `html.Parse` 内部表达**，必须外层包一层：输入侧 `LimitedReader`（`MaxInputBytes` 已能兜住最坏内存：节点数 O(输入字节)），parse 后 walk 校验 depth/节点数以维持现有错误分类。
- 内置 512 深度比本仓库默认 256 宽松：仅靠 x/net 会放过 depth∈(256,512] 的输入；且其错误是字符串 error，不是 `ProcessError`，错误映射需要适配。
- 想保留即时 fail-fast，只能放弃 `Parse`、用 Tokenizer 自建树——那就退化成"用 x/net tokenizer 的手写树"，树形状收益归零。

### 建议

接受"外层包一层"模式：`LimitedReader` + 事后 walk；把 `MaxHTMLNodes` 语义改述为"超限时丢弃结果并报错"（内存由 `MaxInputBytes` 兜底）。在 ADR 中明确记录 512 vs 256 的语义变化。

---

## 5. 序列化（html.Render vs serializeHTMLNode）

### 差异

来源：[render.go](https://github.com/golang/net/blob/master/html/render.go)、[escape.go](https://github.com/golang/net/blob/master/html/escape.go)、[Render godoc](https://pkg.go.dev/golang.org/x/net/html#Render)。

| 维度 | `html.Render` | 手写 `serializeHTMLNode` |
|---|---|---|
| 属性顺序 | 保留 `Node.Attr` slice 的源顺序 | `sort.Strings` 按 key 字典序 |
| 属性引号 | 一律 `key="v"` 双引号；空值也输出 `key=""`（render.go L151-175） | 空值省略 `=""` |
| void 元素 | `<br/>`（写 `"/>"`，render.go L176-181）；有子节点直接报错 | `<br>` 无斜杠 |
| 转义集 | `&'<>"` + `\r`→`&#13;`（escape.go `escapedChars` L317） | std `html.EscapeString`：`&'<>"`，无 `\r` |
| script/style 子 text | **原样字面输出**（`childTextNodesAreLiteral`，render.go L198-214，对应 WHATWG §13.3 serializing） | 所有 text 一律 EscapeString——sanitize 保留的 `<style>` CSS 中 `>`（子选择器）、`"` 都被转义 |
| 注释 | 输出 `<!--...-->`（`escapeComment` 最小转义） | 无注释节点 |
| `pre`/`textarea`/`listing` | 子 text 以 `\n` 开头时额外补一个 `\n` 防止被解析吞掉（render.go L187-195） | 无 |
| round-trip | godoc 明示 "best effort"：非 well-formed 树 Render 后再 Parse 不保证同构 | 同样不保证，但无文档约定 |

### 对 API / 迁移的影响

processor golden 会**全面变化**：属性顺序（最大 diff 面：手写是排序后的稳定序，Render 是源序——对同一输入仍是确定性的，但与现 golden 全不同）、`<br/>`、`key=""`、CSS 文本不再转义 `>`、`&#13;`。`fuzz_test.go` 的禁串断言（`<script`、`onerror="` 等）在 Render 下依然可用，因为 sanitize 在树上完成、Render 不会引入新元素；但需注意 CVE-2023-3978 教训：foreign namespace 的 text 节点曾被 Render 字面输出（见第 6 节），sanitize 必须在树上剥掉 svg/math 或确认其 text 已转义。

### 建议

迁移渲染侧时一次性重生成全部 golden，并保留结构级验证（现有 fixture/golden 约束在 CLAUDE.md 中已是硬要求）；把"属性源序输出"视为新的稳定契约写进测试。

---

## 6. 健壮性（fuzz 历史与 CVE）

### 事实（Go vulnerability database / golang/net 提交记录）

x/net/html 解析器的完整漏洞时间线（均为一手 OSV/vulndb 记录）：

| ID | 缺陷 | 修复版本 |
|---|---|---|
| [GO-2020-0014](https://pkg.go.dev/vuln/GO-2020-0014) (CVE-2018-17846) | `select` 标签处理不当导致无限循环 | 2018-09 |
| [GO-2021-0078](https://pkg.go.dev/vuln/GO-2021-0078) (CVE-2018-17075)、[GO-2022-0192/0193/0197](https://pkg.go.dev/vuln/GO-2022-0197) | 畸形输入 panic / 越界（isindex、template 组合等） | 2018-09 |
| [GO-2021-0238](https://pkg.go.dev/vuln/GO-2021-0238) (CVE-2021-33194) | `ParseFragment` 对 foreign content 内嵌 template 无限循环（[golang/go#46288](https://github.com/golang/go/issues/46288)） | 2021-05 |
| [GO-2023-1988](https://pkg.go.dev/vuln/GO-2023-1988) (CVE-2023-3978) | `Render` 对非 HTML namespace 的 text 节点字面输出 → XSS | v0.13.0 |
| [GO-2024-3333](https://pkg.go.dev/vuln/GO-2024-3333) (CVE-2024-45338) | Parse 系列函数对大小写不敏感内容非线性耗时 → DoS | v0.33.0 |
| [GO-2025-3595](https://pkg.go.dev/vuln/GO-2025-3595) (CVE-2025-22872) | foreign content 中未引号属性值尾随 `/` 被误判自闭合 → DOM 错位/XSS | v0.38.0 |
| [GO-2026-4440](https://pkg.go.dev/vuln/GO-2026-4440) (CVE-2025-47911) | spec 中按设计即二次方复杂度的算法可被恶意放大 → 引入 512 栈上限 | v0.45.0 |
| [GO-2026-4441](https://pkg.go.dev/vuln/GO-2026-4441) (CVE-2025-58190) | `inRowIM` 无限解析循环 | v0.45.0 |
| [GO-2026-5025](https://pkg.go.dev/vuln/GO-2026-5025) (CVE-2026-42506)、[GO-2026-5028](https://pkg.go.dev/vuln/GO-2026-5028) (CVE-2026-25680)、[GO-2026-5030](https://pkg.go.dev/vuln/GO-2026-5030) (CVE-2026-27136) | foreign content namespace 判定错误、Noah's Ark 条款 DoS、重复属性致 sanitizer 混淆 XSS | v0.55.0 |

Fuzz 覆盖：OSS-Fuzz 的 golang 项目只对 **std** `html` 包建了 escape/unescape fuzzer（[oss-fuzz projects/golang/build.sh L216-221](https://github.com/google/oss-fuzz/blob/master/projects/golang/build.sh)）；golang/net 的 `html/` 目录**没有原生 fuzz 测试**（目录清单只有 `*_test.go` 单元测试，[github.com/golang/net/tree/master/html](https://github.com/golang/net/tree/master/html)）。近年漏洞主要由外部研究者（Guido Vranken、Jakub Ciolek、ensy）通过自有 fuzzing/审计报告，修复响应活跃：2026 年仍在持续做 spec 对齐与安全修复（select 行为、bogus comment、NUL 处理等，见 [golang/net 提交记录](https://github.com/golang/net/commits/master/html)）。

### 对 API / 迁移的影响

- 采用 x/net 意味着把一个**活跃出 CVE、也活跃修 CVE**的完整 spec 实现纳入信任边界；本仓库当前 `cli/go.mod` 不依赖 `golang.org/x/net`，这是新增供应链项，需要纳入 govulncheck/升级节奏。
- 反面：手写树攻击面小、限界严格，但零外部审计，spec 覆盖极薄（上述整个"树形状"差异面就是它未实现的规则）。本仓库自己的 `fuzz_test.go` 是唯一防线。
- 值得注意：CVE-2026-27136（重复属性混淆 sanitizer）这类"parser 与浏览器不对齐 → sanitizer 被绕过"的 bug 类别，恰是手写解析器的固有高危面——手写树与浏览器的对齐程度远低于 x/net。

### 建议

无论是否迁移，保留并扩充现有端到端 fuzz（迁移后语料应加入 foster-parenting、foreign content、`<!--<script>` 类种子）；若迁移，固定最低版本 ≥ v0.55.0 并在 CI 加 govulncheck。

---

## 总建议：迁移主要风险点排序

1. **golden/输出全面漂移**（第 5、1、2 节）：属性顺序、`<br/>`、树重排、实体/换行归一化叠加，几乎每个 golden 都要重生成，diff 审查成本是迁移的最大确定性成本。
2. **limits 语义降级**（第 4 节）：`MaxHTMLNodes`/`MaxHTMLDepth` 的 fail-fast 只能改成"parse 后校验"，内置 512 上限不可配置且比现值宽松；必须外层包一层并接受错误分类适配。
3. **字节偏移契约丢失**（第 3 节）：`ProcessError.Offset` 依赖原输入偏移，Parse 树不提供位置信息；extract 层要么不迁、要么改用 Tokenizer+`Raw()`。
4. **script 原文非逐字节保真（树层）**（第 3 节）：`\r`→`\n`、NUL→U+FFFD；走 Tokenizer+`Raw()` 可完全规避。
5. **供应链与 CVE 跟随义务**（第 6 节）：新增 x/net 依赖 + 持续升级压力；换来的是与浏览器对齐的解析（sanitizer 绕过风险显著降低）。
6. **树重排对资源发现的行为影响**（第 1 节）：foster parenting/foreign content 改变节点路径，需用真实微信 fixture 验证 `resources.go` 不丢资源。

可行的低风险路径：**分层迁移**——先把 `render.go`/`resources.go` 的建树+序列化换成 `ParseFragment`+`Render`（一次性重生成 golden），`extract.go` 保持字节扫描（或独立换 Tokenizer+`Raw()`），`limits.go` 以外层 wrapper 保语义。整体可行，无单点阻断项；最大工作量在 golden 与 limits wrapper。
