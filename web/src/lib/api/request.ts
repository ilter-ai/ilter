import { clearToken, getAuthHeaders } from '../auth'

export class ApiError extends Error {
  status: number
  details?: unknown

  constructor(message: string, status: number, details?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.details = details
  }
}

const BASE_URL = '/api'

export async function request<T>(path: string, options?: RequestInit): Promise<T> {
  const url = `${BASE_URL}${path}`

  const authHeaders = getAuthHeaders()

  let res: Response
  try {
    res = await fetch(url, {
      headers: {
        'Content-Type': 'application/json',
        ...authHeaders,
        ...options?.headers,
      },
      ...options,
    })
  } catch (err) {
    const message =
      err instanceof TypeError
        ? 'Network error — unable to reach the server. Please check your connection.'
        : 'An unexpected error occurred while making the request.'
    throw new ApiError(message, 0, err instanceof Error ? err.message : String(err))
  }

  if (res.status === 401) {
    clearToken()
    const body = await res.json().catch(() => ({ error: { message: 'Unauthorized' } }))
    const errMsg = body.error?.message ?? body.message ?? 'Session expired. Please log in again.'
    const err = new ApiError(errMsg, 401, body)
    setTimeout(() => {
      window.location.replace('/login')
    }, 100)
    throw err
  }

  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: { message: res.statusText } }))
    const errMsg = body.error?.message ?? body.message ?? res.statusText
    throw new ApiError(errMsg, res.status, body)
  }

  if (res.status === 204) {
    return undefined as unknown as Promise<T>
  }

  return res.json() as Promise<T>
}
