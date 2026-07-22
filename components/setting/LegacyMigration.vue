<template>
  <UCard class="mx-4 mt-10 flex-1">
    <template #header>
      <h3 class="text-2xl font-semibold">迁移到本地 CLI</h3>
    </template>

    <p class="text-sm text-slate-600 dark:text-slate-300">
      浏览器 IndexedDB 不能由 CLI 自动读取。请在兼容期内从当前浏览器导出版本化 ZIP，再在本机执行
      <code>wechat-article migration import &lt;archive.zip&gt;</code>。导出只读取本地 Dexie 数据并触发浏览器下载，
      不会上传文章、资源或凭据。
    </p>
    <div class="mt-4 flex items-center gap-3">
      <UButton :loading="exporting" icon="i-heroicons-arrow-down-tray" @click="runExport">导出本地迁移包</UButton>
      <span class="text-sm text-slate-500">{{ progress.message }}（{{ progress.percent }}%）</span>
    </div>
    <UAlert
      v-if="result?.manifest.status === 'partial'"
      class="mt-4"
      color="orange"
      title="迁移包已生成，但部分资源缺失"
      :description="`缺失资源 ${result.manifest.missingResources.length} 个；CLI 导入后可按报告重新下载。`"
    />
    <UAlert v-if="error" class="mt-4" color="red" title="导出失败" :description="error.message" />
  </UCard>
</template>

<script setup lang="ts">
const { exporting, progress, result, error, exportArchive } = useLegacyArchiveExport();

async function runExport() {
  await exportArchive();
}
</script>
