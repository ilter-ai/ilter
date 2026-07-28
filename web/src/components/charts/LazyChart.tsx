import { lazy, Suspense, useMemo } from 'react'
import type { AreaChartProps } from './Area'
import type { BarChartProps } from './Bar'
import { ChartSkeleton } from './ChartSkeleton'
import type { LineChartProps } from './Line'
import type { PieChartProps } from './Pie'

// ── Registry ──

const registry = {
  area: lazy(() => import('./Area')),
  bar: lazy(() => import('./Bar')),
  line: lazy(() => import('./Line')),
  pie: lazy(() => import('./Pie')),
} as const

type ChartType = keyof typeof registry

type ChartPropsMap = {
  area: AreaChartProps
  bar: BarChartProps
  line: LineChartProps
  pie: PieChartProps
}

// ── Helpers ──

function isRecord(v: unknown): v is Record<string, unknown> {
  return v !== null && typeof v === 'object' && !Array.isArray(v)
}

// ── Props ──

export type LazyChartProps<T extends ChartType = ChartType> = {
  type: T
  data: Record<string, unknown>[]
  height?: number
  className?: string
  fallback?: React.ReactNode
} & Omit<ChartPropsMap[T], 'data'>

// ── Component ──

export default function LazyChart<T extends ChartType = ChartType>({
  type,
  data,
  height = 320,
  className,
  fallback,
  ...rest
}: LazyChartProps<T>) {
  const ChartComponent = registry[type] as React.ComponentType<object>
  const safeData = useMemo(() => data.filter(isRecord), [data])

  const chartProps = { ...rest, data: safeData } as ChartPropsMap[T]

  return (
    <div className={className} style={{ height, minHeight: height, position: 'relative' }}>
      <Suspense fallback={fallback ?? <ChartSkeleton height={height} />}>
        <ChartComponent {...chartProps} />
      </Suspense>
    </div>
  )
}
