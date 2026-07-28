import type { ReactNode } from 'react'
import { useCallback } from 'react'
import { Pie, PieChart as RechartsPieChart, ResponsiveContainer, Sector, Tooltip } from 'recharts'
import type { NameType, ValueType } from 'recharts/types/component/DefaultTooltipContent'
import type { PieSectorShapeProps } from 'recharts/types/polar/Pie'
import { PALETTE } from '../../lib/recharts-theme'
import { ChartTooltip } from './Shared'

// ── Types ──

export interface PieSeriesConfig {
  dataKey?: string
  nameKey?: string
  innerRadius?: number
  outerRadius?: number
  paddingAngle?: number
  colors?: readonly string[]
}

export interface PieChartProps {
  data: Record<string, unknown>[]
  series: PieSeriesConfig
  margin?: { top: number; right: number; left: number; bottom: number }
  showTooltip?: boolean
  tooltipFormatter?: (value: ValueType | undefined, name: NameType | undefined) => ReactNode
  label?: (props: { name?: string; value?: number; percent?: number; [key: string]: unknown }) => ReactNode
  children?: ReactNode
}

// ── Component ──

export default function PieChart({
  data,
  series,
  margin = { top: 0, right: 0, left: 0, bottom: 0 },
  showTooltip = true,
  tooltipFormatter,
  label,
  children,
}: PieChartProps) {
  const {
    dataKey = 'value',
    nameKey = 'name',
    innerRadius = 60,
    outerRadius = 90,
    paddingAngle = 4,
    colors = PALETTE,
  } = series

  const coloredSector = useCallback(
    (props: PieSectorShapeProps) => <Sector {...props} fill={colors[props.index % colors.length]} />,
    [colors],
  )

  return (
    <ResponsiveContainer width="100%" height="100%">
      <RechartsPieChart margin={margin}>
        <Pie
          data={data}
          cx="50%"
          cy="50%"
          innerRadius={innerRadius}
          outerRadius={outerRadius}
          paddingAngle={paddingAngle}
          dataKey={dataKey}
          nameKey={nameKey}
          label={label as (props: unknown) => ReactNode}
          shape={coloredSector}
        />

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

        {children}
      </RechartsPieChart>
    </ResponsiveContainer>
  )
}
