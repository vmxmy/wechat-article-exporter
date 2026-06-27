// https://nuxt.com/docs/api/configuration/nuxt-config
export default defineNuxtConfig({
  compatibilityDate: '2025-10-30',
  devtools: {
    enabled: false,
  },
  modules: ['@vueuse/nuxt', '@nuxt/ui', 'nuxt-monaco-editor', '@sentry/nuxt/module', 'nuxt-umami'],
  ssr: false,
  runtimeConfig: {
    d1Cache: {
      // 默认开启（元数据持久化默认存储）；显式设 NUXT_D1_CACHE_ENABLED=false 才关闭。
      // 无 D1 binding 的环境（本地/非 CF）会在 getD1Database 处返回 null → 503 → 客户端静默纯本地，故默认开安全。
      enabled: process.env.NUXT_D1_CACHE_ENABLED !== 'false',
      binding: process.env.NITRO_D1_BINDING || 'DB',
      table: process.env.NITRO_D1_TABLE || 'cache_entries',
    },
    public: {
      aggridLicense: process.env.NUXT_AGGRID_LICENSE,
      sentry: {
        dsn: process.env.NUXT_SENTRY_DSN,
      },
    },
  },
  app: {
    head: {
      meta: [
        {
          name: 'referrer',
          content: 'no-referrer',
        },
      ],
      script: [
        {
          src: '/vendors/html-docx-js@0.3.1/html-docx.js',
          defer: true,
        },
      ],
    },
  },
  sourcemap: {
    client: 'hidden',
  },
  nitro: {
    preset: process.env.NITRO_PRESET,
    minify: process.env.NODE_ENV === 'production',
    rollupConfig: {
      external: ['puppeteer'],
    },
    storage: {
      kv: {
        driver: process.env.NITRO_KV_DRIVER || 'memory',
        base: process.env.NITRO_KV_BASE,
        binding: 'KV',
      },
    },
  },
  monacoEditor: {
    locale: 'en',
    componentName: {
      codeEditor: 'MonacoEditor', // 普通编辑器组件名
      diffEditor: 'MonacoDiffEditor', // 差异编辑器组件名
    },
  },

  // https://docs.sentry.io/platforms/javascript/guides/nuxt/manual-setup/
  sentry: {
    org: process.env.NUXT_SENTRY_ORG,
    project: process.env.NUXT_SENTRY_PROJECT,
    authToken: process.env.NUXT_SENTRY_AUTH_TOKEN,
    telemetry: false,
  },

  // https://umami.nuxt.dev/api/configuration
  umami: {
    enabled: true,
    id: process.env.NUXT_UMAMI_ID,
    host: process.env.NUXT_UMAMI_HOST,
    domains: ['down.mptext.top'],
    ignoreLocalhost: true,
    autoTrack: true,
    logErrors: true,
  },
});
