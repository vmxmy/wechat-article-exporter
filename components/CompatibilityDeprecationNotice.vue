<template>
  <div class="border-b border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-950 dark:border-amber-900 dark:bg-amber-950 dark:text-amber-100">
    <div class="mx-auto flex max-w-7xl flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
      <p>
        Web 与 remote MCP 已进入兼容迁移期。请在设置中导出浏览器本地数据；计划最早退役日期为
        <strong>2026-12-31</strong>，且必须先满足稳定版与功能对等门禁。
      </p>
      <div class="flex shrink-0 gap-3">
        <NuxtLink to="/settings/migration" class="font-semibold underline underline-offset-2">导出迁移包</NuxtLink>
        <a
          href="https://github.com/wechat-article/wechat-article-exporter/blob/master/docs/compatibility-retirement.md"
          target="_blank"
          rel="noopener noreferrer"
          class="font-semibold underline underline-offset-2"
        >
          查看计划
        </a>
      </div>
    </div>
  </div>
</template>
