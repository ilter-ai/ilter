import { expect, test } from '@playwright/test'

test.describe('Auth: protected routes reject unauthenticated', () => {
  test('navigate to /overview/ without token shows auth error', async ({ page }) => {
    const resp = await page.goto('/overview/', { waitUntil: 'domcontentloaded' })
    expect(resp?.status()).toBe(401)
    const body = await page.locator('body').innerText()
    expect(body).toContain('unauthorized')
  })

  test('navigate to /overview/ with invalid token shows auth error', async ({ page }) => {
    const resp = await page.goto('/overview/?token=bad-token', { waitUntil: 'domcontentloaded' })
    expect(resp?.status()).toBe(401)
    const body = await page.locator('body').innerText()
    expect(body).toContain('unauthorized')
  })
})
