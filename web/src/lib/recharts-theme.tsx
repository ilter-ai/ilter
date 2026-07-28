/** Recharts theme configuration matching the ILTER admin design system. */

// ── Color palette mapped from design tokens ──
export const CHART_COLORS = {
  brand: '#3B82F6',
  brandLight: '#93C5FD',
  success: '#10B981',
  warning: '#F59E0B',
  error: '#EF4444',
  info: '#6366F1',
  surface: '#94A3B8',
  surface300: '#CBD5E1',
  surface200: '#E2E8F0',
} as const

// Sequential palette for pie/bar charts (10 colors)
export const PALETTE = [
  '#3B82F6', // brand-500
  '#10B981', // success
  '#F59E0B', // warning
  '#6366F1', // info
  '#EF4444', // error
  '#8B5CF6', // violet
  '#EC4899', // pink
  '#14B8A6', // teal
  '#F97316', // orange
  '#06B6D4', // cyan
] as const

// ── Shared chart defaults ──
export const chartDefaults = {
  margin: { top: 8, right: 8, left: 0, bottom: 0 },
}

// ── Axis styles ──
export const axisStyle = {
  tick: { fontSize: 11, fill: '#94A3B8', fontFamily: 'JetBrains Mono, monospace' },
  axisLine: { stroke: '#E2E8F0' },
  tickLine: { stroke: '#E2E8F0' },
}
