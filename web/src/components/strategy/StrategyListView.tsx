import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import { api, type RoutingStrategy } from '../../lib/api'
import { qk } from '../../lib/query'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { EmptyState } from '../ui/empty-state'
import { AlertTriangle, CheckCircle2, Circle, Edit3, Plus, Trash2 } from '../ui/icons'
import { QueryProvider } from '../ui/query-provider'
import { Skeleton } from '../ui/skeleton'

function capName(s: string) {
  return s
    .split('-')
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ')
}

function defaultStrategy(): RoutingStrategy {
  return {
    name: '',
    description: '',
    enabled: false,
    provider_preference: 'cheapest',
    load_balancer_strategy: 'weighted-random',
    scorer: { type: 'heuristic' },
    complexity_thresholds: { economy: 15, standard: 50 },
    rules: [],
  }
}

export interface StrategyListViewProps {
  onEdit: (strategy: RoutingStrategy) => void
}

function StrategyListViewContent({ onEdit }: StrategyListViewProps) {
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null)

  const {
    data: listData,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: qk.strategies,
    queryFn: () => api.strategies.fetchStrategies(),
  })

  const { data: activeData } = useQuery({
    queryKey: qk.activeStrategy,
    queryFn: () => api.strategies.fetchActiveStrategy(),
  })

  const strategies = listData?.data ?? []
  const activeStrategyName = activeData?.active ?? listData?.active ?? ''

  const deleteMutation = useMutation({
    mutationFn: (name: string) => api.strategies.deleteStrategy(name),
    onSuccess: (_data, name) => {
      queryClient.invalidateQueries({ queryKey: qk.strategies })
      queryClient.invalidateQueries({ queryKey: qk.activeStrategy })
      toast.success('Strategy deleted', { description: `"${name}" has been removed.` })
    },
    onError: (err) => {
      toast.error('Failed to delete strategy', {
        description: err instanceof Error ? err.message : 'Unknown error',
      })
    },
  })

  const setActiveMutation = useMutation({
    mutationFn: (name: string) => api.strategies.setActiveStrategy(name),
    onSuccess: (_data, name) => {
      queryClient.invalidateQueries({ queryKey: qk.strategies })
      queryClient.invalidateQueries({ queryKey: qk.activeStrategy })
      toast.success('Active strategy updated', { description: `"${name}" is now the active strategy.` })
    },
    onError: (err) => {
      toast.error('Failed to set active strategy', {
        description: err instanceof Error ? err.message : 'Unknown error',
      })
    },
  })

  const sortedStrategies = [...strategies].sort((a, b) => {
    if (a.name === 'economy') return -1
    if (b.name === 'economy') return 1
    return a.name.localeCompare(b.name)
  })

  const filteredStrategies = search
    ? sortedStrategies.filter((s) => s.name.toLowerCase().includes(search.toLowerCase()))
    : sortedStrategies

  if (error) {
    return (
      <EmptyState
        title="Failed to load strategies"
        icon={<AlertTriangle size={20} />}
        action={{ label: 'Retry', onClick: () => refetch() }}
      />
    )
  }

  if (isLoading) {
    return (
      <div className="space-y-6">
        <div className="flex items-center justify-between">
          <Skeleton className="h-6 w-40" />
          <Skeleton className="h-8 w-32" />
        </div>
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Card key={i}>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <Skeleton className="h-5 w-32" />
                  <Skeleton className="h-5 w-16 rounded-full" />
                </div>
              </CardHeader>
              <CardContent>
                <div className="space-y-3">
                  <Skeleton className="h-3 w-full" />
                  <Skeleton className="h-3 w-3/4" />
                  <div className="flex items-center gap-4 pt-2">
                    <Skeleton className="h-3 w-20" />
                    <Skeleton className="h-3 w-16" />
                  </div>
                  <div className="flex items-center gap-2 pt-2">
                    <Skeleton className="h-7 w-20" />
                    <Skeleton className="h-7 w-14" />
                    <Skeleton className="h-7 w-14" />
                  </div>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-surface-900">Strategy Manager</h2>
        <Button onClick={() => onEdit(defaultStrategy())}>
          <Plus size={16} />
          Create Strategy
        </Button>
      </div>

      {/* Search */}
      <div className="flex items-center gap-4">
        <div className="relative flex-1 max-w-sm">
          <svg
            className="absolute left-3 top-1/2 -translate-y-1/2 text-surface-400"
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <circle cx="11" cy="11" r="8" />
            <path d="m21 21-4.3-4.3" />
          </svg>
          <input
            type="text"
            placeholder="Search strategies..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full rounded-lg border border-surface-300 bg-white py-2 pl-10 pr-3 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
          />
        </div>
        <span className="text-sm text-surface-400">
          {filteredStrategies.length} / {strategies.length} strategies
        </span>
      </div>

      {/* Empty state */}
      {filteredStrategies.length === 0 && !isLoading ? (
        <EmptyState
          title={search ? 'No strategies match your search' : 'No strategies configured'}
          description={search ? 'Try a different search term.' : 'Create your first routing strategy to get started.'}
          action={
            search
              ? { label: 'Clear search', onClick: () => setSearch('') }
              : { label: 'Create Strategy', onClick: () => onEdit(defaultStrategy()) }
          }
        />
      ) : (
        /* Strategy cards grid */
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {filteredStrategies.map((strategy) => {
            const isActive = strategy.name === activeStrategyName
            return (
              <Card key={strategy.name}>
                <CardHeader>
                  <div className="flex items-start justify-between gap-2">
                    <div className="flex items-center gap-2 min-w-0">
                      <CardTitle className="truncate">{capName(strategy.name)}</CardTitle>
                      {isActive && (
                        <span className="inline-flex items-center gap-1 rounded-full bg-success/10 px-2 py-0.5 text-xs font-medium text-success border border-success/20 shrink-0">
                          <CheckCircle2 size={10} />
                          Active
                        </span>
                      )}
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      {strategy.enabled ? (
                        <span className="inline-flex items-center rounded-full bg-success/10 px-2 py-0.5 text-xs font-medium text-success border border-success/20">
                          Enabled
                        </span>
                      ) : (
                        <span className="inline-flex items-center rounded-full bg-surface-100 px-2 py-0.5 text-xs font-medium text-surface-500 border border-surface-200">
                          Disabled
                        </span>
                      )}
                    </div>
                  </div>
                </CardHeader>
                <CardContent>
                  <p className="text-sm text-surface-600 line-clamp-2 mb-3">
                    {strategy.description || 'No description'}
                  </p>
                  <div className="flex items-center gap-4 text-xs text-surface-500">
                    <span className="inline-flex items-center gap-1">
                      <span className="font-medium text-surface-700">{strategy.rules.length}</span> rule
                      {strategy.rules.length !== 1 ? 's' : ''}
                    </span>
                    <span className="inline-flex items-center gap-1">
                      Scorer: <span className="font-medium text-surface-700 capitalize">{strategy.scorer.type}</span>
                    </span>
                    <span className="inline-flex items-center gap-1">
                      LB:{' '}
                      <span className="font-medium text-surface-700">{capName(strategy.load_balancer_strategy)}</span>
                    </span>
                  </div>
                  <div className="flex items-center gap-2 pt-4 border-t border-surface-100 mt-3">
                    {!isActive && (
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => setActiveMutation.mutate(strategy.name)}
                        disabled={setActiveMutation.isPending}
                      >
                        <Circle size={12} />
                        Set Active
                      </Button>
                    )}
                    <Button size="sm" variant="outline" onClick={() => onEdit(strategy)}>
                      <Edit3 size={12} />
                      Edit
                    </Button>
                    <Button
                      size="sm"
                      variant="outline"
                      className="text-error hover:text-error hover:border-error/40"
                      onClick={() => setDeleteConfirm(strategy.name)}
                    >
                      <Trash2 size={12} />
                      Delete
                    </Button>
                  </div>
                </CardContent>
              </Card>
            )
          })}
        </div>
      )}

      {/* Delete confirmation */}
      {deleteConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <Card className="w-full max-w-sm mx-4">
            <CardHeader>
              <CardTitle>Delete Strategy</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-surface-600 mb-4">
                Are you sure you want to delete strategy{' '}
                <span className="font-medium text-surface-900">"{deleteConfirm}"</span>? This action cannot be undone.
              </p>
              <div className="flex justify-end gap-3">
                <Button variant="outline" onClick={() => setDeleteConfirm(null)}>
                  Cancel
                </Button>
                <Button
                  variant="destructive"
                  onClick={() => {
                    deleteMutation.mutate(deleteConfirm)
                    setDeleteConfirm(null)
                  }}
                  disabled={deleteMutation.isPending}
                >
                  Delete
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  )
}

export function StrategyListView(props: StrategyListViewProps) {
  return (
    <QueryProvider>
      <StrategyListViewContent {...props} />
    </QueryProvider>
  )
}
