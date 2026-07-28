import type { ReactNode } from 'react'

interface SearchableListProps<T> {
  items: T[]
  isLoading?: boolean
  loadingLabel?: string
  search: string
  onSearchChange: (value: string) => void
  searchPlaceholder: string
  isSelected: (item: T) => boolean
  onSelect: (item: T) => void
  getKey: (item: T) => string | number
  renderItem: (item: T) => ReactNode
  emptyMessage?: string
  error?: string | null
}

/**
 * Compact search box + scrollable single-select list, used by every
 * user/group/key picker (Budget, Rate Limiting scope views, etc). Filtering
 * is left to the caller (via already-filtered `items`) since match rules
 * differ slightly per entity type.
 */
export function SearchableList<T>({
  items,
  isLoading,
  loadingLabel = 'Loading...',
  search,
  onSearchChange,
  searchPlaceholder,
  isSelected,
  onSelect,
  getKey,
  renderItem,
  emptyMessage = 'No results found',
  error,
}: SearchableListProps<T>) {
  return (
    <div className="space-y-4">
      <input
        type="text"
        placeholder={searchPlaceholder}
        value={search}
        onChange={(e) => onSearchChange(e.target.value)}
        className="w-full rounded-lg border border-surface-200 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:outline-none focus:ring-2 focus:ring-brand-500"
      />

      {isLoading && (
        <div className="flex items-center justify-center py-8">
          <div className="h-6 w-6 animate-spin rounded-full border-2 border-surface-200 border-t-brand-600" />
          <span className="ml-2 text-sm text-surface-500">{loadingLabel}</span>
        </div>
      )}

      {error && (
        <div className="rounded-lg bg-error/10 border border-error/20 px-4 py-3 text-sm text-error">{error}</div>
      )}

      <div className="max-h-60 overflow-y-auto rounded-lg border border-surface-200 divide-y divide-surface-100">
        {items.length === 0 && !isLoading && (
          <div className="px-4 py-6 text-center text-sm text-surface-500">{emptyMessage}</div>
        )}
        {items.map((item) => (
          <button
            key={getKey(item)}
            type="button"
            onClick={() => onSelect(item)}
            className={`w-full text-left px-4 py-3 text-sm hover:bg-surface-50 transition-colors ${
              isSelected(item) ? 'bg-brand-50 border-l-2 border-l-brand-500' : ''
            }`}
          >
            {renderItem(item)}
          </button>
        ))}
      </div>
    </div>
  )
}
