/// <reference types="node" />
import { defineConfig } from '@playwright/test';

export default defineConfig({
  timeout: 30000,
  expect: { timeout: 10000 },
  fullyParallel: true,
  retries: 0,
  reporter: [
    ['json', { outputFile: '../playwright/smoke-report.json' }],
    ['list'],
  ],
  use: {
    baseURL: 'http://localhost:9191',
    channel: 'chrome',
  },
  testDir: './tests',
  // Server MUST be running externally (e.g. via `make dev`).
  // CI starts the server before Playwright via a separate step.
  // webServer removed to avoid port 9192 (metrics) conflicts with the running dev instance.
  webServer: [],
});
