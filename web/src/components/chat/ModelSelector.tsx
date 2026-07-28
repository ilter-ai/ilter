import { useQuery } from '@tanstack/react-query'
import { Search, X } from 'lucide-react'
import { useMemo, useState } from 'react'
import { api, type ModelProvider } from '../../lib/api'
import { qk } from '../../lib/query'
import { cn } from '../../lib/utils'
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from '../ui/select'

export const providerMeta: Record<string, { label: string; dot: string }> = {
  openai: { label: 'OpenAI', dot: 'bg-green-500' },
  anthropic: { label: 'Anthropic', dot: 'bg-purple-500' },
  deepseek: { label: 'DeepSeek', dot: 'bg-blue-500' },
  gemini: { label: 'Gemini', dot: 'bg-yellow-500' },
  openrouter: { label: 'OpenRouter', dot: 'bg-orange-500' },
  opencode: { label: 'OpenCode', dot: 'bg-teal-500' },
  opencode_go: { label: 'OpenCode Go', dot: 'bg-teal-500' },
  opencode_zen: { label: 'OpenCode Zen', dot: 'bg-teal-500' },
  ollama: { label: 'Ollama', dot: 'bg-gray-500' },
  qwen: { label: 'Qwen', dot: 'bg-cyan-500' },
}

export const tierLabel: Record<string, string> = {
  free: 'Free',
  economy: 'Eco',
  standard: 'Std',
  premium: 'Prem',
}

export const tierColors: Record<string, string> = {
  free: 'bg-gray-100 text-gray-700 border border-gray-200',
  economy: 'bg-green-50 text-green-700 border border-green-200',
  standard: 'bg-blue-50 text-blue-700 border border-blue-200',
  premium: 'bg-purple-50 text-purple-700 border border-purple-200',
}

export function TierBadge({ tier }: { tier?: string }) {
  if (!tier) return null
  return (
    <span
      className={`inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase leading-none ${tierColors[tier] || 'bg-surface-100 text-surface-500'}`}
    >
      {tierLabel[tier] || tier}
    </span>
  )
}

