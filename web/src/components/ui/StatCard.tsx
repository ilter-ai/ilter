import { cn } from '../../lib/utils'
import { ChevronDown, ChevronUp } from '../ui/icons'

interface StatCardProps {
  title: string
  value: string | number
  description?: string
  icon?: React.ReactNode
  trend?: {
    value: number
    positive?: boolean
  }
  className?: string
}

export function StatCard({ title, value, description, icon, trend, className }: StatCardProps) {
  return (
    <div className={cn('rounded-xl border border-surface-200 bg-white p-6 shadow-card', className)}>
      <div className="flex items-start justify-between">
        <div className="space-y-1">
          <p className="text-sm font-medium text-surface-500">{title}</p>
          <p className="text-2xl font-bold text-surface-900">{value}</p>
          {description && <p className="text-xs text-surface-400">{description}</p>}
        </div>
        {icon && <div className="rounded-lg bg-brand-50 p-2.5 text-brand-600">{icon}</div>}
      </div>
      {trend && (
        <div className="mt-4 flex items-center gap-1.5">
          <span
            className={cn(
              'inline-flex items-center gap-0.5 text-sm font-medium',
              trend.positive ? 'text-success' : 'text-error',
            )}
          >
            {trend.positive ? <ChevronUp size={14} /> : <ChevronDown size={14} />}
            {Math.abs(trend.value)}%
          </span>
          <span className="text-xs text-surface-400">vs last period</span>
        </div>
      )}
    </div>
  )
}
