import { Check, ChevronDown, Search, X } from 'lucide-react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { cn } from '../../lib/utils'

export interface MultiSelectOption {
  value: string
  label: string
  provider?: string
  tier?: string
}

interface MultiSelectProps {
  options: MultiSelectOption[]
  value: string // comma-separated selected values
  onChange: (csv: string) => void
  placeholder?: string
  disabled?: boolean
  groupByProvider?: boolean
}

const providerMeta: Record<string, { label: string; dot: string }> = {
  openai: { label: 'OpenAI', dot: 'bg-green-500' },
  anthropic: { label: 'Anthropic', dot: 'bg-purple-500' },
  deepseek: { label: 'DeepSeek', dot: 'bg-blue-500' },
  gemini: { label: 'Gemini', dot: 'bg-yellow-500' },
  openrouter: { label: 'OpenRouter', dot: 'bg-orange-500' },
  opencode: { label: 'OpenCode', dot: 'bg-teal-500' },
  ollama: { label: 'Ollama', dot: 'bg-gray-500' },
  qwen: { label: 'Qwen', dot: 'bg-cyan-500' },
}

const tierLabel: Record<string, string> = {
  free: 'Free',
  economy: 'Eco',
  standard: 'Std',
  premium: 'Prem',
}
const tierColors: Record<string, string> = {
  free: 'bg-gray-100 text-gray-700',
  economy: 'bg-green-100 text-green-700',
  standard: 'bg-blue-100 text-blue-700',
  premium: 'bg-purple-100 text-purple-700',
}

function groupByProviderKey(arr: MultiSelectOption[]): Map<string, MultiSelectOption[]> {
  const groups = new Map<string, MultiSelectOption[]>()
  for (const item of arr) {
    const key = item.provider || 'other'
    if (!groups.has(key)) groups.set(key, [])
    groups.get(key)?.push(item)
  }
  return groups
}

