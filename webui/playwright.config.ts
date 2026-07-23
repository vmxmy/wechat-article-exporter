import { defineConfig, devices } from '@playwright/test'

const port = Number(process.env.WEBUI_E2E_PORT ?? 4173)
const baseURL = `http://127.0.0.1:${port}`

export default defineConfig({
  testDir: './e2e',
  fullyParallel: false,
  forbidOnly: Boolean(process.env.CI),
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: process.env.CI ? [['github'], ['html', { outputFolder: '../.playwright-report', open: 'never' }]] : [['list'], ['html', { outputFolder: '../.playwright-report', open: 'never' }]],
  use: {
    baseURL,
    // The loopback fixture fulfils every API request in-process. Chromium's
    // network-trace copier races those fulfilled responses on failure, which
    // can mask the actual assertion with ENOENT; screenshots and videos still
    // provide failure artifacts.
    trace: 'off',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure'
  },
  webServer: {
    command: `pnpm exec vite --host 127.0.0.1 --port ${port} --strictPort`,
    url: baseURL,
    reuseExistingServer: !process.env.CI,
    stdout: 'pipe',
    stderr: 'pipe',
    timeout: 30_000
  },
  projects: [{ name: 'chromium', use: { ...devices['Desktop Chrome'] } }],
  outputDir: '../.playwright-test-results'
})
