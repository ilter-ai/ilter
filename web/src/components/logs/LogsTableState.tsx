import { Activity, Loader2 } from '../ui/icons'

export function LogsTableSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <div className="space-y-3">
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="h-14 rounded-xl bg-surface-100 animate-pulse" />
      ))}
    </div>
  )
}

export function LogsEmptyState({ title, description }: { title: string; description: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-16">
      <Activity size={48} strokeWidth={1.5} className="text-surface-300 mb-4" />
      <p className="text-sm font-medium text-surface-500 mb-2">{title}</p>
      <p className="text-xs text-surface-400 text-center max-w-md">{description}</p>
    </div>
  )
}

export function LogsDetailLoading() {
  return (
    <div className="flex items-center justify-center py-16">
      <Loader2 size={24} className="animate-spin text-brand-600" />
    </div>
  )
}
