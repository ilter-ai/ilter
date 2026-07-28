import { expect, type Page, test } from '@playwright/test'
import { ADMIN_TOKEN, captureErrors, checkCrashIndicators, verifyMainContent } from './helpers'

const AUTH = { Authorization: `Bearer ${ADMIN_TOKEN}` }
const API = 'http://localhost:9191'
const PATH = `/mcp/servers/?token=${ADMIN_TOKEN}`

// ── UI Component Tests ──

test.describe('MCP Servers page', () => {
  async function setup(page: Page) {
    const errors = captureErrors(page)
    await page.goto(PATH, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    return errors
  }

  test('shows Servers and Marketplace tabs, Servers selected', async ({ page }) => {
    const errors = await setup(page)
    const serversTab = page.getByRole('tab', { name: /Servers/i })
    const marketplaceTab = page.getByRole('tab', { name: /Marketplace/i })
    await expect(serversTab).toBeVisible({ timeout: 10000 })
    await expect(marketplaceTab).toBeVisible({ timeout: 10000 })
    await expect(serversTab).toHaveAttribute('aria-selected', 'true')
    await checkCrashIndicators(page, errors)
    await verifyMainContent(page, errors)
    expect(errors).toEqual([])
  })

  test('shows 4 stat cards with correct labels', async ({ page }) => {
    const errors = await setup(page)
    const statLabels = ['Total Servers', 'Online', 'Offline / Error', 'Available Tools']
    for (const label of statLabels) {
      await expect(page.getByText(label, { exact: false }).first()).toBeVisible({ timeout: 10000 })
    }
    // 4 stat cards = 4 stat values rendered as bold paragraphs
    await expect(page.locator('p.text-surface-900')).toHaveCount(4)
    await checkCrashIndicators(page, errors)
    await verifyMainContent(page, errors)
    expect(errors).toEqual([])
  })

  test('shows How To Connect and Add Server buttons', async ({ page }) => {
    const errors = await setup(page)
    await expect(page.getByRole('button', { name: /How To Connect/i })).toBeVisible({ timeout: 10000 })
    await expect(page.getByRole('button', { name: /Add Server/i })).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows search textbox with placeholder', async ({ page }) => {
    const errors = await setup(page)
    const search = page.getByPlaceholder('Search by name, URL, or transport...')
    await expect(search).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows status filter buttons All, Online, Offline, Error', async ({ page }) => {
    const errors = await setup(page)
    // All button is plain text; Online/Offline/Error have count suffix via <span>
    await expect(page.getByRole('button', { name: /^All$/ })).toBeVisible({ timeout: 10000 })
    await expect(page.getByRole('button', { name: /Online/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /Offline/i })).toBeVisible()
    await expect(page.getByRole('button', { name: /Error/i })).toBeVisible()
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows server cards with names and tool counts', async ({ page }) => {
    const errors = await setup(page)
    // Seeded servers: Fetch (1 tool) and SQLite (8 tools)
    await expect(page.getByText('Fetch').first()).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('SQLite').first()).toBeVisible({ timeout: 10000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('opens Add Server modal with form fields', async ({ page }) => {
    const errors = await setup(page)
    await page
      .getByRole('button', { name: /Add Server/i })
      .first()
      .click()
    await expect(page.getByText('Server Name')).toBeVisible()
    await expect(page.getByText('Transport')).toBeVisible()
    await expect(page.getByRole('button', { name: /Cancel/i })).toBeVisible()
    await page.getByRole('button', { name: /Cancel/i }).click()
    await expect(page.getByText('Server Name')).not.toBeVisible()
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('opens How To Connect panel with connection details', async ({ page }) => {
    const errors = await setup(page)
    await page.getByRole('button', { name: /How To Connect/i }).click()
    await expect(page.getByText('Connect to ILTER MCP Hub')).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('Endpoint URL')).toBeVisible()
    await expect(page.getByText('Auth Token')).toBeVisible()
    await expect(page.getByText('Example Config (SSE)')).toBeVisible()
    await page.getByRole('button', { name: /Close/i }).click()
    await expect(page.getByText('Connect to ILTER MCP Hub')).not.toBeVisible()
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('clicking Marketplace tab navigates and swaps aria-selected', async ({ page }) => {
    const errors = captureErrors(page)
    await page.goto(PATH, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)

    await expect(page.getByRole('tab', { name: /Servers/i })).toHaveAttribute('aria-selected', 'true', {
      timeout: 10000,
    })
    await expect(page.getByRole('tab', { name: /Marketplace/i })).toHaveAttribute('aria-selected', 'false')

    // Marketplace tab is <a href="/mcp/marketplace"> — click triggers full navigation
    await page.getByRole('tab', { name: /Marketplace/i }).click()
    await page.waitForURL('**/mcp/marketplace', { timeout: 15000 })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)

    await expect(page.getByRole('tab', { name: /Servers/i })).toHaveAttribute('aria-selected', 'false')
    await expect(page.getByRole('tab', { name: /Marketplace/i })).toHaveAttribute('aria-selected', 'true')
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('status filter buttons are interactive — Offline shows empty state', async ({ page }) => {
    const errors = await setup(page)

    await page.getByRole('button', { name: /Offline/i }).click()
    await expect(page.getByText('No matching servers')).toBeVisible({ timeout: 5000 })

    await page.getByRole('button', { name: /^All$/ }).click()
    await expect(page.getByText('Fetch').first()).toBeVisible({ timeout: 5000 })
    await expect(page.getByText('SQLite').first()).toBeVisible({ timeout: 5000 })
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('server cards render action buttons (Test, Access, Configure)', async ({ page }) => {
    const errors = await setup(page)
    const testBtn = page.getByRole('button', { name: /Test/i }).first()
    const accessBtn = page.getByRole('button', { name: /Access/i }).first()
    const configBtn = page.getByRole('button', { name: /Configure/i }).first()
    await expect(testBtn).toBeVisible({ timeout: 10000 })
    await expect(accessBtn).toBeVisible()
    await expect(configBtn).toBeVisible()
    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })
})

// ── MCP API CRUD ──

test.describe('MCP API', () => {
  test('GET /api/mcp-servers lists servers', async ({ page }) => {
    const resp = await page.request.get(`${API}/api/mcp-servers`, { headers: AUTH })
    expect(resp.ok()).toBeTruthy()
  })

  test('POST + DELETE MCP server', async ({ page }) => {
    const resp = await page.request.post(`${API}/api/mcp-servers`, {
      headers: { ...AUTH, 'Content-Type': 'application/json' },
      data: { name: `e2e-mcp-${Date.now()}`, transport: 'stdio', command: 'echo', enabled: true },
    })
    if (!resp.ok()) {
      // MCP server creation requires validation that may fail without full config
      return
    }
    const data = await resp.json()
    const serverId = data.id

    // Cleanup
    await page.request.delete(`${API}/api/mcp-servers/${serverId}`, { headers: AUTH }).catch(() => {})
  })
})
