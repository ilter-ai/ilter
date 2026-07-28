/**
 * Display formatting utilities.
 *
 * Centralises all number-to-string formatting so every view shows
 * consistent decimal counts, currency formats, and unit suffixes.
 */

// ── Latency / Duration ──

/**
 * Format a millisecond value for display:
 * - ≥ 1000 ms  → seconds with 2 decimals (e.g. 4.39 s)
 * - ≥ 10 ms    → ms with 1 decimal  (e.g. 234.5 ms)
 * - < 10 ms    → ms with 2 decimals (e.g. 0.54 ms)
 */
export function formatMs(ms: number): string {
  if (!Number.isFinite(ms)) return '-'
  if (ms >= 1000) return `${(ms / 1000).toFixed(2)}s`
  if (ms >= 10) return `${ms.toFixed(1)}ms`
  return `${ms.toFixed(2)}ms`
}

// ── Cost / Currency ──

/**
 * Format a dollar cost amount for display.
 *
 * - ≥ 1 000   → thousands with 1 decimal + "k" suffix  (e.g. $12.3k)
 * - ≥ 1       → 2 decimals   (e.g. $42.38)
 * - ≥ 0.01    → 4 decimals   (e.g. $0.1234)
 * - < 0.01    → 6 decimals   (e.g. $0.000123)
 * - 0         → $0.00
 */
export function formatCost(value: number | undefined | null): string {
  if (value == null || !Number.isFinite(value)) return '-'
  if (value === 0) return '$0.00'
  if (value >= 1000) return `$${(value / 1000).toFixed(1)}k`
  if (value >= 1) return `$${value.toFixed(2)}`
  if (value >= 0.01) return `$${value.toFixed(4)}`
  return `$${value.toFixed(6)}`
}

// ── Percentages ──

/**
 * Format a ratio (0–1 or 0–100) as a percentage string.
 * If `alreadyDecimal` is false (default), the input is treated as 0–100.
 */
export function formatPercent(value: number | undefined | null, decimals = 1): string {
  if (value == null || !Number.isFinite(value)) return '-'
  return `${value.toFixed(decimals)}%`
}

// ── General numbers ──

/**
 * Format a number with locale-aware thousands separators.
 */
export function formatNumber(value: number | undefined | null): string {
  if (value == null || !Number.isFinite(value)) return '-'
  return value.toLocaleString('en-US')
}

/**
 * Compact number (e.g. 1 234 → "1.2k", 1 234 567 → "1.2M").
 */
export function formatCompact(value: number | undefined | null): string {
  if (value == null || !Number.isFinite(value)) return '-'
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`
  if (value >= 1_000) return `${(value / 1_000).toFixed(1)}k`
  return String(value)
}

// ── Relative time ──

const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto', style: 'narrow' })

/**
 * Format an ISO date string relative to now (past or future).
 * e.g. "in 5m", "3h ago", "just now".
 */
export function formatRelativeTime(iso: string): string {
  const ms = new Date(iso).getTime()
  if (Number.isNaN(ms)) return '—'
  const diffSec = (ms - Date.now()) / 1000
  const absSec = Math.abs(diffSec)

  if (absSec < 5) return diffSec > 0 ? 'in a few seconds' : 'just now'

  const val = (unit: Intl.RelativeTimeFormatUnit, divisor: number) => rtf.format(Math.round(diffSec / divisor), unit)

  if (absSec < 60) return val('second', 1)
  if (absSec < 3600) return val('minute', 60)
  if (absSec < 86400) return val('hour', 3600)
  return val('day', 86400)
}

// ── Feature keys ──

// The single canonical feature_key → display name map. Keep in sync with the
// keys features/handler.go's bootFeatures() actually returns — anything not
// listed here falls back to a Title Cased version of the raw key.
const FEATURE_LABELS: Record<string, string> = {
  pii: 'PII Guard',
  smart_router: 'Smart Router',
  loop_detection: 'Loop Detection',
  semantic_cache: 'Semantic Cache',
  rate_limit: 'Rate Limiting',
  budget: 'Budget Enforcement',
  guardrails: 'Guardrails',
  mcp: 'MCP Gateway',
  openapi: 'OpenAPI Tools',
}

/** Human-readable label for a feature_key (e.g. "loop_detection" → "Loop Detection"). */
export function featureLabel(key: string): string {
  return FEATURE_LABELS[key] ?? key.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())
}
