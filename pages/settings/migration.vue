<template>
  <div class="min-h-screen bg-slate-50 px-4 py-10 dark:bg-slate-950 sm:px-6">
    <main class="mx-auto max-w-4xl space-y-6">
      <div>
        <NuxtLink to="/dashboard/settings" class="text-sm font-medium text-blue-600 hover:underline dark:text-blue-400">
          ← 返回设置
        </NuxtLink>
        <h1 class="mt-4 text-3xl font-bold text-slate-900 dark:text-slate-50">导出旧版浏览器数据</h1>
        <p class="mt-3 max-w-3xl leading-7 text-slate-600 dark:text-slate-300">
          生成供本地 CLI 导入的版本化 ZIP 归档。导出过程只读取当前浏览器的 IndexedDB，所有整理、校验和压缩都在本机完成。
        </p>
      </div>

      <UAlert
        color="blue"
        variant="soft"
        icon="i-lucide:shield-check"
        title="完全本地，不上传数据"
        description="不会调用服务器或上传接口。归档可能包含文章正文、评论和资源文件，请像保管原始数据一样妥善保存。"
      />

      <UCard>
        <template #header>
          <div>
            <h2 class="text-xl font-semibold">归档内容</h2>
            <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">支持当前 Dexie v1-v3 数据库升级后的全部本地表。</p>
          </div>
        </template>

        <div class="grid gap-3 text-sm text-slate-700 dark:text-slate-200 sm:grid-cols-2">
          <div v-for="item in archiveContents" :key="item" class="flex items-center gap-2">
            <UIcon name="i-lucide:check" class="size-4 text-emerald-500" />
            <span>{{ item }}</span>
          </div>
        </div>

        <div class="mt-6 rounded-lg bg-slate-100 p-4 text-sm leading-6 text-slate-600 dark:bg-slate-900 dark:text-slate-300">
          ZIP 内包含 <code>manifest.json</code>、<code>checksums.json</code>、逐表 JSON 记录和原始 Blob 字节。
          每个文件都有 SHA-256 校验；如果资源映射引用了缺失文件，归档仍会下载并标记为 partial。
        </div>

        <div v-if="progress.phase !== 'idle'" class="mt-6 space-y-2">
          <div class="flex items-center justify-between gap-4 text-sm">
            <span class="font-medium text-slate-700 dark:text-slate-200">{{ progress.message }}</span>
            <span class="tabular-nums text-slate-500 dark:text-slate-400">{{ progress.percent }}%</span>
          </div>
          <UProgress :value="progress.percent" :max="100" :color="progressColor" />
        </div>

        <UAlert
          v-if="error"
          class="mt-6"
          color="red"
          variant="soft"
          icon="i-lucide:circle-alert"
          title="导出失败"
          :description="error.message"
        />

        <UAlert
          v-if="result"
          class="mt-6"
          :color="result.manifest.status === 'complete' ? 'green' : 'orange'"
          variant="soft"
          :icon="result.manifest.status === 'complete' ? 'i-lucide:circle-check' : 'i-lucide:triangle-alert'"
          :title="result.manifest.status === 'complete' ? '归档已下载' : '归档已下载，但包含缺失数据'"
        >
          <template #description>
            <p>{{ result.filename }} · {{ formatBytes(result.blob.size) }}</p>
            <p v-if="result.manifest.status === 'partial'" class="mt-1">
              缺失资源 {{ result.manifest.missingResources.length }} 个，其他警告
              {{ Math.max(0, result.manifest.warnings.length - result.manifest.missingResources.length) }} 个。
              详情已写入 manifest.json。
            </p>
          </template>
        </UAlert>

        <template #footer>
          <div class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <p class="text-xs leading-5 text-slate-500 dark:text-slate-400">
              导出期间请保持此页面打开。浏览器会在完成后显示 ZIP 下载。
            </p>
            <UButton
              color="blue"
              size="lg"
              icon="i-lucide:archive"
              :loading="exporting"
              :disabled="exporting"
              @click="startExport"
            >
              {{ exporting ? '正在生成归档' : '导出旧版数据 ZIP' }}
            </UButton>
          </div>
        </template>
      </UCard>
    </main>
  </div>
</template>

<script setup lang="ts">
import { websiteName } from '~/config';

const archiveContents = [
  '公众号账号与文章索引',
  '文章 HTML 原始字节',
  '阅读量等元数据',
  '评论与回复',
  '文章资源映射',
  '图片、样式与其他资源字节',
];

const { exporting, progress, result, error, exportArchive } = useLegacyArchiveExport();
const toast = useToast();

const progressColor = computed(() => {
  if (progress.value.phase === 'error') return 'red';
  if (progress.value.phase === 'complete') return result.value?.manifest.status === 'partial' ? 'orange' : 'green';
  return 'blue';
});

async function startExport() {
  try {
    const exported = await exportArchive();
    toast.add({
      color: exported.manifest.status === 'complete' ? 'green' : 'orange',
      title: exported.manifest.status === 'complete' ? '旧版数据导出完成' : '旧版数据已部分导出',
      description:
        exported.manifest.status === 'complete'
          ? 'ZIP 已保存到浏览器下载目录。'
          : `ZIP 已保存，manifest 记录了 ${exported.manifest.missingResources.length} 个缺失资源。`,
    });
  } catch (caught) {
    console.error('[legacy-archive-export] failed', caught);
  }
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = bytes / 1024;
  let unitIndex = 0;
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024;
    unitIndex++;
  }
  return `${value.toFixed(value >= 10 ? 1 : 2)} ${units[unitIndex]}`;
}

useHead({
  title: `旧版数据迁移 | ${websiteName}`,
});
</script>
