import { cn } from '../../lib/utils'
import { EmptyState } from './empty-state'
import { AlertTriangle, Loader2 } from './icons'
import { Skeleton } from './skeleton'

/**
 * Standardised loading / error / empty state component for useQuery results.
 *
 * Usage:
 *   if (isLoading) return <QueryState.Loading kind="card" count={4} />;
 *   if (error)     return <QueryState.Error message={error.message} onRetry={refetch} />;
 *   if (!data)     return <QueryState.Empty title="No data" description="..." />;
 *
 *   return <ActualView data={data} />;
 */

// ── Loading ──

interface LoadingProps {
  kind?: 'card' | 'table'
  count?: number
  rows?: number
  cols?: number
}

function Loading({ kind = 'card', count = 1, rows = 4, cols = 5 }: LoadingProps) {
  return (
    <div className="space-y-4 animate-pulse">
      {kind === 'card' ? (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          {Array.from({ length: count }).map((_, i) => (
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
        <div className="rounded-xl border border-surface-200 bg-white shadow-card">
          <div className="flex border-b border-surface-200 px-4 py-3 gap-4">
            {Array.from({ length: cols }).map((_, ci) => (
              <Skeleton key={`h-${ci}`} className={cn('h-3', ci === 0 ? 'w-1/4' : 'w-1/6')} />
            ))}
          </div>
          {Array.from({ length: rows }).map((_, ri) => (
            <div key={`r-${ri}`} className="flex border-b border-surface-100 px-4 py-3.5 gap-4 last:border-b-0">
              {Array.from({ length: cols }).map((_, ci) => (
                <Skeleton key={`c-${ri}-${ci}`} className={cn('h-3', ci === 0 ? 'w-2/5' : 'w-1/5')} />
              ))}
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// ── Error ──

interface ErrorProps {
  message: string
  onRetry?: () => void
  title?: string
}

function ErrorState({ message, onRetry, title = 'Something went wrong' }: ErrorProps) {
  return (
    <div className="flex flex-col items-center justify-center py-16">
      <div className="rounded-full bg-error/10 p-4 mb-4">
        <AlertTriangle size={32} className="text-error" />
      </div>
      <h3 className="text-lg font-semibold text-surface-900 mb-1">{title}</h3>
      <p className="text-sm text-surface-500 mb-6 max-w-md text-center">{message}</p>
      {onRetry && (
        <button
          onClick={onRetry}
          className="inline-flex items-center gap-2 rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white hover:bg-brand-700 transition-colors"
        >
          <Loader2 size={14} />
          Try again
        </button>
      )}
    </div>
  )
}

// ── Empty ──

interface EmptyProps {
  title: string
  description: string
  action?: { label: string; onClick: () => void }
}

function Empty({ title, description, action }: EmptyProps) {
  return <EmptyState title={title} description={description} action={action} />
}

// ── Inline loading spinner (for mutation loading / small areas) ──

function Spinner({ size = 16 }: { size?: number }) {
  return <Loader2 size={size} className="animate-spin text-brand-600" />
}

export const QueryState = {
  Loading,
  Error: ErrorState,
  Empty,
  Spinner,
}
