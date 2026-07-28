import { cn } from '../../lib/utils'

interface CategoryFilterProps {
  categories: Array<{ name: string; count: number }>
  selected: string | null
  onSelect: (cat: string | null) => void
}

export function CategoryFilter({ categories, selected, onSelect }: CategoryFilterProps) {
  const total = categories.reduce((sum, c) => sum + c.count, 0)

  return (
    <div className="flex items-center gap-2 overflow-x-auto pb-1 scrollbar-thin">
      <button
        onClick={() => onSelect(null)}
        className={cn(
          'shrink-0 rounded-full border px-3.5 py-1.5 text-xs font-medium transition-colors',
          selected === null
            ? 'bg-brand-600 text-white border-brand-600'
            : 'bg-white text-surface-600 border-surface-300 hover:border-surface-400 hover:text-surface-800',
        )}
      >
        All
        <span className="ml-1.5 text-surface-400 font-normal">{total}</span>
      </button>

      {categories.map((cat) => (
        <button
          key={cat.name}
          onClick={() => onSelect(cat.name === selected ? null : cat.name)}
          className={cn(
            'shrink-0 rounded-full border px-3.5 py-1.5 text-xs font-medium transition-colors whitespace-nowrap',
            selected === cat.name
              ? 'bg-brand-600 text-white border-brand-600'
              : 'bg-white text-surface-600 border-surface-300 hover:border-surface-400 hover:text-surface-800',
          )}
        >
          {cat.name}
          <span className="ml-1.5 text-surface-400 font-normal">{cat.count}</span>
        </button>
      ))}
    </div>
  )
}
