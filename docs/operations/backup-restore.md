# 备份、恢复与回滚

备份归档包含 active profile 的 SQLite、内容寻址对象和非 secret 配置，并带独立 manifest 和 SHA-256 校验。操作系统凭据库中的微信会话、Credential 和代理 authorization 默认不进入归档。

## 创建与验证

```bash
wechat-article db backup --output ./backups/work.zip
wechat-article db verify ./backups/work.zip
wechat-article db integrity
```

在升级、迁移、恢复和大规模 GC 前创建并验证备份。不要在程序运行时手工复制 SQLite/WAL 文件。

## 恢复

恢复先在 staging 目录验证数据库、对象和 manifest，再原子替换 active profile 存储。需要精确确认值：

```bash
wechat-article db restore ./backups/work.zip \
  --conflict refuse \
  --confirm 'restore-backup:./backups/work.zip'
```

profile 冲突可选择拒绝或重命名。失败时 staging 被清理，现有 profile 保持可用；恢复完成后程序重新打开数据库。

## 垃圾回收

先运行 dry run，核对对象数量、字节数和 retention，再使用命令输出要求的精确确认值执行。共享或仍被引用的对象不能删除。

## 二进制回滚

- 数据库 schema 只向前迁移；旧二进制会拒绝新 schema。
- 回滚线上 Web/MCP 与回滚本地数据库是两个独立操作。
- 优先修复或升级；如必须回滚本地数据，恢复升级前备份到独立 profile。
- 兼容期紧急恢复 Web/MCP 不会修改用户的本地 SQLite。
