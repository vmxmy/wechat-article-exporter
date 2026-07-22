<template>
  <div class="h-full">
    <Teleport defer to="#title">
      <h1 class="text-[28px] leading-[34px] text-slate-12 dark:text-slate-50 font-bold">设置</h1>
    </Teleport>

    <div class="h-full overflow-scroll">
      <UCard class="mx-4 mt-10">
        <template #header>
          <div class="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
            <div>
              <h2 class="text-2xl font-semibold">旧版数据迁移</h2>
              <p class="mt-1 text-sm text-slate-500 dark:text-slate-400">
                将浏览器本地 IndexedDB 数据打包为带 manifest 和 SHA-256 校验的 ZIP，全程不上传。
              </p>
            </div>
            <UButton
              to="/settings/migration"
              color="blue"
              icon="i-lucide:archive"
              trailing-icon="i-lucide:arrow-right"
            >
              导出旧版数据
            </UButton>
          </div>
        </template>
      </UCard>
      <SettingProxy />
      <div class="flex flex-wrap">
        <SettingExport />
        <SettingMisc />
      </div>
      <div class="flex flex-wrap">
        <SettingLegacyMigration />
      </div>
      <!--      <div>-->
      <!--        <SettingDisplay />-->
      <!--      </div>-->
      <div class="h-[30vh]"></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { websiteName } from '~/config';

useHead({
  title: `设置 | ${websiteName}`,
});
</script>