export function ModelBadge({
  modelId,
  provider,
  tier,
  name,
  onRemove,
  className,
}: {
  modelId: string
  provider?: string
  tier?: string
  name?: string
  onRemove?: () => void
  className?: string
}) {
  const provKey = provider || (modelId.includes('/') ? modelId.split('/')[0] : '')
  const displayName = name || (modelId.includes('/') ? modelId.split('/')[1] : modelId)
  const meta = provKey ? providerMeta[provKey.toLowerCase()] : undefined

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md text-xs font-mono bg-white border border-surface-200 shadow-sm text-surface-900',
        className,
      )}
    >
      {meta?.dot && <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${meta.dot}`} />}
      {provKey && <span className="font-semibold text-surface-500 font-sans">{meta?.label || provKey} /</span>}
      <span className="font-medium font-sans">{displayName}</span>
      <TierBadge tier={tier} />
      {onRemove && (
        <button
          type="button"
          onClick={onRemove}
          className="ml-1 text-surface-400 hover:text-error transition-colors"
          title={`Remove ${modelId}`}
        >
          <X size={13} />
        </button>
      )}
    </span>
  )
}

function groupByProvider(models: ModelProvider[]): Map<string, ModelProvider[]> {
  const groups = new Map<string, ModelProvider[]>()
  for (const m of models) {
    const key = m.provider || 'other'
    if (!groups.has(key)) groups.set(key, [])
    groups.get(key)?.push(m)
  }
  return groups
}

export function ModelSelector({
  models: propModels,
  value,
  onChange,
  disabled,
  placeholder = 'Select model...',
  side = 'bottom',
}: {
  models?: ModelProvider[]
  value: string
  onChange: (id: string) => void
  disabled?: boolean
  placeholder?: string
  side?: 'top' | 'bottom'
}) {
  const { data: fetchedModels } = useQuery({
    queryKey: qk.models,
    queryFn: () => api.models.getModelProviders().catch(() => []),
    enabled: !propModels,
  })

  const models = propModels ?? fetchedModels ?? []
  const [search, setSearch] = useState('')

  const sortedGroups = useMemo(() => {
    const q = search.toLowerCase().trim()
    const groups = groupByProvider(models)
    const out = new Map<string, ModelProvider[]>()
    for (const [provider, list] of groups) {
      let filtered = list
      if (q) {
        filtered = list.filter(
          (m) =>
            m.name.toLowerCase().includes(q) || m.provider.toLowerCase().includes(q) || m.id.toLowerCase().includes(q),
        )
      }
      if (filtered.length > 0) {
        filtered.sort((a, b) => a.name.localeCompare(b.name))
        out.set(provider, filtered)
      }
    }
    return out
  }, [models, search])

  const selected = models.find((m) => m.id === value || m.model === value || m.name === value)
  const meta = selected ? providerMeta[selected.provider.toLowerCase()] : undefined

  return (
    <Select value={value} onValueChange={(val) => val !== null && onChange(val)} disabled={disabled}>
      <SelectTrigger
        className={cn(
          'inline-flex items-center gap-1.5 rounded-lg border border-surface-300 bg-white px-2.5 py-1.5 text-sm text-surface-900',
          'focus:outline-none focus:ring-2 focus:ring-brand-500 focus:border-brand-500',
          'hover:border-surface-400 hover:bg-surface-50',
          'disabled:opacity-50 disabled:cursor-not-allowed',
          'min-w-[140px]',
        )}
      >
        <SelectValue placeholder={placeholder}>
          {selected && (
            <span className="flex items-center gap-1.5 min-w-0">
              {meta && <span className={`w-1.5 h-1.5 rounded-full shrink-0 ${meta.dot}`} />}
              <span className="font-medium truncate">{selected.name}</span>
              <TierBadge tier={selected.tier} />
            </span>
          )}
        </SelectValue>
      </SelectTrigger>

      <SelectContent
        className="z-50 min-w-[260px] max-h-[320px] overflow-y-auto rounded-lg border border-surface-200 bg-white shadow-lg"
        side={side}
        sideOffset={4}
        align="start"
        alignItemWithTrigger={false}
      >
        <div className="relative mx-2 mt-1.5 mb-1">
          <Search
            size={14}
            className="absolute left-2.5 top-1/2 -translate-y-1/2 text-surface-400 pointer-events-none"
          />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search models..."
            className="w-full rounded-md border border-surface-200 bg-surface-50 pl-8 pr-7 py-1.5 text-xs text-surface-900 placeholder:text-surface-400 outline-none focus:border-brand-500 focus:ring-1 focus:ring-brand-500"
            onKeyDown={(e) => e.stopPropagation()}
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

        {models.length === 0 ? (
          <SelectItem value="__noop" disabled className="px-3 py-2 text-xs text-surface-400 cursor-default">
            No models available
          </SelectItem>
        ) : sortedGroups.size === 0 ? (
          <div className="px-3 py-4 text-xs text-surface-400 text-center">No models match &quot;{search}&quot;</div>
        ) : (
          Array.from(sortedGroups.entries()).map(([provider, providerModels]) => (
            <SelectGroup key={provider}>
              <SelectLabel className="flex items-center gap-1.5 px-3 py-1.5 text-[11px] font-semibold uppercase tracking-wider text-surface-500">
                {providerMeta[provider.toLowerCase()]?.dot && (
                  <span className={`w-1.5 h-1.5 rounded-full ${providerMeta[provider.toLowerCase()].dot}`} />
                )}
                {providerMeta[provider.toLowerCase()]?.label || provider}
              </SelectLabel>
              {providerModels.map((m) => (
                <SelectItem
                  key={m.id}
                  value={m.id}
                  className={cn(
                    'relative flex cursor-pointer select-none items-center justify-between gap-2 rounded-md px-3 py-2 text-sm text-surface-900',
                    'data-[highlighted]:bg-brand-50 data-[highlighted]:text-brand-700 data-[highlighted]:outline-none',
                    'data-[state=checked]:bg-brand-50 data-[state=checked]:text-brand-700',
                  )}
                >
                  <span className="flex items-center gap-2">
                    <span className="font-medium">{search ? highlightMatch(m.name, search) : m.name}</span>
                    <TierBadge tier={m.tier} />
                  </span>
                </SelectItem>
              ))}
            </SelectGroup>
          ))
        )}
      </SelectContent>
    </Select>
  )
}

function highlightMatch(text: string, query: string): React.ReactNode {
  const q = query.toLowerCase().trim()
  if (!q) return text
  const idx = text.toLowerCase().indexOf(q)
  if (idx === -1) return text
  return (
    <>
      {text.slice(0, idx)}
      <mark className="bg-yellow-200 rounded-sm px-0.5">{text.slice(idx, idx + q.length)}</mark>
      {text.slice(idx + q.length)}
    </>
  )
}
