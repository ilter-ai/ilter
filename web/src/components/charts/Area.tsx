import type { ReactNode } from 'react'
import { useId } from 'react'
import {
  Area,
  Legend,
  AreaChart as RechartsAreaChart,
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

export interface AreaSeriesConfig {
  dataKey: string
  color?: string
  fill?: string
  fillOpacity?: number
  strokeWidth?: number
  name?: string
  type?: 'monotone' | 'linear' | 'step' | 'natural'
}

export interface GradientDef {
  id?: string
  color: string
  startOpacity?: number
  endOpacity?: number
}

export interface RefLineDef {
  y: number
  color?: string
  label?: string
  strokeDasharray?: string
}

export interface AreaChartProps {
  data: Record<string, unknown>[]
  xKey?: string
  series: AreaSeriesConfig[]
  margin?: { top: number; right: number; left: number; bottom: number }
  gradient?: GradientDef
  referenceLine?: RefLineDef
  showLegend?: boolean
  showGrid?: boolean
  showTooltip?: boolean
  tooltipFormatter?: (value: ValueType | undefined, name: NameType | undefined) => ReactNode
  yAxisFormatter?: (value: number) => string
  xAxisInterval?: number
  yAxisDomain?: [number | string, number | string]
  className?: string
  children?: ReactNode
}

// ── Component ──

export default function AreaChart({
  data,
  xKey = 'name',
  series,
  margin = { top: 8, right: 8, left: 0, bottom: 0 },
  gradient,
  referenceLine,
  showLegend = false,
  showGrid = true,
  showTooltip = true,
  tooltipFormatter,
  yAxisFormatter,
  yAxisDomain,
  xAxisInterval,
  children,
}: AreaChartProps) {
  const gradientId = useId()

  return (
    <ResponsiveContainer width="100%" height="100%">
      <RechartsAreaChart data={data} margin={margin}>
        {gradient && (
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={gradient.color} stopOpacity={gradient.startOpacity ?? 0.25} />
              <stop offset="100%" stopColor={gradient.color} stopOpacity={gradient.endOpacity ?? 0.02} />
            </linearGradient>
          </defs>
        )}

        {showGrid && <ChartGrid />}

        <XAxis dataKey={xKey} {...axisStyle} interval={xAxisInterval} />
        <YAxis {...axisStyle} tickFormatter={yAxisFormatter} domain={yAxisDomain} />

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

        {referenceLine && (
          <ReferenceLine
            y={referenceLine.y}
            stroke={referenceLine.color ?? CHART_COLORS.warning}
            strokeDasharray={referenceLine.strokeDasharray ?? '6 3'}
            label={
              referenceLine.label
                ? {
                    value: referenceLine.label,
                    position: 'right',
                    fill: referenceLine.color ?? CHART_COLORS.warning,
                    fontSize: 11,
                    fontFamily: 'JetBrains Mono, monospace',
                  }
                : undefined
            }
          />
        )}

        {series.map((s) => (
          <Area
            key={s.dataKey}
            type={s.type ?? 'monotone'}
            dataKey={s.dataKey}
            stroke={s.color ?? CHART_COLORS.brand}
            strokeWidth={s.strokeWidth ?? 2}
            fill={s.fill ?? (gradient ? `url(#${gradientId})` : (s.color ?? CHART_COLORS.brand))}
            fillOpacity={s.fillOpacity ?? (gradient ? 1 : 0.15)}
            name={s.name ?? s.dataKey}
          />
        ))}

        {children}
      </RechartsAreaChart>
    </ResponsiveContainer>
  )
}
