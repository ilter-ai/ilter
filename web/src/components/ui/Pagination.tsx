import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { ChevronLeft, ChevronRight } from '../ui/icons'

interface PaginationProps {
  page: number
  totalPages: number
  total: number
  perPage: number
  onPageChange: (page: number) => void
  className?: string
}

export function Pagination({ page, totalPages, total, perPage, onPageChange, className }: PaginationProps) {
  if (totalPages <= 1) return null

  const startItem = (page - 1) * perPage + 1
  const endItem = Math.min(page * perPage, total)

  const getVisiblePages = (): (number | '...')[] => {
    const pages: (number | '...')[] = []
    const delta = 2

    pages.push(1)

    if (page - delta > 2) {
      pages.push('...')
    }

    for (let i = Math.max(2, page - delta); i <= Math.min(totalPages - 1, page + delta); i++) {
      pages.push(i)
    }

    if (page + delta < totalPages - 1) {
      pages.push('...')
    }

    pages.push(totalPages)

    return pages
  }

  const visiblePages = getVisiblePages()

  return (
    <div className={cn('flex items-center justify-between', className)}>
      <p className="text-sm text-surface-500">
        Showing <span className="font-medium">{startItem}</span> to <span className="font-medium">{endItem}</span> of{' '}
        <span className="font-medium">{total}</span> results
      </p>

      <div className="flex items-center gap-1">
        <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => onPageChange(page - 1)}>
          <ChevronLeft size={16} />
          Previous
        </Button>

        {visiblePages.map((p, idx) =>
          p === '...' ? (
            <span key={`ellipsis-${idx}`} className="px-2 text-sm text-surface-400">
              ...
            </span>
          ) : (
            <Button
              key={p}
              variant={p === page ? 'default' : 'outline'}
              size="sm"
              className="min-w-[2rem]"
              onClick={() => onPageChange(p)}
            >
              {p}
            </Button>
          ),
        )}

        <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => onPageChange(page + 1)}>
          Next
          <ChevronRight size={16} />
        </Button>
      </div>
    </div>
  )
}
