import { cn } from '../../lib/utils'

interface ChartSkeletonProps {
  height?: number
  className?: string
}

export function ChartSkeleton({ height = 320, className }: ChartSkeletonProps) {
  return (
    <div
      className={cn('w-full rounded-xl border border-surface-100 bg-white p-6', className)}
      style={{ height, minHeight: height }}
      aria-hidden="true"
    >
      {/* Title bar */}
      <div className="flex items-center gap-4 mb-6">
        <div className="h-4 w-28 animate-pulse rounded bg-surface-200" />
        <div className="h-3 w-16 animate-pulse rounded bg-surface-100" />
      </div>

      {/* Y-axis label */}
      <div className="flex gap-4 h-[calc(100%-60px)]">
        <div className="flex flex-col justify-between py-1">
          {Array.from({ length: 5 }).map((_, i) => (
            <div key={i} className="h-2 w-8 animate-pulse rounded bg-surface-100" />
          ))}
        </div>

        {/* Chart area */}
        <div className="flex-1 flex items-end gap-2 pb-1">
          {Array.from({ length: 12 }).map((_, i) => (
            <div
              key={i}
              className="flex-1 animate-pulse rounded-t"
              style={{
                height: `${30 + Math.random() * 60}%`,
                backgroundColor: i % 3 === 0 ? '#E2E8F0' : '#F1F5F9',
                animationDelay: `${i * 80}ms`,
              }}
            />
          ))}
        </div>
      </div>
    </div>
  )
}
