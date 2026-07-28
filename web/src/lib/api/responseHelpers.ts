/**
 * Response extraction helpers for API functions.
 * Reduces boilerplate in API modules by providing consistent field extraction logic.
 */

/**
 * Extract array field from API response with fallback to empty array
 *
 * @example
 * const res = await request({ items: [...], total: 5 });
 * const items = extractArray(res, 'items'); // returns [...] or []
 */
export function extractArray<T = unknown>(response: unknown, fieldName: string, fallback?: T[]): T[] {
  if (!response || typeof response !== 'object') return fallback || []
  const value = (response as Record<string, unknown>)[fieldName]
  return Array.isArray(value) ? value : fallback || []
}

/**
 * Extract single field from API response with fallback
 *
 * @example
 * const res = await request({ user: { id: '123', name: 'John' } });
 * const user = extractField(res, 'user'); // returns { id: '123', name: 'John' }
 */
export function extractField<T = unknown>(response: unknown, fieldName: string, fallback?: T): T {
  if (!response || typeof response !== 'object') return fallback as T
  const value = (response as Record<string, unknown>)[fieldName]
  return value !== undefined ? (value as T) : (fallback as T)
}

/**
 * Extract multiple fields from response into object
 *
 * @example
 * const res = await request({ user: {...}, total: 5, page: 1 });
 * const extracted = extractFields(res, ['user', 'total']); // returns { user, total }
 */
export function extractFields<T extends Record<string, unknown>>(response: unknown, fieldNames: string[]): Partial<T> {
  const result: Record<string, unknown> = {}
  for (const field of fieldNames) {
    if (response && typeof response === 'object' && field in response) {
      result[field] = (response as Record<string, unknown>)[field]
    }
  }
  return result as Partial<T>
}
