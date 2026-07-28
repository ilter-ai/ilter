import { expect, type Page, test } from '@playwright/test'
import { ADMIN_TOKEN, captureErrors, checkCrashIndicators, verifyMainContent } from './helpers'

const AUTH = { Authorization: `Bearer ${ADMIN_TOKEN}` }
const API = 'http://localhost:9191'
const PATH = `/access/keys/?token=${ADMIN_TOKEN}`

// ── UI Component Tests ──

test.describe('Access / API Keys page', () => {
  async function setup(page: Page) {
    const errors = captureErrors(page)
    await page.goto(PATH, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    return errors
  }

  test('shows 4 tabs with API Keys selected', async ({ page }) => {
    const errors = await setup(page)

    // Tablist contains exactly 4 tabs
    const tablist = page.getByRole('tablist')
    await expect(tablist).toBeVisible({ timeout: 10000 })
    const tabs = tablist.getByRole('tab')
    await expect(tabs).toHaveCount(4)

    // Exact tab labels (from AccessManagementView.tsx TABS array)
    await expect(tabs.nth(0)).toHaveText('API Keys')
    await expect(tabs.nth(1)).toHaveText('Users')
    await expect(tabs.nth(2)).toHaveText('Groups')
    await expect(tabs.nth(3)).toHaveText('Tool Permissions')

    // First tab (keys) is selected by default for /access/keys/ path
    await expect(tabs.nth(0)).toHaveAttribute('aria-selected', 'true')
    await expect(tabs.nth(1)).toHaveAttribute('aria-selected', 'false')
    await expect(tabs.nth(2)).toHaveAttribute('aria-selected', 'false')
    await expect(tabs.nth(3)).toHaveAttribute('aria-selected', 'false')

    await checkCrashIndicators(page, errors)
    await verifyMainContent(page, errors)
    expect(errors).toEqual([])
  })

  test('shows 4 stat cards with correct labels', async ({ page }) => {
    const errors = await setup(page)

    // StatCard title labels rendered by StatCard component
    await expect(page.getByText('Total Keys', { exact: true })).toBeVisible({ timeout: 10000 })
    await expect(page.getByText('Enabled', { exact: true })).toBeVisible()
    await expect(page.getByText('Monthly Budget', { exact: true })).toBeVisible()
    await expect(page.getByText('Rate Limit (RPM)', { exact: true })).toBeVisible()

    // Values from seeded "test" key: total=1, enabled=1, budget=100, rpm=200
    await expect(page.getByText('$100.00', { exact: true }).first()).toBeVisible()

    await checkCrashIndicators(page, errors)
    await verifyMainContent(page, errors)
    expect(errors).toEqual([])
  })

  test('shows Create API Key and Export buttons', async ({ page }) => {
    const errors = await setup(page)

    // Create API Key — primary button with Plus icon
    await expect(page.getByRole('button', { name: /Create API Key/i })).toBeVisible({ timeout: 10000 })

    // Export — outline button with Download icon, visible because filteredKeys.length > 0
    await expect(page.getByRole('button', { name: /Export/i })).toBeVisible()

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('search textbox has correct placeholder', async ({ page }) => {
    const errors = await setup(page)

    // FilterBar renders Input with placeholder "Search by name..."
    const searchInput = page.getByPlaceholder('Search by name...')
    await expect(searchInput).toBeVisible({ timeout: 10000 })
    await expect(searchInput).toHaveAttribute('type', 'text')

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('shows group and user filter selects', async ({ page }) => {
    const errors = await setup(page)

    // Group <select> — native HTML select rendered in ApiKeysManager
    const groupSelect = page.locator('select').first()
    await expect(groupSelect).toBeVisible({ timeout: 10000 })
    await expect(groupSelect.locator('option')).toHaveCount(4)
    await expect(groupSelect.locator('option').first()).toHaveText('All Groups')

    // User <select>
    const userSelect = page.locator('select').nth(1)
    await expect(userSelect).toBeVisible()
    await expect(userSelect.locator('option')).toHaveCount(3)
    await expect(userSelect.locator('option').first()).toHaveText('All Users')

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('table has 9 column headers from source', async ({ page }) => {
    const errors = await setup(page)

    // From ApiKeysManager.tsx thead: Name, Group, User, Budget USD, Budget Tokens, RPM / TPM, Allowed Models, Status, Actions
    const headers = page.locator('th')
    await expect(headers).toHaveCount(9)

    const expectedHeaders = [
      'Name',
      'Group',
      'User',
      'Budget USD',
      'Budget Tokens',
      'RPM / TPM',
      'Allowed Models',
      'Status',
      'Actions',
    ]
    for (let i = 0; i < expectedHeaders.length; i++) {
      await expect(headers.nth(i)).toHaveText(expectedHeaders[i])
    }

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('"test" key row shows Active status with correct values', async ({ page }) => {
    const errors = await setup(page)

    // Find the seeded key row — filter by exact name "test" and budget $100
    const testRow = page
      .getByRole('row')
      .filter({ has: page.getByText('Active') })
      .filter({ hasText: '$100.00' })
      .first()
    await expect(testRow).toBeVisible({ timeout: 10000 })

    // Status badge shows "Active" for enabled = true
    await expect(testRow.getByText('Active')).toBeVisible()

    // No group assigned — shows "—"
    await expect(testRow.getByText('—').first()).toBeVisible()

    // No models restriction — shows "*"
    await expect(testRow.getByText('*', { exact: true })).toBeVisible()

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('action buttons per row: Disable, Edit, Delete', async ({ page }) => {
    const errors = await setup(page)

    const testRow = page
      .getByRole('row')
      .filter({ has: page.getByText('Active') })
      .filter({ hasText: '$100.00' })
      .first()
    await expect(testRow).toBeVisible({ timeout: 10000 })

    // Disable button (shown because enabled = true)
    await expect(testRow.getByRole('button', { name: /Disable/i })).toBeVisible()

    // Edit button with title="Edit key" and Edit3 icon
    await expect(testRow.getByRole('button', { name: /Edit key/i })).toBeVisible()

    // Delete button (destructive variant)
    await expect(testRow.getByRole('button', { name: /Delete/i })).toBeVisible()

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })
})

// ── Dashboard Stats ──

test.describe('Dashboard Stats', () => {
  test('GET /api/stats returns stats with expected fields', async ({ page }) => {
    const resp = await page.request.get(`${API}/api/stats`, { headers: AUTH })
    expect(resp.ok()).toBeTruthy()
    const data = await resp.json()
    expect(data).toHaveProperty('total_requests')
    expect(data).toHaveProperty('total_cost')
    expect(data).toHaveProperty('total_keys')
    expect(data).toHaveProperty('daily_stats')
  })

  test('GET /api/analytics/overview returns overview', async ({ page }) => {
    const resp = await page.request.get(`${API}/api/analytics/overview`, { headers: AUTH })
    expect(resp.ok()).toBeTruthy()
    const data = await resp.json()
    expect(typeof data).toBe('object')
  })
})

// ── API Keys ──

test.describe('API Keys', () => {
  test('GET /api/api-keys lists seeded keys', async ({ page }) => {
    const resp = await page.request.get(`${API}/api/api-keys`, { headers: AUTH })
    expect(resp.ok()).toBeTruthy()
    const data = await resp.json()
    expect(data.api_keys).toBeDefined()
    expect(Array.isArray(data.api_keys)).toBeTruthy()
    expect(data.api_keys.length).toBeGreaterThanOrEqual(1)
  })

  test('POST + DELETE key lifecycle', async ({ page }) => {
    // Create
    const createResp = await page.request.post(`${API}/api/api-keys`, {
      headers: { ...AUTH, 'Content-Type': 'application/json' },
      data: { name: `e2e-test-key-${Date.now()}` },
    })
    expect(createResp.ok()).toBeTruthy()
    const created = await createResp.json()
    expect(created).toHaveProperty('id')
    expect(created).toHaveProperty('key') // plaintext on create
    const keyId = created.id

    // Delete
    const delResp = await page.request.delete(`${API}/api/api-keys/${keyId}`, { headers: AUTH })
    expect(delResp.ok()).toBeTruthy()

    // Verify gone
    const listResp = await page.request.get(`${API}/api/api-keys`, { headers: AUTH })
    const list = await listResp.json()
    expect(list.api_keys.find((k: { id: string }) => k.id === keyId)).toBeUndefined()
  })

  test('POST + PATCH key name', async ({ page }) => {
    // Create
    const createResp = await page.request.post(`${API}/api/api-keys`, {
      headers: { ...AUTH, 'Content-Type': 'application/json' },
      data: { name: `e2e-rename-${Date.now()}` },
    })
    expect(createResp.ok()).toBeTruthy()
    const created = await createResp.json()
    const keyId = created.id
    const newName = `e2e-renamed-${Date.now()}`

    const putResp = await page.request.put(`${API}/api/api-keys/${keyId}`, {
      headers: { ...AUTH, 'Content-Type': 'application/json' },
      data: { name: newName },
    })
    expect(putResp.ok()).toBeTruthy()

    // Verify
    const listResp = await page.request.get(`${API}/api/api-keys`, { headers: AUTH })
    const list = await listResp.json()
    expect(list.api_keys.find((k: { id: string }) => k.id === keyId)?.name).toBe(newName)

    // Cleanup
    await page.request.delete(`${API}/api/api-keys/${keyId}`, { headers: AUTH })
  })
})

// ── Interaction Tests ──

test.describe('Interaction Tests', () => {
  async function setup(page: Page) {
    const errors = captureErrors(page)
    await page.goto(PATH, { waitUntil: 'domcontentloaded' })
    await page.waitForFunction(() => !document.getElementById('auth-spinner'), { timeout: 10000 }).catch(() => {})
    await page.waitForTimeout(2000)
    return errors
  }

  test('clicking Users tab selects it', async ({ page }) => {
    const errors = await setup(page)

    const tabs = page.getByRole('tablist').getByRole('tab')
    await expect(tabs.nth(0)).toHaveAttribute('aria-selected', 'true')
    await expect(tabs.nth(1)).toHaveAttribute('aria-selected', 'false')

    await tabs.nth(1).click()
    await expect(tabs.nth(0)).toHaveAttribute('aria-selected', 'false')
    await expect(tabs.nth(1)).toHaveAttribute('aria-selected', 'true')

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('clicking Groups tab selects it', async ({ page }) => {
    const errors = await setup(page)

    const tabs = page.getByRole('tablist').getByRole('tab')
    await expect(tabs.nth(2)).toHaveAttribute('aria-selected', 'false')

    await tabs.nth(2).click()
    await expect(tabs.nth(2)).toHaveAttribute('aria-selected', 'true')

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('clicking back to API Keys re-selects it', async ({ page }) => {
    const errors = await setup(page)

    const tabs = page.getByRole('tablist').getByRole('tab')
    // Start at API Keys (selected) → Users
    await tabs.nth(1).click()
    await expect(tabs.nth(1)).toHaveAttribute('aria-selected', 'true')
    // Groups
    await tabs.nth(2).click()
    await expect(tabs.nth(2)).toHaveAttribute('aria-selected', 'true')
    // Back to API Keys
    await tabs.nth(0).click()
    await expect(tabs.nth(0)).toHaveAttribute('aria-selected', 'true')

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('Create API Key button opens dialog with correct heading', async ({ page }) => {
    const errors = await setup(page)

    await page.getByRole('button', { name: /Create API Key/i }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible({ timeout: 5000 })
    await expect(dialog.getByRole('heading', { name: /Create API Key/i })).toBeVisible()

    // Close dialog
    await page.keyboard.press('Escape')
    await expect(dialog).not.toBeVisible()

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('key row visible with Active status', async ({ page }) => {
    const errors = await setup(page)

    const testRow = page.getByRole('row').filter({ hasText: 'test' }).first()
    await expect(testRow).toBeVisible({ timeout: 10000 })
    await expect(testRow.getByText('Active')).toBeVisible()

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('group filter combobox has correct options', async ({ page }) => {
    const errors = await setup(page)

    // Group combobox — first combobox in the filter bar
    const groupSelect = page.getByRole('combobox').first()
    await expect(groupSelect).toBeVisible({ timeout: 10000 })
    await expect(groupSelect.locator('option')).toHaveCount(4)
    await expect(groupSelect.locator('option').nth(0)).toHaveText('All Groups')
    await expect(groupSelect.locator('option').nth(1)).toHaveText('test')
    await expect(groupSelect.locator('option').nth(2)).toHaveText('engineering')
    await expect(groupSelect.locator('option').nth(3)).toHaveText('admin')

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })

  test('full round-trip: create API key via dialog and verify in table', async ({ page }) => {
    const errors = await setup(page)
    const keyName = `e2e-test-${Date.now()}`

    // 1. Click Create API Key button to open dialog
    await page.getByRole('button', { name: /Create API Key/i }).click()
    const dialog = page.getByRole('dialog')
    await expect(dialog).toBeVisible({ timeout: 5000 })
    await expect(dialog.getByRole('heading', { name: /Create API Key/i })).toBeVisible()

    // 2. Fill in key name
    await dialog.getByPlaceholder('e.g. production-api-key').fill(keyName)

    // 3. Set Monthly Budget to 50.00
    const budgetInput = dialog.getByRole('spinbutton').first()
    await budgetInput.fill('50')

    // 4. Click Create Key button
    const createBtn = dialog.getByRole('button', { name: /Create Key/i })
    await expect(createBtn).toBeEnabled()
    await createBtn.click()

    // 5. Wait for dialog to close and table to update
    await expect(dialog).not.toBeVisible({ timeout: 5000 })
    await page.waitForTimeout(500)

    // 6. Verify the new key appears in the table
    const keyRow = page.getByRole('row').filter({ hasText: keyName }).first()
    await expect(keyRow).toBeVisible({ timeout: 10000 })

    // 7. Verify its status is Active
    await expect(keyRow.getByText('Active')).toBeVisible()

    await checkCrashIndicators(page, errors)
    expect(errors).toEqual([])
  })
})

// ── Guardrails ──

test.describe('Guardrails', () => {
  test('GET /api/guardrails returns config', async ({ page }) => {
    const resp = await page.request.get(`${API}/api/guardrails`, { headers: AUTH })
    if (resp.status() === 404) {
      // Guardrails are optional — skip without failing
      return
    }
    expect(resp.ok()).toBeTruthy()
  })
})
