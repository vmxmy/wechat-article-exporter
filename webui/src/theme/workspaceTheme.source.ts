import { defineTheme } from '@astryxdesign/core/theme'

export const workspaceTheme = defineTheme({
  name: 'wechat-article-workspace',
  color: { accent: '#1769aa', neutralStyle: 'cool', contrast: 'standard' },
  typography: {
    scale: { base: 16, ratio: 1.2 },
    body: { family: 'Avenir Next, Segoe UI, -apple-system, BlinkMacSystemFont, PingFang SC, Hiragino Sans GB, Microsoft YaHei, Noto Sans SC, sans-serif' },
    heading: { family: 'Avenir Next, Segoe UI, -apple-system, BlinkMacSystemFont, PingFang SC, Hiragino Sans GB, Microsoft YaHei, Noto Sans SC, sans-serif' },
    code: { family: 'SFMono-Regular, Menlo, Monaco, Consolas, Liberation Mono, monospace' }
  },
  radius: { base: 6, multiplier: 1 }
})
