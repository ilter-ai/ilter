import type { ReactNode } from 'react'
import { CartesianGrid } from 'recharts'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'

// ── CartesianGrid style ──
export function ChartGrid() {
  return <CartesianGrid strokeDasharray="3 3" stroke="#E2E8F0" vertical={false} />
}

// ── Custom tooltip props ──
export interface ChartTooltipProps {
  active?: boolean
  payload?: ReadonlyArray<Record<string, unknown>>
  label?: string | number
  formatter?: (value: ValueType | undefined, name: NameType | undefined) => ReactNode
}

// ── Custom tooltip (matches design system) ──
export function ChartTooltip({ active, payload, label, formatter }: ChartTooltipProps) {
  if (!active || !payload || payload.length === 0) return null

  return (
    <div className="rounded-lg border border-surface-200 bg-white px-3 py-2 shadow-lg text-xs">
      <p className="font-medium text-surface-700 mb-1">{String(label ?? '')}</p>
      {payload.map((entry: Record<string, unknown>, idx: number) => (
        <p key={idx} className="flex items-center gap-2 text-surface-600">
          <span className="inline-block w-2 h-2 rounded-full" style={{ backgroundColor: entry.color as string }} />
          <span>{String(entry.name)}: </span>
          <span className="font-medium text-surface-900">
            {formatter
              ? formatter(entry.value as ValueType, entry.name as NameType)
              : typeof entry.value === 'number'
                ? entry.value.toLocaleString()
                : String(entry.value)}
          </span>
        </p>
      ))}
    </div>
  )
}