export function MultiSelect({
  options,
  value,
  onChange,
  placeholder = 'Select...',
  disabled,
  groupByProvider,
}: MultiSelectProps) {
  const [open, setOpen] = useState(false)
  const [search, setSearch] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (open) {
      setSearch('')
      requestAnimationFrame(() => inputRef.current?.focus())
    }
  }, [open])

  const selected = useMemo(() => {
    const vals = value ? value.split(',').filter(Boolean) : []
    return new Set(vals)
  }, [value])

  const toggle = (optValue: string) => {
    const next = selected.has(optValue) ? [...selected].filter((v) => v !== optValue) : [...selected, optValue]
    onChange(next.join(','))
  }
  const selectAll = () => onChange(options.map((o) => o.value).join(','))
  const clearAll = () => onChange('')

  const filtered = useMemo(() => {
    if (!search.trim()) return options
    const q = search.toLowerCase().trim()
    return options.filter(
      (o) =>
        o.label.toLowerCase().includes(q) ||
        o.value.toLowerCase().includes(q) ||
        (o.provider || '').toLowerCase().includes(q),
    )
  }, [options, search])

  const grouped = useMemo(() => {
    if (!groupByProvider) return null
    const groups = groupByProviderKey(filtered)
    return Array.from(groups.entries()).sort(([a], [b]) => a.localeCompare(b))
  }, [filtered, groupByProvider])

  const selectedCount = selected.size

  const wrapperRef = useRef<HTMLDivElement>(null)

  // click outside to close — no portal, no radix
  useEffect(() => {
    if (!open) return
    const handler = (e: MouseEvent) => {
      if (wrapperRef.current && !wrapperRef.current.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [open])

  return (
    <div ref={wrapperRef} className="relative min-w-[180px] w-full">
      <button
        type="button"
        disabled={disabled}
        onClick={() => setOpen((o) => !o)}
        className={cn(
          'inline-flex items-center gap-1.5 rounded-lg border border-surface-300 bg-white px-2.5 py-1.5 text-sm',
          'focus:outline-none focus:ring-2 focus:ring-brand-500 focus:border-brand-500',
          'hover:border-surface-400 hover:bg-surface-50',
          'disabled:opacity-50 disabled:cursor-not-allowed',
          'min-w-[180px] w-full justify-between',
          selectedCount === 0 ? 'text-surface-400' : 'text-surface-900',
        )}
      >
        <span className="truncate">{selectedCount === 0 ? placeholder : `${selectedCount} selected`}</span>
        <ChevronDown size={14} className={cn('text-surface-400 shrink-0 transition-transform', open && 'rotate-180')} />
      </button>

      {open && (
        <div className="absolute top-full left-0 z-50 mt-1 w-[320px] rounded-lg border border-surface-200 bg-white shadow-lg">
          <div className="relative mx-2 mt-1.5 mb-1">
            <Search
              size={14}
              className="absolute left-2.5 top-1/2 -translate-y-1/2 text-surface-400 pointer-events-none"
            />
            <input
              ref={inputRef}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Search..."
              className="w-full rounded-md border border-surface-200 bg-surface-50 pl-8 pr-7 py-1.5 text-xs text-surface-900 placeholder:text-surface-400 outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
              onKeyDown={(e) => {
                e.stopPropagation()
                if (e.key === 'Escape') setOpen(false)
              }}
            />
            {search && (
              <button
                type="button"
                onClick={() => setSearch('')}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-surface-400 hover:text-surface-600"
              >
                <X size={13} />
              </button>
            )}
          </div>

          <div className="flex items-center gap-2 px-3 py-1 border-b border-surface-100">
            <button
              type="button"
              onClick={selectAll}
              className="text-xs text-brand-600 hover:text-brand-700 font-medium"
            >
              Select All
            </button>
            <span className="text-surface-300">|</span>
            <button
              type="button"
              onClick={clearAll}
              className="text-xs text-surface-500 hover:text-surface-700 font-medium"
            >
              Clear
            </button>
            {selectedCount > 0 && <span className="ml-auto text-xs text-surface-400">{selectedCount} selected</span>}
          </div>

          <div className="max-h-56 overflow-y-auto py-1">
            {filtered.length === 0 ? (
              <p className="px-3 py-4 text-xs text-surface-400 text-center">
                {search ? `No matches for "${search}"` : 'No options available'}
              </p>
            ) : grouped ? (
              grouped.map(([provider, items]) => (
                <div key={provider}>
                  <div className="flex items-center gap-1.5 px-3 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-surface-500">
                    {providerMeta[provider]?.dot && (
                      <span className={`w-1.5 h-1.5 rounded-full ${providerMeta[provider].dot}`} />
                    )}
                    {providerMeta[provider]?.label || provider}
                  </div>
                  {items.map((opt) => (
                    <label
                      key={opt.value}
                      className="flex items-center gap-2 px-3 py-1.5 text-sm text-surface-700 hover:bg-surface-50 cursor-pointer"
                    >
                      <input
                        type="checkbox"
                        checked={selected.has(opt.value)}
                        onChange={() => toggle(opt.value)}
                        className="rounded border-surface-300 text-brand-600 focus:ring-brand-500 shrink-0"
                      />
                      <span className="truncate flex-1">{opt.label}</span>
                      {opt.tier && (
                        <span
                          className={`inline-flex items-center rounded px-1 py-0.5 text-[10px] font-semibold uppercase leading-none shrink-0 ${tierColors[opt.tier] || 'bg-surface-100 text-surface-500'}`}
                        >
                          {tierLabel[opt.tier] || opt.tier}
                        </span>
                      )}
                      {selected.has(opt.value) && <Check size={14} className="text-brand-600 shrink-0" />}
                    </label>
                  ))}
                </div>
              ))
            ) : (
              filtered.map((opt) => (
                <label
                  key={opt.value}
                  className="flex items-center gap-2 px-3 py-1.5 text-sm text-surface-700 hover:bg-surface-50 cursor-pointer"
                >
                  <input
                    type="checkbox"
                    checked={selected.has(opt.value)}
                    onChange={() => toggle(opt.value)}
                    className="rounded border-surface-300 text-brand-600 focus:ring-brand-500 shrink-0"
                  />
                  <span className="truncate flex-1">{opt.label}</span>
                  {selected.has(opt.value) && <Check size={14} className="text-brand-600 shrink-0" />}
                </label>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  )
}
