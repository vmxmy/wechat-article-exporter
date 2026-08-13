# patrol — 现网版式巡检

对现网微信文章页逐锚点探测公众号名 anchor chain（`wechat.ArticleAccountNameChain`）的命中情况，用于发现版式漂移。链定义单一来源于 `internal/wechat`，本工具不复制锚点规则。

## 用法

```bash
cd cli
go run ./tools/patrol -urls /path/to/urls.txt
```

## 链接文件格式

- 每行一条文章 URL；
- `#` 开头的行为注释，空行忽略；
- 链接文件保存在仓库外（不进 repo）。

## 退出码

- `0`：所有 URL 的主锚点（链首）均命中；
- `1`：任一 URL 主锚点未命中（含抓取/解析失败）；
- `2`：运行错误（如链接文件不可读）。

单次失败 ≠ 漂移：重跑确认后再回流。

## 脱敏承诺

报告只输出 URL 序号（`URL_1`…）、锚点名与命中/未命中，绝不打印 URL、文章内容或提取值；失败备注只含脱敏分类（`fetch failed` / `HTTP <code>` / `parse failed`）。
