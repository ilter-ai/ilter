import { expect, type Page, test } from '@playwright/test'
import { ADMIN_TOKEN } from './helpers'

const BASE = 'http://localhost:9191'

test.describe('MCP OAuth Authorize Page', () => {
  test('shows error when required params missing', async ({ page }) => {
    await page.goto(`${BASE}/authorize/?token=${ADMIN_TOKEN}`, { waitUntil: 'load', timeout: 15000 })
    // Wait for the error element to become visible (JS hides loading, shows error)
    await page.waitForFunction(
      (text) => {
        const el = document.getElementById('error')
        return el && !el.classList.contains('hidden') && el.textContent?.includes(text)
      },
      'Invalid authorization request',
      { timeout: 10000 },
    )

    await expect(page.getByText('Authorize MCP Client')).toBeVisible()
  })

  async function gotoAuthorize(page: Page, params: URLSearchParams) {
    const url = `${BASE}/authorize/?${params}&token=${ADMIN_TOKEN}`
    await page.goto(url, { waitUntil: 'domcontentloaded' })
    await page
      .waitForFunction(() => document.getElementById('loading')?.classList.contains('hidden'), { timeout: 5000 })
      .catch(() => {})
    await page.waitForTimeout(500)
  }

  test('renders consent form with valid params', async ({ page }) => {
    const params = new URLSearchParams({
      request_id: 'test-req-001',
      client_id: 'vscode-mcp-client',
      redirect_uri: 'http://localhost:8181/callback',
    })
    await gotoAuthorize(page, params)

    await expect(page.getByText('Authorize MCP Client')).toBeVisible()
    await expect(page.getByText('This application is requesting access')).toBeVisible()
    await expect(page.getByText('vscode-mcp-client')).toBeVisible()

    await expect(page.getByText('Create new MCP API key')).toBeVisible()
    await expect(page.getByText('Use existing API key')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Allow' })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Cancel' })).toBeVisible()
  })

  test('existing key field toggles on radio selection', async ({ page }) => {
    const params = new URLSearchParams({
      request_id: 'test-req-002',
      client_id: 'vscode-mcp-client',
      redirect_uri: 'http://localhost:8181/callback',
    })
    await gotoAuthorize(page, params)

    const createRadio = page.getByRole('radio', { name: /Create new/ })
    const existingRadio = page.getByRole('radio', { name: /Use existing/ })
    await expect(createRadio).toBeChecked()

    await existingRadio.click()
    await expect(page.locator('#existing-key-field')).toBeVisible()

    await createRadio.click()
    await expect(page.locator('#existing-key-field')).not.toBeVisible()
  })

  test('cancel button shows cancellation message', async ({ page }) => {
    const params = new URLSearchParams({
      request_id: 'test-req-003',
      client_id: 'vscode-mcp-client',
      redirect_uri: 'http://localhost:8181/callback',
    })
    await gotoAuthorize(page, params)

    await page.getByRole('button', { name: 'Cancel' }).click()
    await page.waitForTimeout(500)

    await expect(page.getByText('Authorization Cancelled')).toBeVisible()
  })

  test('without auth token returns 401', async ({ page }) => {
    const resp = await page.goto(`${BASE}/authorize/`, { waitUntil: 'domcontentloaded' })
    expect(resp?.status()).toBe(401)
  })
})
