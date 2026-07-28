import type { ReactNode } from 'react'
import {
  Bar,
  Legend,
  BarChart as RechartsBarChart,
  ReferenceLine,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'
import { axisStyle, CHART_COLORS } from '../../lib/recharts-theme'
import { ChartGrid, ChartTooltip } from './Shared'

// ── Types ──

export interface BarSeriesConfig {
  dataKey: string
  color?: string
  name?: string
  radius?: [number, number, number, number]
  maxBarSize?: number
  stackId?: string
}

export interface BarChartProps {
  data: Record<string, unknown>[]
  xKey?: string
  yKey?: string
  series: BarSeriesConfig[]
  layout?: 'vertical' | 'horizontal'
  margin?: { top: number; right: number; left: number; bottom: number }
  referenceLine?: {
    y: number
    color?: string
    label?: string
    strokeDasharray?: string
  }
  showLegend?: boolean
  showGrid?: boolean
  showTooltip?: boolean
  tooltipFormatter?: (value: ValueType | undefined, name: NameType | undefined) => ReactNode
  yAxisFormatter?: (value: number) => string
  xAxisInterval?: number
  yAxisDomain?: [number | string, number | string]
  xAxisType?: 'number' | 'category'
  yAxisType?: 'number' | 'category'
  xAxisWidth?: number
  children?: ReactNode
}

// ── Component ──

export default function BarChart({
  data,
  xKey = 'name',
  yKey,
  series,
  layout = 'horizontal',
  margin = { top: 8, right: 8, left: 0, bottom: 0 },
  referenceLine,
  showLegend = false,
  showGrid = true,
  showTooltip = true,
  tooltipFormatter,
  yAxisFormatter,
  yAxisDomain,
  xAxisType = 'category',
  yAxisType = 'number',
  xAxisWidth,
  xAxisInterval,
  children,
}: BarChartProps) {
  const isVertical = layout === 'vertical'

  return (
    <ResponsiveContainer width="100%" height="100%">
      <RechartsBarChart data={data} margin={margin} layout={layout} barCategoryGap={isVertical ? '20%' : undefined}>
        {showGrid && <ChartGrid />}

        {isVertical ? (
          <>
            <XAxis type={xAxisType || 'number'} {...axisStyle} tickFormatter={yAxisFormatter} />
            <YAxis type={yAxisType || 'category'} dataKey={yKey || xKey} {...axisStyle} width={xAxisWidth ?? 100} />
          </>
        ) : (
          <>
            <XAxis dataKey={xKey} {...axisStyle} type={xAxisType} interval={xAxisInterval} />
            <YAxis {...axisStyle} type={yAxisType} tickFormatter={yAxisFormatter} domain={yAxisDomain} />
          </>
        )}

        {referenceLine && (
          <ReferenceLine
            y={referenceLine.y}
            stroke={referenceLine.color ?? CHART_COLORS.error}
            strokeDasharray={referenceLine.strokeDasharray ?? '6 3'}
            label={
              referenceLine.label
                ? {
                    value: referenceLine.label,
                    position: 'right',
                    fill: referenceLine.color ?? CHART_COLORS.error,
                    fontSize: 11,
                    fontFamily: 'JetBrains Mono, monospace',
                  }
                : undefined
            }
          />
        )}

        {showTooltip && (
          <Tooltip
            content={({ active, payload, label }: Record<string, unknown>) => (
              <ChartTooltip
                active={active as boolean}
                payload={payload as ReadonlyArray<Record<string, unknown>>}
                label={label as string | number}
                formatter={tooltipFormatter}
              />
            )}
          />
        )}

        {showLegend && (
          <Legend
            wrapperStyle={{ fontSize: 12, fontFamily: 'Inter, sans-serif' }}
            formatter={(value: string) => <span className="text-surface-700">{value}</span>}
          />
        )}

        {series.map((s) => (
          <Bar
            key={s.dataKey}
            dataKey={s.dataKey}
            fill={s.color ?? CHART_COLORS.brand}
            radius={s.radius ?? [3, 3, 0, 0]}
            maxBarSize={s.maxBarSize ?? 32}
            name={s.name ?? s.dataKey}
            stackId={s.stackId}
          />
        ))}

        {children}
      </RechartsBarChart>
    </ResponsiveContainer>
  )
}
