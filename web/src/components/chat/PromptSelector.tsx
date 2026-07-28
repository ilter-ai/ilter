import { FileText, Loader2, Search, X } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { api, type PromptTemplate } from '../../lib/api'
import { cn } from '../../lib/utils'
import { Select, SelectContent, SelectItem, SelectTrigger } from '../ui/select'

export function PromptSelector({
  onSelect,
  disabled,
}: {
  onSelect: (template: PromptTemplate) => void
  disabled?: boolean
}) {
  const [templates, setTemplates] = useState<PromptTemplate[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [value, setValue] = useState('__prompts__')

  useEffect(() => {
    api.prompts
      .getPromptTemplates()
      .then(setTemplates)
      .catch(() => setError('Failed to load templates'))
      .finally(() => setLoading(false))
  }, [])

  const filtered = useMemo(() => {
    if (!search.trim()) return templates
    const q = search.toLowerCase()
    return templates.filter((t) => t.name.toLowerCase().includes(q) || t.description?.toLowerCase().includes(q))
  }, [templates, search])

  const handleValueChange = (val: string | null) => {
    if (!val || val === '__prompts__') return
    const template = templates.find((t) => t.id === val)
    if (template) {
      onSelect(template)
    }
    setValue('__prompts__')
  }

  return (
    <Select value={value} onValueChange={handleValueChange} disabled={disabled}>
      <SelectTrigger
        className={cn(
          'inline-flex items-center gap-1.5 rounded-lg border border-surface-300 bg-white px-2.5 py-1.5 text-sm text-surface-900',
          'focus:outline-none focus:ring-2 focus:ring-brand-500 focus:border-brand-500',
          'hover:border-surface-400 hover:bg-surface-50',
          'disabled:opacity-50 disabled:cursor-not-allowed',
        )}
      >
        <FileText size={14} />
        Prompts
      </SelectTrigger>

      <SelectContent
        side="top"
        alignItemWithTrigger={false}
        className="z-50 min-w-[260px] overflow-hidden rounded-lg border border-surface-200 bg-white shadow-lg"
        sideOffset={4}
        align="start"
      >
        <div className="relative mx-2 mt-1.5 mb-1">
          <Search
            size={14}
            className="absolute left-2.5 top-1/2 -translate-y-1/2 text-surface-400 pointer-events-none"
          />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search prompts..."
            className="w-full rounded-md border border-surface-200 bg-surface-50 pl-8 pr-7 py-1.5 text-xs text-surface-900 placeholder:text-surface-400 outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
            onKeyDown={(e) => e.stopPropagation()}
          />
          {search && (
            <button
              onClick={() => setSearch('')}
              className="absolute right-2 top-1/2 -translate-y-1/2 text-surface-400 hover:text-surface-600"
            >
              <X size={13} />
            </button>
          )}
        </div>

        {loading && (
          <div className="flex items-center justify-center gap-2 px-4 py-4 text-xs text-surface-400">
            <Loader2 size={14} className="animate-spin" />
            Loading prompts...
          </div>
        )}
        {error && !loading && <div className="px-4 py-3 text-xs text-error">{error}</div>}
        {!loading && !error && filtered.length === 0 && (
          <div className="px-3 py-4 text-xs text-surface-400 text-center">
            {search ? `No prompts match "${search}"` : 'No prompts available'}
          </div>
        )}
        {!loading &&
          !error &&
          filtered.map((t) => (
            <SelectItem
              key={t.id}
              value={t.id}
              className={cn(
                'relative flex cursor-pointer select-none items-center gap-2 rounded-md px-3 py-2 text-sm text-surface-900',
                'data-[highlighted]:bg-brand-50 data-[highlighted]:text-brand-700 data-[highlighted]:outline-none',
                'data-[state=checked]:bg-brand-50 data-[state=checked]:text-brand-700',
              )}
            >
              <span className="flex items-center gap-2">
                <span className="font-medium">{t.name}</span>
              </span>
            </SelectItem>
          ))}
      </SelectContent>
    </Select>
  )
}
