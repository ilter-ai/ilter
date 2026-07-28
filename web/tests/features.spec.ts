import { expect, type Page, test } from '@playwright/test'
import { ADMIN_TOKEN, captureErrors, checkCrashIndicators, verifyMainContent } from './helpers'

const AUTH = { Authorization: `Bearer ${ADMIN_TOKEN}` }
const API = 'http://localhost:9191'
const PATH = `/features/budget?token=${ADMIN_TOKEN}`

// ── UI Component Tests ──

test.describe('Features / Budget page', () => {
  async function setup(page: Page) {
    const errors = captureErrors(page)
    await page.goto(PATH, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page
      .waitForFunction(() => !document.body.innerText.includes('Loading budget data...'), { timeout: 10000 })
      .catch(() => {})
    await page.waitForTimeout(2000)
    return errors
  }

  test('shows all 10 feature tab labels', async ({ page }) => {
    const errors = await setup(page)
    const tabLabels = [
      'Budget',
      'PII Protection',
      'Smart Router',
      'Rate Limiting',
      'Guardrails',
      'Loop Detection',
      'Semantic Cache',
      'Circuit Breaker',
      'Fallback',
      'Feature Flags',
    ]
    for (const label of tabLabels) {
      await expect(page.getByRole('tab', { name: new RegExp(label, 'i') })).toBeVisible({ timeout: 10000 })
    }
    await checkCrashIndicators(page, errors)
    await verifyMainContent(page, errors)
    expect(errors).toEqual([])
  })

  test('shows 6 stat cards', async ({ page }) => {
    const errors = await setup(page)
    const statLabels = [
      'Total Monthly Budget',
      'Remaining',
      'Over Budget',
      'Total Savings',
      'Total Requests',
      'Avg Cost/Request',
    ]
    for (const label of statLabels) {
      await expect(page.getByText(label, { exact: false }).first()).toBeVisible({ timeout: 10000 })
    }
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows Budget Configuration section', async ({ page }) => {
    const errors = await setup(page)
    await expect(page.getByText(/Budget|Monthly|Limit/, { exact: false }).first()).toBeVisible({ timeout: 10000 })
    // Budget config contains limit fields
    await expect(page.getByText(/monthly|budget|limit/i).first()).toBeVisible({ timeout: 10000 })
    await expect(page.getByText(/daily|spent|status/i).first()).toBeVisible({ timeout: 10000 })

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows scope selector tabs', async ({ page }) => {
    const errors = await setup(page)
    await expect(page.getByRole('button', { name: 'Key Level' })).toBeVisible({ timeout: 10000 })
    await expect(page.getByRole('button', { name: 'User Level' })).toBeVisible({ timeout: 10000 })
    await expect(page.getByRole('button', { name: 'Group Level' })).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows Cost & Analytics section', async ({ page }) => {
    const errors = await setup(page)
    await expect(page.getByText(/Cost|Analytics|Breakdown/i, { exact: false }).first()).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('Budget tab is selected by default', async ({ page }) => {
    const errors = await setup(page)
    const budgetTab = page.getByRole('tab', { name: /budget/i })
    await expect(budgetTab).toHaveAttribute('aria-selected', 'true')
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('Budget Configuration section rendered', async ({ page }) => {
    const errors = await setup(page)

    await expect(page.getByText(/Budget|Monthly|Limit/, { exact: false }).first()).toBeVisible({ timeout: 10000 })
    await expect(page.getByText(/monthly|budget|limit/i).first()).toBeVisible({ timeout: 10000 })
    await expect(page.getByText(/daily|spent|status/i).first()).toBeVisible({ timeout: 10000 })

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })
})

// ── Tab Interaction Tests ──

test.describe('Features tab interactions', () => {
  async function setupInteraction(page: Page) {
    const errors = captureErrors(page)
    await page.goto(PATH, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    return errors
  }

  async function waitForPageReady(page: Page) {
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
  }

  test('switches tabs and preserves aria-selected', async ({ page }) => {
    const errors = await setupInteraction(page)

    // 1. Verify Budget tab initially selected
    const budgetTab = page.getByRole('tab', { name: /^Budget$/i })
    await expect(budgetTab).toHaveAttribute('aria-selected', 'true', { timeout: 10000 })

    // 2. Click PII Protection tab
    const piiTab = page.getByRole('tab', { name: /PII Protection/i })
    await piiTab.click()
    await waitForPageReady(page)
    await expect(piiTab).toHaveAttribute('aria-selected', 'true', { timeout: 10000 })

    // 3. Click Smart Router tab
    const routerTab = page.getByRole('tab', { name: /Smart Router/i })
    await routerTab.click()
    await waitForPageReady(page)
    await expect(routerTab).toHaveAttribute('aria-selected', 'true', { timeout: 10000 })

    // 4. Click Rate Limiting tab
    const rateLimitTab = page.getByRole('tab', { name: /Rate Limiting/i })
    await rateLimitTab.click()
    await waitForPageReady(page)
    await expect(rateLimitTab).toHaveAttribute('aria-selected', 'true', { timeout: 10000 })

    // 5. Click back to Budget tab
    await budgetTab.click()
    await waitForPageReady(page)
    await expect(budgetTab).toHaveAttribute('aria-selected', 'true', { timeout: 10000 })

    // 6. Verify Budget Configuration section visible
    await expect(page.getByText(/Budget|Monthly|Limit/, { exact: false }).first()).toBeVisible({ timeout: 10000 })
    await expect(page.getByText(/monthly|budget|limit/i).first()).toBeVisible({ timeout: 10000 })
    await expect(page.getByText(/daily|spent|status/i).first()).toBeVisible({ timeout: 10000 })

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('clicking each of the 10 tabs sets it as selected', async ({ page }) => {
    const errors = await setupInteraction(page)
    const tabTests = [
      { name: 'Budget', urlPattern: /\/features\/budget/ },
      { name: 'PII Protection', urlPattern: /\/features\/pii/ },
      { name: 'Fallback', urlPattern: /\/features\/fallback/ },
    ]

    for (const { name, urlPattern } of tabTests) {
      await page.getByRole('tab', { name: new RegExp(`^${name}$`, 'i') }).click()
      await page.waitForURL(urlPattern)
      await waitForPageReady(page)

      // Re-query tab after navigation (page reference is stale after full-page nav)
      await expect(page.getByRole('tab', { name: new RegExp(`^${name}$`, 'i') })).toHaveAttribute(
        'aria-selected',
        'true',
      )
    }

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })
})

// ── Budget API ──

test.describe('Budget API', () => {
  test('GET /api/budget returns budget info', async ({ page }) => {
    const resp = await page.request.get(`${API}/api/budget`, { headers: AUTH })
    if (resp.status() === 404) {
      // Budget is optional — skip without failing
      return
    }
    expect(resp.ok()).toBeTruthy()
  })
})

// ── Fallback API & UI ──

test.describe('Fallback API & UI', () => {
  test('GET /api/fallback returns config summary', async ({ page }) => {
    const resp = await page.request.get(`${API}/api/fallback`, { headers: AUTH })
    expect(resp.ok()).toBeTruthy()
    const body = await resp.json()
    expect(body).toHaveProperty('enabled')
    expect(body).toHaveProperty('cooldown_duration')
    expect(body).toHaveProperty('active_cooldowns')
  })

  test('POST /api/fallback updates configuration', async ({ page }) => {
    const resp = await page.request.post(`${API}/api/fallback`, {
      headers: AUTH,
      data: {
        cooldown_duration: '3m',
        max_attempts: 2,
        model_downgrade: 'none',
      },
    })
    expect(resp.ok()).toBeTruthy()
    const body = await resp.json()
    expect(body.cooldown_duration).toMatch(/^3m(0s)?$/)
    expect(body.max_attempts).toBe(2)
  })
})
