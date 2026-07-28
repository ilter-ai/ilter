import { logger } from './logger'

const TOKEN_STORAGE_KEY = 'ilter_admin_token'

export function getToken(): string | null {
  if (typeof window !== 'undefined') {
    const urlParams = new URLSearchParams(window.location.search)
    const urlToken = urlParams.get('token')
    if (urlToken) {
      setStoredToken(urlToken)
      const url = new URL(window.location.href)
      url.searchParams.delete('token')
      window.history.replaceState({}, '', url.toString())
      return urlToken
    }

    const stored = sessionStorage.getItem(TOKEN_STORAGE_KEY)
    if (stored) return stored
  }

  return null
}

function setStoredToken(token: string): void {
  try {
    sessionStorage.setItem(TOKEN_STORAGE_KEY, token)
  } catch (err) {
    // quota / private mode - read-only mode
    logger.error('Token storage error', err)
  }
}

export function clearToken(): void {
  sessionStorage.removeItem(TOKEN_STORAGE_KEY)
}

export function getAuthHeaders(): Record<string, string> {
  const token = getToken()
  if (!token) return {}
  return { Authorization: `Bearer ${token}` }
}

const API_BASE = '/api'

export async function login(token: string): Promise<boolean> {
  const res = await fetch(`${API_BASE}/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token }),
  })
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error?.message || 'Login failed')
  }
  setStoredToken(token)
  return true
}

export async function checkAuth(): Promise<boolean> {
  if (typeof window === 'undefined') return false

  const params = new URLSearchParams(window.location.search)
  const hasToken = getToken() !== null || params.has('token')

  if (!hasToken) return false

  try {
    const res = await fetch(`${API_BASE}/stats`, {
      headers: getAuthHeaders(),
    })
    return res.ok
  } catch (err) {
    // Network / auth / quota errors - gracefully fail closed
    logger.error('Auth check error', err)
    return false
  }
}
