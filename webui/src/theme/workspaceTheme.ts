import { defineTheme } from '@astryxdesign/core/theme'

export const workspaceTheme = defineTheme({
  name: 'wechat-article-workspace',
  color: { accent: '#1769aa', neutralStyle: 'cool', contrast: 'standard' },
  typography: {
    scale: { base: 16, ratio: 1.2 },
    body: { family: 'ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif' }
  },
  radius: { base: 6, multiplier: 1 }
})
