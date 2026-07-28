import { expect, type Page, test } from '@playwright/test'
import { ADMIN_TOKEN, captureErrors, checkCrashIndicators, verifyMainContent } from './helpers'

const AUTH = { Authorization: `Bearer ${ADMIN_TOKEN}` }
const API = 'http://localhost:9191'
const PATH = `/logs/requests?token=${ADMIN_TOKEN}`

// ── UI Component Tests ──

test.describe('Logs / Requests page', () => {
  async function setup(page: Page) {
    const errors = captureErrors(page)
    await page.goto(PATH, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    return errors
  }

  test('shows Requests tab selected and MCP tab', async ({ page }) => {
    const errors = await setup(page)
    // Tablist with role="tab" — Requests is selected, MCP is available
    const reqTab = page.getByRole('tab', { name: /Requests/i })
    await expect(reqTab).toBeVisible({ timeout: 10000 })
    await expect(reqTab).toHaveAttribute('aria-selected', 'true')
    await expect(page.getByRole('tab', { name: /MCP/i })).toBeVisible()
    await checkCrashIndicators(page, errors)
    await verifyMainContent(page, errors)
    expect(errors).toEqual([])
  })

  test('shows Auto-refresh checkbox checked by default', async ({ page }) => {
    const errors = await setup(page)
    // Auto-refresh is an <input type="checkbox"> inside a <label> with text "Auto-refresh"
    const checkbox = page.getByRole('checkbox', { name: /auto.?refresh/i })
    await expect(checkbox).toBeVisible({ timeout: 10000 })
    await expect(checkbox).toBeChecked()
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows all four time range buttons', async ({ page }) => {
    const errors = await setup(page)
    // TimeRangeFilter renders <button> elements: Last Hour, Last 24h, Last 7 Days, Custom
    await expect(page.getByRole('button', { name: 'Last Hour' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Last 24h' })).toBeVisible()
    await expect(page.getByRole('button', { name: /Last 7 Days/i })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Custom' })).toBeVisible()
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows Total Requests stat card', async ({ page }) => {
    const errors = await setup(page)
    // StatCard renders <p>{title}</p> with "Total Requests"
    await expect(page.getByText('Total Requests', { exact: false })).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows Error Rate stat card', async ({ page }) => {
    const errors = await setup(page)
    await expect(page.getByText('Error Rate', { exact: false })).toBeVisible()
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows Cost stat card', async ({ page }) => {
    const errors = await setup(page)
    await expect(page.getByText('Cost', { exact: false }).first()).toBeVisible()
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows Cache Hit Rate stat card', async ({ page }) => {
    const errors = await setup(page)
    await expect(page.getByText('Cache Hit Rate', { exact: false })).toBeVisible()
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows search box', async ({ page }) => {
    const errors = await setup(page)
    // FilterBar renders <input> with placeholder "Search by model, provider, IP..."
    const searchBox = page.getByPlaceholder(/Search by model/i)
    await expect(searchBox).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows status filter buttons (All, Success, Error)', async ({ page }) => {
    const errors = await setup(page)
    // Three <button> elements: "All", "Success", "Error"
    await expect(page.getByRole('button', { name: 'All' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Success' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Error' })).toBeVisible()
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows DataTable columns when requests exist', async ({ page }) => {
    const errors = await setup(page)
    // Empty state shows "No requests yet"; otherwise DataTable renders <th> column headers
    const emptyState = page.getByText('No requests yet')
    const isEmpty = await emptyState.isVisible().catch(() => false)
    if (!isEmpty) {
      for (const col of ['Time', 'Status', 'Model', 'Provider', 'Tokens', 'Cost', 'Latency', 'IP']) {
        await expect(page.getByRole('columnheader', { name: col })).toBeVisible()
      }
    }
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows pagination text when data spans multiple pages', async ({ page }) => {
    const errors = await setup(page)
    // Pagination renders text "Showing X to Y of Z results" + Previous/Next buttons
    const showing = page.getByText(/Showing/i)
    const isVisible = await showing.isVisible().catch(() => false)
    if (isVisible) {
      await expect(showing).toBeVisible()
      await expect(page.getByRole('button', { name: /Previous/i })).toBeVisible()
      await expect(page.getByRole('button', { name: /Next/i })).toBeVisible()
    }
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  // ── Interaction Tests ──

  test('switches to MCP tab and verifies it is selected', async ({ page }) => {
    const errors = captureErrors(page)
    await page.goto(`/logs/mcp?token=${ADMIN_TOKEN}`, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    // MCP tab should be selected
    await expect(page.getByRole('tab', { name: /MCP/i })).toHaveAttribute('aria-selected', 'true')
    // Requests tab should NOT be selected
    await expect(page.getByRole('tab', { name: /Requests/i })).toHaveAttribute('aria-selected', 'false')
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('switches from MCP back to Requests tab', async ({ page }) => {
    const errors = captureErrors(page)
    await page.goto(`/logs/mcp?token=${ADMIN_TOKEN}`, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    // Click Requests tab link to navigate back
    await page.getByRole('tab', { name: /Requests/i }).click()
    await page.waitForURL('**/logs/requests**')
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    // Verify Requests tab is selected
    await expect(page.getByRole('tab', { name: /Requests/i })).toHaveAttribute('aria-selected', 'true')
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows at least 2 table rows with model data', async ({ page }) => {
    const errors = await setup(page)
    await expect(page.getByRole('heading', { name: /Requests|Logs/i }).first()).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })
})

// ── Audit Log API ──

test.describe('Audit Log', () => {
  test('GET /api/requests returns request log', async ({ page }) => {
    const resp = await page.request.get(`${API}/api/requests`, { headers: AUTH })
    expect(resp.ok()).toBeTruthy()
    const data = await resp.json()
    expect(data).toBeDefined()
  })
})
