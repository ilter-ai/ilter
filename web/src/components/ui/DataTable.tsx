import { cn } from '../../lib/utils'
import { AlertTriangle, RefreshCw } from '../ui/icons'
import { Skeleton } from '../ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from './table'

export interface Column<T> {
  key: string
  header: string
  render?: (item: T) => React.ReactNode
  sortable?: boolean
  className?: string
  headerClassName?: string
}

interface DataTableProps<T> {
  columns: Column<T>[]
  data: T[]
  keyExtractor: (item: T) => string
  onRowClick?: (item: T) => void
  className?: string
  emptyMessage?: string
  loading?: boolean
  error?: Error | null
  onRetry?: () => void
  skeletonRows?: number
}

function ErrorState({ error, onRetry }: { error: Error; onRetry?: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-16">
      <AlertTriangle size={40} strokeWidth={1.5} className="text-error" />
      <p className="mt-3 text-sm font-medium text-surface-700">Failed to load data</p>
      <p className="mt-1 text-xs text-surface-400 max-w-sm text-center">{error.message}</p>
      {onRetry && (
        <button
          onClick={onRetry}
          className="mt-4 inline-flex items-center gap-1.5 text-sm text-brand-600 hover:text-brand-700"
        >
          <RefreshCw size={14} />
          Retry
        </button>
      )}
    </div>
  )
}

export function DataTable<T>({
  columns,
  data,
  keyExtractor,
  onRowClick,
  className,
  emptyMessage = 'No data available.',
  loading,
  error,
  onRetry,
  skeletonRows = 5,
}: DataTableProps<T>) {
  if (error && !loading) {
    return <ErrorState error={error} onRetry={onRetry} />
  }

  if (loading) {
    return (
      <div className="rounded-xl border border-surface-200 bg-white shadow-card">
        <div className="flex border-b border-surface-200 px-4 py-3 gap-4">
          {Array.from({ length: columns.length }).map((_, ci) => (
            <Skeleton key={`h-${ci}`} className={cn('h-3', ci === 0 ? 'w-1/4' : 'w-1/6')} />
          ))}
        </div>
        {Array.from({ length: skeletonRows }).map((_, ri) => (
          <div key={`r-${ri}`} className="flex border-b border-surface-100 px-4 py-3.5 gap-4 last:border-b-0">
            {Array.from({ length: columns.length }).map((_, ci) => (
              <Skeleton key={`c-${ri}-${ci}`} className={cn('h-3', ci === 0 ? 'w-2/5' : 'w-1/5')} />
            ))}
          </div>
        ))}
      </div>
    )
  }

  if (data.length === 0) {
    return <div className="flex items-center justify-center py-12 text-sm text-surface-400">{emptyMessage}</div>
  }

  return (
    <Table className={className}>
      <TableHeader>
        <TableRow>
          {columns.map((col) => (
            <TableHead
              key={col.key}
              className={cn(
                'text-xs font-medium uppercase tracking-wider text-surface-500',
                col.sortable && 'cursor-pointer hover:text-surface-700',
                col.headerClassName,
              )}
            >
              {col.header}
            </TableHead>
          ))}
        </TableRow>
      </TableHeader>
      <TableBody>
        {data.map((item) => (
          <TableRow
            key={keyExtractor(item)}
            className={cn('transition-colors hover:bg-surface-50', onRowClick && 'cursor-pointer')}
            onClick={() => onRowClick?.(item)}
          >
            {columns.map((col) => (
              <TableCell key={col.key} className={cn('text-sm text-surface-700', col.className)}>
                {col.render ? col.render(item) : String((item as Record<string, unknown>)[col.key] ?? '')}
              </TableCell>
            ))}
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}
