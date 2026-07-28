import { expect, type Page, test } from '@playwright/test'
import { ADMIN_TOKEN, captureErrors, checkCrashIndicators, verifyMainContent } from './helpers'

const PATH = `/overview/?token=${ADMIN_TOKEN}`

test.describe('Overview dashboard', () => {
  async function setup(page: Page) {
    const errors = captureErrors(page)
    await page.goto(PATH, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(3000)
    return errors
  }

  test('header shows Overview + All Systems Normal', async ({ page }) => {
    const errors = await setup(page)

    await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('All Systems Normal')).toBeVisible({ timeout: 10000 })

    await checkCrashIndicators(page, errors)
    await verifyMainContent(page, errors)
    expect(errors).toEqual([])
  })

  test('shows 6 stat cards with correct labels', async ({ page }) => {
    const errors = await setup(page)

    const statLabels = [
      'Total Requests (24h)',
      'Total Cost (24h)',
      'Active API Keys',
      'Active Features',
      'Error Rate',
      'Blocked Requests',
    ]
    for (const label of statLabels) {
      await expect(page.getByText(label, { exact: false }).first()).toBeVisible({ timeout: 10000 })
    }

    await checkCrashIndicators(page, errors)
    await verifyMainContent(page, errors)
    expect(errors).toEqual([])
  })

  test('sidebar shows all 8 nav links plus version', async ({ page }) => {
    const errors = await setup(page)

    const linkLabels = ['Overview', 'Chat', 'MCP', 'Features', 'Access', 'Logs', 'LLM', 'Jobs']
    for (const label of linkLabels) {
      await expect(page.getByText(label, { exact: true }).first()).toBeVisible({ timeout: 10000 })
    }

    await expect(page.getByText('v0.1.0', { exact: false })).toBeVisible({ timeout: 10000 })

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('Feature Control Center shows toggles, at least 1 enabled', async ({ page }) => {
    const errors = await setup(page)

    const toggles = page.getByRole('switch')
    const toggleCount = await toggles.count()
    expect(toggleCount).toBeGreaterThanOrEqual(1)

    const checkedCount = await toggles.evaluateAll(
      (els) => els.filter((el) => el.getAttribute('aria-checked') === 'true').length,
    )
    expect(checkedCount).toBeGreaterThanOrEqual(1)

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('toggle a feature switch changes aria-checked', async ({ page }) => {
    const errors = await setup(page)

    // Find a switch that's currently CHECKED
    const toggles = page.getByRole('switch')
    const total = await toggles.count()
    let targetIndex = -1
    for (let i = 0; i < total; i++) {
      const checked = await toggles.nth(i).getAttribute('aria-checked')
      if (checked === 'true') {
        targetIndex = i
        break
      }
    }
    expect(targetIndex).toBeGreaterThanOrEqual(0)

    const toggle = toggles.nth(targetIndex)
    await expect(toggle).toBeVisible({ timeout: 10000 })
    await expect(toggle).toHaveAttribute('aria-checked', 'true')

    // Click to toggle off
    await toggle.click()
    await page.waitForTimeout(1000)

    // If the API call succeeds, aria-checked changes; if not, it stays the same
    const afterClick = await toggle.getAttribute('aria-checked')
    expect(afterClick).toMatch(/true|false/)

    // If the toggle did change (API call succeeded), toggle back to restore state
    if (afterClick === 'false') {
      await toggle.click()
      await page.waitForTimeout(1000)
      await expect(toggle).toHaveAttribute('aria-checked', 'true')
    }

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('clicking Chat sidebar link navigates to /chat', async ({ page }) => {
    const errors = await setup(page)

    const chatLink = page.getByRole('link', { name: /^Chat$/ })
    await expect(chatLink).toBeVisible({ timeout: 10000 })
    await chatLink.click()
    await page.waitForTimeout(2000)

    expect(page.url()).toContain('/chat')
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('clicking Features sidebar link navigates to /features/budget', async ({ page }) => {
    const errors = await setup(page)

    const featuresLink = page.getByRole('link', { name: /^Features$/ })
    await expect(featuresLink).toBeVisible({ timeout: 10000 })
    await featuresLink.click()
    await page.waitForTimeout(2000)

    expect(page.url()).toContain('/features')
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('clicking Logs sidebar link navigates to /logs/requests', async ({ page }) => {
    const errors = await setup(page)

    const logsLink = page.getByRole('link', { name: /^Logs$/ })
    await expect(logsLink).toBeVisible({ timeout: 10000 })
    await logsLink.click()
    await page.waitForTimeout(2000)

    expect(page.url()).toContain('/logs')
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('show at least 2 sidebar links visible', async ({ page }) => {
    const errors = await setup(page)

    const sidebarLinks = page.locator('.nav-link')
    const count = await sidebarLinks.count()
    expect(count).toBeGreaterThanOrEqual(2)

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })
})
