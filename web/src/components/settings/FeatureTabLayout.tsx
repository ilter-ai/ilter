import type { ReactNode } from 'react'
import { Skeleton } from '../ui/skeleton'

interface FeatureTabLayoutProps {
  title: string
  description?: string
  status: ReactNode
  stats?: ReactNode
  config?: ReactNode
  table?: ReactNode
  enabled?: boolean
  loading?: boolean
  error?: ReactNode
}

export function FeatureTabLayout({
  title,
  description,
  status,
  stats,
  config,
  table,
  enabled = true,
  loading,
  error,
}: FeatureTabLayoutProps) {
  if (loading) {
    return (
      <div className="space-y-6">
        {stats ? (
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <div key={i} className="rounded-xl border border-surface-200 bg-white p-6 shadow-card">
                <Skeleton className="h-4 w-1/3 mb-4" />
                <Skeleton className="h-8 w-1/2 mb-3" />
                <Skeleton className="h-3 w-2/3 mb-2" />
                <Skeleton className="h-3 w-1/2 mb-2" />
                <Skeleton className="h-3 w-3/4" />
              </div>
            ))}
          </div>
        ) : (
          <div className="rounded-xl border border-surface-200 bg-white p-6 shadow-card">
            <Skeleton className="h-4 w-1/3 mb-4" />
            <Skeleton className="h-8 w-1/2 mb-3" />
            <Skeleton className="h-3 w-2/3 mb-2" />
            <Skeleton className="h-3 w-1/2 mb-2" />
            <Skeleton className="h-3 w-3/4" />
          </div>
        )}
      </div>
    )
  }

  if (error) {
    return <>{error}</>
  }

  return (
    <div className="space-y-6">
      <header className="flex items-start justify-between gap-4">
        <div>
          <h2 className="text-lg font-semibold text-surface-900">{title}</h2>
          {description && <p className="text-sm text-surface-500 mt-0.5">{description}</p>}
        </div>
        <div className="shrink-0">{status}</div>
      </header>

      {stats && <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">{stats}</div>}

      {config && (
        <fieldset disabled={!enabled} aria-disabled={!enabled} className={!enabled ? 'opacity-50' : undefined}>
          {config}
        </fieldset>
      )}

      {table}
    </div>
  )
}
