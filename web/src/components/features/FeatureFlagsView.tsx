import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'
import { api, type FeatureFlag } from '../../lib/api'
import { featureLabel } from '../../lib/format'
import { logger } from '../../lib/logger'
import { qk } from '../../lib/query'
import { useApiMutation } from '../../lib/useApiMutation'
import { cn } from '../../lib/utils'
import { FeatureStatus } from '../settings/FeatureStatus'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { type Column, DataTable } from '../ui/DataTable'
import { EmptyState } from '../ui/empty-state'
import { AlertTriangle, Flag, Search, Zap } from '../ui/icons'
import { QueryProvider } from '../ui/query-provider'
import { Skeleton } from '../ui/skeleton'
import { Switch } from '../ui/switch'

function FeatureFlagsViewContent() {
  const [toggling, setToggling] = useState<string | null>(null)
  const [search, setSearch] = useState('')

  const toggleFlag = useApiMutation(
    (args: { feature_key: string; enabled: boolean }) => api.features.toggleFeature(args.feature_key, args.enabled),
    { invalidate: [qk.features] },
  )

  const {
    data: flags,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: qk.features,
    queryFn: () => api.features.getFeatures(),
  })
  const safeFlags = flags ?? []

  const filteredFlags = useMemo(() => {
    if (!search) return safeFlags
    const q = search.toLowerCase()
    return safeFlags.filter((f) => f.feature_key.toLowerCase().includes(q))
  }, [safeFlags, search])

  const handleToggle = async (flag: FeatureFlag) => {
    setToggling(flag.feature_key)
    try {
      await toggleFlag.mutateAsync({
        feature_key: flag.feature_key,
        enabled: !flag.enabled,
      })
      setToggling(null)
      toast.success(`Feature "${flag.feature_key}" ${!flag.enabled ? 'enabled' : 'disabled'}`)
    } catch (e) {
      logger.error('Failed to toggle feature flag:', e)
      setToggling(null)
      const errorMsg = e instanceof Error ? e.message : 'Failed to toggle feature'
      toast.error(errorMsg)
    }
  }

  const flagColumns: Column<FeatureFlag>[] = [
    {
      key: 'feature_key',
      header: 'Feature',
      render: (flag) => (
        <div className="flex flex-col">
          <span className="text-sm font-medium text-surface-900">{featureLabel(flag.feature_key)}</span>
          <span className="text-xs text-surface-500 font-mono">{flag.feature_key}</span>
        </div>
      ),
    },
    {
      key: 'enabled',
      header: 'Enabled',
      render: (flag) => (
        <Switch
          size="sm"
          checked={flag.enabled}
          onCheckedChange={() => handleToggle(flag)}
          disabled={toggling === flag.feature_key}
        />
      ),
    },
  ]

  return (
    <div className="space-y-6">
      {!isLoading && (
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold text-surface-900">Feature Flags</h2>
          <FeatureStatus type="count" enabled={safeFlags.filter((f) => f.enabled).length} total={safeFlags.length} />
        </div>
      )}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="text-base flex items-center gap-2">
              <Flag size={20} />
              Feature Flags
            </CardTitle>
          </div>
        </CardHeader>
        <CardContent>
          <div className="flex flex-wrap items-center gap-3 mb-4">
            <div className="relative flex-1 min-w-[200px] max-w-sm">
              <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-surface-400" />
              <input
                type="text"
                placeholder="Search features..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="w-full rounded-lg border border-surface-300 bg-white py-2 pl-10 pr-3 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
            </div>
          </div>

          {isLoading ? (
            <div className="rounded-xl border border-surface-200 bg-white shadow-card">
              <div className="flex border-b border-surface-200 px-4 py-3 gap-4">
                {Array.from({ length: 2 }).map((_, ci) => (
                  <Skeleton key={`h-${ci}`} className={cn('h-3', ci === 0 ? 'w-1/4' : 'w-1/6')} />
                ))}
              </div>
              {Array.from({ length: 4 }).map((_, ri) => (
                <div key={`r-${ri}`} className="flex border-b border-surface-100 px-4 py-3.5 gap-4 last:border-b-0">
                  {Array.from({ length: 2 }).map((_, ci) => (
                    <Skeleton key={`c-${ri}-${ci}`} className={cn('h-3', ci === 0 ? 'w-2/5' : 'w-1/5')} />
                  ))}
                </div>
              ))}
            </div>
          ) : error ? (
            <EmptyState
              title={error instanceof Error ? error.message : 'Failed to load features'}
              icon={<AlertTriangle size={20} />}
              action={{ label: 'Retry', onClick: () => refetch() }}
            />
          ) : filteredFlags.length === 0 ? (
            <EmptyState
              title="No feature flags"
              description={search ? 'No flags match your search' : 'No feature flags configured'}
              icon={<Zap size={20} />}
            />
          ) : (
            <DataTable columns={flagColumns} data={filteredFlags} keyExtractor={(flag) => flag.feature_key} />
          )}
        </CardContent>
      </Card>
    </div>
  )
}

export function FeatureFlagsView() {
  return (
    <QueryProvider>
      <FeatureFlagsViewContent />
    </QueryProvider>
  )
}
