import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { api } from '../../lib/api'
import { formatMs } from '../../lib/format'
import { logger } from '../../lib/logger'
import { qk } from '../../lib/query'
import { CHART_COLORS } from '../../lib/recharts-theme'
import LazyChart from '../charts/LazyChart'
import { FeatureStatus } from '../settings/FeatureStatus'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { DataTable } from '../ui/DataTable'
import { Activity, Clock, Database, Trash2, XCircle, Zap } from '../ui/icons'
import { QueryState } from '../ui/QueryState'
import { QueryProvider } from '../ui/query-provider'
import { StatCard } from '../ui/StatCard'

function formatTTL(seconds: number): string {
  if (seconds <= 0) return '—'
  if (seconds % 3600 === 0) return `${seconds / 3600}h`
  if (seconds % 60 === 0) return `${seconds / 60}m`
  return `${seconds}s`
}

function SemanticCacheViewContent() {
  const [flushing, setFlushing] = useState(false)

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: qk.semanticCache,
    queryFn: () => api.cache.getSemanticCacheSummary(),
  })

  const { data: costData } = useQuery({
    queryKey: qk.costSummary('24h'),
    queryFn: () => api.costs.getCostSummary('24h'),
  })

  useEffect(() => {
    const interval = setInterval(refetch, 30000)
    return () => clearInterval(interval)
  }, [refetch])

  if (isLoading) return <QueryState.Loading kind="card" count={5} />
  if (error) return <QueryState.Error message={error.message} onRetry={refetch} />
  if (!data) return null

  const totalOps = data.cache_hits_24h + data.cache_misses_24h
  const cacheSavings = costData?.savings_summary?.cache_savings

  return (
    <div className="space-y-6">
      <div className="flex flex-col gap-3">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-semibold text-surface-900">Semantic Cache</h2>
          <div className="flex items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={flushing}
              onClick={async () => {
                setFlushing(true)
                try {
                  await api.cache.flushCache()
                  toast.success('Cache flushed successfully')
                  refetch()
                } catch (e) {
                  logger.error('Failed to flush cache:', e)
                  toast.error('Failed to flush cache')
                } finally {
                  setFlushing(false)
                }
              }}
            >
              <Trash2 size={16} />
              {flushing ? 'Flushing...' : 'Flush Cache'}
            </Button>
            <FeatureStatus
              type="toggle"
              enabled={data.mode !== 'disabled'}
              disabled={!!data.redis_error}
              onToggle={async () => {
                const was = data.mode !== 'disabled'
                try {
                  await api.cache.toggleCacheMode(!was)
                  refetch()
                } catch (e) {
                  logger.error('Failed to toggle cache:', e)
                  toast.error('Failed to toggle cache')
                }
              }}
              label={data.mode !== 'disabled' ? 'Enabled' : 'Disabled'}
            />
          </div>
        </div>
        {data.redis_error && (
          <div className="rounded-lg border border-warning/20 bg-warning/5 px-4 py-3 text-sm text-warning">
            <p className="font-medium">Redis not available</p>
            <p className="mt-0.5 text-warning/80">{data.redis_error}</p>
          </div>
        )}
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5 gap-4">
        <StatCard title="Cache Hits (24h)" value={data.cache_hits_24h.toLocaleString()} icon={<Zap size={20} />} />
        <StatCard
          title="Cache Misses (24h)"
          value={data.cache_misses_24h.toLocaleString()}
          icon={<XCircle size={20} />}
        />
        <StatCard
          title="Hit Rate"
          value={`${data.hit_rate_pct}%`}
          description={`${totalOps.toLocaleString()} total lookups`}
          icon={<Activity size={20} />}
        />
        <StatCard
          title="Cache Size"
          value={data.cache_size_entries.toLocaleString()}
          description={`${data.cache_size_mb} MB stored`}
          icon={<Database size={20} />}
        />
        <StatCard
          title="Avg Latency Saved"
          value={formatMs(data.avg_latency_saved_ms)}
          description={cacheSavings !== undefined ? `~$${cacheSavings.toFixed(2)} cost avoided` : undefined}
          icon={<Clock size={20} />}
        />
      </div>

      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-surface-900">Hit / Miss Rate (24h)</h2>
        <div className="flex items-center gap-2">
          <span className="inline-flex items-center gap-1.5 text-xs text-surface-500">
            <span className="inline-block h-2 w-2 rounded-full bg-success animate-pulse" />
            Live
          </span>
        </div>
      </div>

      <Card>
        <CardContent className="p-4">
          <LazyChart
            type="bar"
            data={data.hourly_data as unknown as Record<string, unknown>[]}
            xKey="time"
            height={250}
            showLegend
            series={[
              { dataKey: 'hits', color: CHART_COLORS.success, name: 'Hits', stackId: 'a' },
              { dataKey: 'misses', color: CHART_COLORS.warning, name: 'Misses', stackId: 'a' },
            ]}
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Top Cached Queries</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <DataTable
            columns={[
              {
                key: 'query_preview',
                header: 'Query',
                className: 'max-w-[280px]',
                render: (q) => <span className="truncate font-mono text-xs">{q.query_preview || '(empty)'}</span>,
              },
              {
                key: 'model',
                header: 'Model',
                render: (q) => <span className="font-medium text-surface-600">{q.model}</span>,
              },
              {
                key: 'hit_count',
                header: 'Hit Count',
                render: (q) => <span className="font-mono text-xs text-surface-700">{q.hit_count}</span>,
              },
              {
                key: 'avg_latency',
                header: 'Avg Latency',
                render: (q) => (
                  <span className="font-mono text-xs text-surface-500">{q.avg_latency.toFixed(2)} ms</span>
                ),
              },
              {
                key: 'last_accessed',
                header: 'Last Accessed',
                render: (q) => (
                  <span className="font-mono text-xs text-surface-500">
                    {new Date(q.last_accessed).toLocaleString()}
                  </span>
                ),
              },
            ]}
            data={data.top_queries}
            keyExtractor={(q) => q.query_preview + q.model}
            emptyMessage="No cached queries found in the last 24 hours."
          />
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Cache Configuration</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-6">
            <div>
              <p className="text-sm font-medium text-surface-700 mb-1">Default TTL</p>
              <p className="text-2xl font-bold text-surface-900 font-mono">{formatTTL(data.ttl_seconds)}</p>
              <p className="text-xs text-surface-500 mt-0.5">cache entry expiration</p>
            </div>

            <div>
              <p className="text-sm font-medium text-surface-700 mb-1">Similarity Threshold</p>
              <p className="text-2xl font-bold text-surface-900 font-mono">{data.similarity_threshold}</p>
              <p className="text-xs text-surface-500 mt-0.5">cosine similarity minimum</p>
            </div>

            <div>
              <p className="text-sm font-medium text-surface-700 mb-1">Mode</p>
              <div className="flex items-center gap-2">
                <span
                  className={`inline-block h-2.5 w-2.5 rounded-full ${data.mode !== 'disabled' ? 'bg-success' : 'bg-surface-400'}`}
                />
                <p className="text-sm font-medium text-surface-900">
                  {data.mode !== 'disabled' ? 'Enabled' : 'Disabled'}
                </p>
              </div>
              <p className="text-xs text-surface-500 mt-0.5">
                {data.mode !== 'disabled' ? 'Feature flag on' : 'cache not available'}
              </p>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

export function SemanticCacheView() {
  return (
    <QueryProvider>
      <SemanticCacheViewContent />
    </QueryProvider>
  )
}
