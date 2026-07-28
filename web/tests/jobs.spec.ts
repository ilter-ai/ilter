import { expect, type Page, test } from '@playwright/test'
import { ADMIN_TOKEN, captureErrors, checkCrashIndicators, verifyMainContent } from './helpers'

const PATH = `/jobs/?token=${ADMIN_TOKEN}`

test.describe('Jobs dashboard', () => {
  async function setup(page: Page) {
    const errors = captureErrors(page)
    await page.goto(PATH, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    return errors
  }

  test('shows stat cards with labels and Active Jobs count', async ({ page }) => {
    const errors = await setup(page)
    await expect(page.getByText('Active Jobs', { exact: false })).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('Next Run', { exact: false }).first()).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('Recent Failures', { exact: false })).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('2 / 2', { exact: false })).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    await verifyMainContent(page, errors)
    expect(errors).toEqual([])
  })

  test('shows Create Job button', async ({ page }) => {
    const errors = await setup(page)
    await expect(page.getByRole('button', { name: /Create Job/i })).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows all 6 table column headers', async ({ page }) => {
    const errors = await setup(page)
    const headers = ['Name', 'Schedule', 'Next Run', 'Last Run', 'Status', 'Actions']
    for (const header of headers) {
      await expect(page.getByRole('columnheader', { name: new RegExp(header.replace(/ /g, '\\s+'), 'i') })).toBeVisible(
        {
          timeout: 10000,
        },
      )
    }
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows job names Content Analysis and DB Health Check', async ({ page }) => {
    const errors = await setup(page)
    await expect(page.getByText('Content Analysis', { exact: false })).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('DB Health Check', { exact: false })).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows Active status badge on each job', async ({ page }) => {
    const errors = await setup(page)
    const badges = page.getByText('Active', { exact: false })
    const count = await badges.count()
    expect(count).toBeGreaterThanOrEqual(1)
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows Run Now buttons', async ({ page }) => {
    const errors = await setup(page)
    const runNowButtons = page.getByRole('button', { name: /Run Now/i })
    await expect(runNowButtons.first()).toBeVisible({ timeout: 10000 })
    const count = await runNowButtons.count()
    expect(count).toBeGreaterThanOrEqual(1)
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows schedule column with code element per job row', async ({ page }) => {
    const errors = await setup(page)
    const scheduleCells = page.locator('table tbody tr td:nth-child(2) code')
    const count = await scheduleCells.count()
    expect(count).toBeGreaterThanOrEqual(1)
    await expect(scheduleCells.first()).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows toggle switches all checked', async ({ page }) => {
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
})
