import { cn } from '../../lib/utils'
import type { TimeRange } from './logsTime'

export function TimeRangeFilter({
  value,
  onChange,
  customFrom,
  customTo,
  onCustomFromChange,
  onCustomToChange,
  resetPage,
}: {
  value: TimeRange
  onChange: (range: TimeRange) => void
  customFrom: string
  customTo: string
  onCustomFromChange: (val: string) => void
  onCustomToChange: (val: string) => void
  resetPage?: () => void
}) {
  const handleChange = (range: TimeRange) => {
    onChange(range)
    resetPage?.()
  }

  const handleFrom = (val: string) => {
    onCustomFromChange(val)
    resetPage?.()
  }

  const handleTo = (val: string) => {
    onCustomToChange(val)
    resetPage?.()
  }

  return (
    <div className="flex items-center gap-2 flex-wrap">
      <span className="text-xs font-medium text-surface-500 mr-1">Time:</span>
      {(['1h', '24h', '7d', 'custom'] as TimeRange[]).map((range) => (
        <button
          key={range}
          onClick={() => handleChange(range)}
          className={cn(
            'text-xs px-3 py-1.5 rounded-lg border transition-colors font-medium',
            value === range
              ? 'bg-brand-50 text-brand-700 border-brand-200'
              : 'bg-white text-surface-500 border-surface-200 hover:border-surface-300',
          )}
        >
          {range === '1h' ? 'Last Hour' : range === '24h' ? 'Last 24h' : range === '7d' ? 'Last 7 Days' : 'Custom'}
        </button>
      ))}
      {value === 'custom' && (
        <div className="flex items-center gap-2 ml-1">
          <input
            type="datetime-local"
            value={customFrom}
            onChange={(e) => handleFrom(e.target.value)}
            className="rounded-lg border border-surface-300 bg-white px-2 py-1.5 text-xs text-surface-900 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
          />
          <span className="text-xs text-surface-400">to</span>
          <input
            type="datetime-local"
            value={customTo}
            onChange={(e) => handleTo(e.target.value)}
            className="rounded-lg border border-surface-300 bg-white px-2 py-1.5 text-xs text-surface-900 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
          />
        </div>
      )}
    </div>
  )
}
