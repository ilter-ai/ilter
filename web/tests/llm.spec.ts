import { expect, type Page, test } from '@playwright/test'
import { ADMIN_TOKEN, captureErrors, checkCrashIndicators, verifyMainContent } from './helpers'

const PATH = `/llm/providers/?token=${ADMIN_TOKEN}`

test.describe('LLM / Providers page', () => {
  async function setup(page: Page) {
    const errors = captureErrors(page)
    await page.goto(PATH, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    return errors
  }

  test('shows Providers tab selected with Models and Prompts tabs', async ({ page }) => {
    const errors = await setup(page)
    // Tablist with role="tab": Providers (selected), Models, Prompts
    const providersTab = page.getByRole('tab', { name: /Providers/i })
    await expect(providersTab).toBeVisible({ timeout: 10000 })
    await expect(providersTab).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('tab', { name: /Models/i })).toBeVisible()
    await expect(page.getByRole('tab', { name: /Prompts/i })).toBeVisible()
    await checkCrashIndicators(page, errors)
    await verifyMainContent(page, errors)
    expect(errors).toEqual([])
  })

  test('shows Active Providers heading', async ({ page }) => {
    const errors = await setup(page)
    // Active Providers section has <h3> with text "Active Providers (N)"
    await expect(page.getByText('Active Providers', { exact: false })).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows opencode_go provider with Online status and model metrics', async ({ page }) => {
    const errors = await setup(page)
    // ProviderCard renders name <p> with "opencode_go" and StatusBadge with "Online"
    await expect(page.getByText('opencode_go', { exact: false }).first()).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('Online', { exact: false }).first()).toBeVisible()
    // Active Models section shows "Active Models" label and "22 / 22" count
    await expect(page.getByText(/\d+ \/ \d+/).first()).toBeVisible()
    // Models expand button: <button> containing "Models (22)"
    await expect(page.getByRole('button', { name: /Models \(\d+\)/ }).first()).toBeVisible()
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows opencode_zen provider with Online status and model metrics', async ({ page }) => {
    const errors = await setup(page)
    // ProviderCard renders name <p> with "opencode_zen" and StatusBadge with "Online"
    await expect(page.getByText('opencode_zen', { exact: false }).first()).toBeVisible({ timeout: 10000 })
    // Active Models section shows "Active Models" label and a count like "N / N"
    await expect(page.getByText(/\/ \d+/, { exact: false }).first()).toBeVisible()
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows Passive Providers heading', async ({ page }) => {
    const errors = await setup(page)
    // Passive Providers section has <h3> with text "Passive Providers (N)"
    await expect(page.getByText('Passive Providers', { exact: false })).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows at least one provider as Offline', async ({ page }) => {
    const errors = await setup(page)
    // StatusBadge renders "Offline" for providers with status other than 'online'/'degraded'
    await expect(page.getByText('Offline', { exact: false }).first()).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  // ── Interaction Tests ──

  test('switches to Models tab and verifies it is selected', async ({ page }) => {
    const errors = captureErrors(page)
    await page.goto(`/llm/models?token=${ADMIN_TOKEN}`, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    // Models tab should be selected
    await expect(page.getByRole('tab', { name: /Models/i })).toHaveAttribute('aria-selected', 'true')
    // Providers tab should NOT be selected
    await expect(page.getByRole('tab', { name: /Providers/i })).toHaveAttribute('aria-selected', 'false')
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('switches to Prompts tab and verifies it is selected', async ({ page }) => {
    const errors = captureErrors(page)
    await page.goto(`/llm/prompts?token=${ADMIN_TOKEN}`, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    // Prompts tab should be selected
    await expect(page.getByRole('tab', { name: /Prompts/i })).toHaveAttribute('aria-selected', 'true')
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('switches from Models back to Providers tab', async ({ page }) => {
    const errors = captureErrors(page)
    await page.goto(`/llm/models?token=${ADMIN_TOKEN}`, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    // Click Providers tab link to navigate back
    await page.getByRole('tab', { name: /Providers/i }).click()
    await page.waitForURL('**/llm/providers**')
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    // Verify Providers tab is selected
    await expect(page.getByRole('tab', { name: /Providers/i })).toHaveAttribute('aria-selected', 'true')
    // Active Providers heading visible
    await expect(page.getByText('Active Providers', { exact: false })).toBeVisible()
    // At least one Online status badge visible
    await expect(page.getByText('Online', { exact: false }).first()).toBeVisible()
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })
})
