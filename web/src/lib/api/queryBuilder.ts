/**
 * Build URL query string from parameter object.
 * Handles null/undefined filtering, type conversion, and proper encoding.
 *
 * @param params Object with query parameters (null/undefined values excluded)
 * @returns Query string with '?' prefix if params present, empty string otherwise
 *
 * @example
 * buildQueryString({ page: 1, limit: 10 }) // "?page=1&limit=10"
 * buildQueryString({ name: "foo bar" }) // "?name=foo+bar" (or foo%20bar)
 * buildQueryString({ tag: null, page: 1 }) // "?page=1" (null filtered out)
 */
export function buildQueryString(params?: Record<string, unknown>): string {
  if (!params || typeof params !== 'object') return ''

  const search = new URLSearchParams()

  for (const [key, value] of Object.entries(params)) {
    // Skip null, undefined, empty string
    if (value == null || value === '') continue

    // Convert to string and add to search params
    search.set(key, String(value))
  }

  const qs = search.toString()
  return qs ? `?${qs}` : ''
}

/**
 * Build URL with query string appended.
 *
 * @param path Base URL path (e.g., '/jobs')
 * @param params Query parameters object
 * @returns Full URL with query string
 *
 * @example
 * buildUrl('/jobs/123/runs', { limit: 10, offset: 0 })
 * // Returns: '/jobs/123/runs?limit=10&offset=0'
 */
export function buildUrl(path: string, params?: Record<string, unknown>): string {
  return path + buildQueryString(params)
}
