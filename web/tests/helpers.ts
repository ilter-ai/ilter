import { expect, type Page } from '@playwright/test'

// The dev server uses ILTER_ADMIN_KEY=test (from Makefile)
export const ADMIN_TOKEN = 'test'

// ── Test helpers ──

export const CRASH_INDICATORS = [
  'Error:',
  'Uncaught',
  'react-error-overlay',
  'error boundary',
  'crashed',
  'Something went wrong',
  'not defined',
  'Cannot read properties',
  'Cannot destructure',
  'undefined is not',
  'null is not',
] as const

export const IGNORED_CONSOLE_PATTERNS = [
  /Failed to load resource: the server responded with a status of 404/,
  /favicon\.ico/,
  /\.map$/,
  /sourcemap/i,
] as const

export const FORBIDDEN_VISIBLE_TEXT = ['Failed to load', 'Internal Server Error'] as const

export interface CaptureOptions {
  ignorePatterns?: RegExp[]
}

/**
 * Capture console errors and page crashes during a test.
 * Call before navigating, then assert errors array is empty after.
 */
export function captureErrors(page: Page, options?: CaptureOptions) {
  const errors: string[] = []
  const patterns = options?.ignorePatterns ?? []

  page.on('console', (msg) => {
    if (msg.type() === 'error') {
      const text = msg.text()
      if (IGNORED_CONSOLE_PATTERNS.some((p) => p.test(text))) return
      if (patterns.some((p) => p.test(text))) return
      errors.push(`[${msg.type()}] ${text}`)
    }
  })

  page.on('pageerror', (err) => {
    errors.push(`[PAGE ERROR] ${err.message}`)
  })

  return errors
}

/**
 * Check for crash indicators in visible text.
 */
export async function checkCrashIndicators(page: Page, errors: string[]): Promise<void> {
  for (const indicator of CRASH_INDICATORS) {
    const found = await page
      .getByText(indicator, { exact: false })
      .isVisible()
      .catch(() => false)
    if (found) {
      errors.push(`[CRASH INDICATOR] Found "${indicator}" on page`)
    }
  }
}

/**
 * Verify main content area exists and has meaningful content.
 */
export async function verifyMainContent(page: Page, errors: string[], minTextLength = 50): Promise<void> {
  const mainEl = page.locator('main').first()
  const mainCount = await mainEl.count()
  if (mainCount > 0) {
    const mainText = await mainEl.innerText()
    expect(mainText.length).toBeGreaterThan(minTextLength)
    for (const forbidden of FORBIDDEN_VISIBLE_TEXT) {
      const found = await page
        .getByText(forbidden, { exact: false })
        .isVisible()
        .catch(() => false)
      if (found) {
        errors.push(`[FORBIDDEN TEXT] Found "${forbidden}" on page`)
      }
    }
  }
}
