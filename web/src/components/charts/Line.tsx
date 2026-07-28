import type { ReactNode } from 'react'
import { Legend, Line, LineChart as RechartsLineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'
import { axisStyle, CHART_COLORS } from '../../lib/recharts-theme'
import { ChartGrid, ChartTooltip } from './Shared'

// ── Types ──

export interface LineSeriesConfig {
  dataKey: string
  color?: string
  name?: string
  strokeWidth?: number
  dot?: boolean | Record<string, unknown>
  type?: 'monotone' | 'linear' | 'step' | 'natural'
}

export interface LineChartProps {
  data: Record<string, unknown>[]
  xKey?: string
  series: LineSeriesConfig[]
  margin?: { top: number; right: number; left: number; bottom: number }
  showLegend?: boolean
  showGrid?: boolean
  showTooltip?: boolean
  tooltipFormatter?: (value: ValueType | undefined, name: NameType | undefined) => ReactNode
  yAxisFormatter?: (value: number) => string
  xAxisInterval?: number
  legendVerticalAlign?: 'top' | 'middle' | 'bottom'
  legendHeight?: number
  children?: ReactNode
}

// ── Component ──

export default function LineChart({
  data,
  xKey = 'name',
  series,
  margin = { top: 8, right: 8, left: 0, bottom: 0 },
  showLegend = false,
  showGrid = true,
  showTooltip = true,
  tooltipFormatter,
  yAxisFormatter,
  xAxisInterval,
  legendVerticalAlign = 'top',
  legendHeight = 36,
  children,
}: LineChartProps) {
  return (
    <ResponsiveContainer width="100%" height="100%">
      <RechartsLineChart data={data} margin={margin}>
        {showGrid && <ChartGrid />}

        <XAxis dataKey={xKey} {...axisStyle} interval={xAxisInterval} />
        <YAxis {...axisStyle} tickFormatter={yAxisFormatter} />

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
            verticalAlign={legendVerticalAlign}
            height={legendHeight}
            formatter={(value: string) => <span className="text-sm text-surface-700">{value}</span>}
          />
        )}

        {series.map((s) => (
          <Line
            key={s.dataKey}
            type={s.type ?? 'monotone'}
            dataKey={s.dataKey}
            stroke={s.color ?? CHART_COLORS.brand}
            strokeWidth={s.strokeWidth ?? 2}
            dot={s.dot ?? false}
            name={s.name ?? s.dataKey}
          />
        ))}

        {children}
      </RechartsLineChart>
    </ResponsiveContainer>
  )
}
