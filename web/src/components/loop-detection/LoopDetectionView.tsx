import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { api, type LoopEvent } from '../../lib/api'
import { qk } from '../../lib/query'
import { CHART_COLORS } from '../../lib/recharts-theme'
import LazyChart from '../charts/LazyChart'
import { FeatureStatus } from '../settings/FeatureStatus'
import { FeatureTabLayout } from '../settings/FeatureTabLayout'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { type Column, DataTable } from '../ui/DataTable'
import { EmptyState } from '../ui/empty-state'
import { Activity, AlertTriangle, Download, Key, Search, Shield } from '../ui/icons'
import { QueryProvider } from '../ui/query-provider'
import { StatCard } from '../ui/StatCard'
import { LoopSettingsForm } from './LoopSettingsForm'

function ActionBadge({ action }: { action: string }) {
  const colors: Record<string, string> = {
    blocked: 'bg-error/10 text-error border-error/20',
    throttled: 'bg-warning/10 text-warning border-warning/20',
    alerted: 'bg-info/10 text-info border-info/20',
  }
  const label = action.charAt(0).toUpperCase() + action.slice(1)
  return (
    <span
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${colors[action] || 'bg-surface-100 text-surface-600'}`}
    >
      {label}
    </span>
  )
}

function LoopDetectionViewContent() {
  const [search, setSearch] = useState('')
  const [actionFilter, setActionFilter] = useState('')
  const queryClient = useQueryClient()

  const { data: features = [] } = useQuery({
    queryKey: qk.features,
    queryFn: () => api.features.getFeatures(),
  })

  const loopFeature = features.find((f) => f.feature_key === 'loop_detection')
  const shieldEnabled = loopFeature ? loopFeature.enabled : false

  const toggleMutation = useMutation({
    mutationFn: (enabled: boolean) => api.features.toggleFeature('loop_detection', enabled),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: qk.features })
    },
  })

  const {
    data: events = [],
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: qk.loopDetection,
    queryFn: () =>
      api.loops
        .getLoopEvents({ per_page: 100 })
        .then((r) => r.data)
        .catch(() => [] as LoopEvent[]),
  })

  const filtered = useMemo(() => {
    let items = [...events].sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime())
    if (search) {
      const q = search.toLowerCase()
      items = items.filter(
        (e) =>
          e.fingerprint.toLowerCase().includes(q) ||
          e.action_taken.toLowerCase().includes(q) ||
          e.key?.key_name?.toLowerCase().includes(q),
      )
    }
    if (actionFilter) {
      items = items.filter((e) => e.action_taken === actionFilter)
    }
    return items
  }, [search, actionFilter, events])

  const stats = useMemo(() => {
    const total = events.length
    const blocked = events.filter((e) => e.action_taken === 'blocked').length
    const uniqueKeys = new Set(events.map((e) => e.key?.id).filter(Boolean)).size
    const avgRepeat = total > 0 ? (events.reduce((s, e) => s + e.repeat_count, 0) / total).toFixed(1) : '0'
    return { total, blocked, uniqueKeys, avgRepeat }
  }, [events])

  const hourlyTrend = useMemo(() => {
    const counts: Record<string, number> = {}
    events.forEach((e) => {
      const hour = `${new Date(e.timestamp).toISOString().slice(0, 13)}:00`
      counts[hour] = (counts[hour] || 0) + 1
    })
    return Object.entries(counts)
      .map(([time, count]) => ({ time, count }))
      .sort((a, b) => a.time.localeCompare(b.time))
      .slice(-24)
  }, [events])

  const columns: Column<LoopEvent>[] = [
    {
      key: 'timestamp',
      header: 'Timestamp',
      render: (evt) => (
        <span className="font-mono text-xs text-surface-500">{new Date(evt.timestamp).toLocaleString()}</span>
      ),
    },
    {
      key: 'key',
      header: 'Key',
      render: (evt) =>
        evt.key ? (
          <div className="flex items-center gap-1.5">
            <span className="font-mono text-xs text-surface-600">{evt.key.key_prefix}</span>
            <span className="text-xs text-surface-700">{evt.key.key_name}</span>
            {evt.key.owner_name && (
              <span className="text-[10px] font-medium px-1.5 py-0.5 rounded-full bg-brand-50 text-brand-700 border border-brand-200">
                {evt.key.owner_name}
              </span>
            )}
          </div>
        ) : (
          <span className="text-xs text-surface-400 font-mono">#{evt.api_key_id ?? '-'}</span>
        ),
    },
    {
      key: 'fingerprint',
      header: 'Fingerprint',
      render: (evt) => <span className="font-mono text-xs text-surface-700">{evt.fingerprint.slice(0, 16)}…</span>,
    },
    {
      key: 'repeat_count',
      header: 'Repeats',
      render: (evt) => <span className="font-mono text-xs text-surface-700">{evt.repeat_count}</span>,
    },
    {
      key: 'action_taken',
      header: 'Action',
      render: (evt) => <ActionBadge action={evt.action_taken} />,
    },
  ]

  if (error && events.length === 0) {
    return (
      <EmptyState
        icon={<AlertTriangle size={48} strokeWidth={1.5} />}
        title="Failed to load loop events"
        description={error instanceof Error ? error.message : 'An unexpected error occurred.'}
        action={{ label: 'Retry', onClick: () => refetch() }}
      />
    )
  }

  if (!isLoading && events.length === 0) {
    return (
      <EmptyState
        icon={<Shield size={48} strokeWidth={1.5} />}
        title="No loop events yet."
        description="Loop detection is active and monitoring requests. Results will appear here when repeating patterns are detected."
      />
    )
  }

  return (
    <FeatureTabLayout
      title="Loop Detection"
      description="Detects and blocks agentic loops — runaway agents repeating the same call pattern."
      enabled={shieldEnabled}
      status={
        <FeatureStatus
          type="toggle"
          enabled={shieldEnabled}
          onToggle={() => toggleMutation.mutate(!shieldEnabled)}
          disabled={toggleMutation.isPending}
          label={toggleMutation.isPending ? 'Updating...' : shieldEnabled ? 'Enabled' : 'Disabled'}
        />
      }
      loading={isLoading}
      config={<LoopSettingsForm />}
      stats={
        <>
          <StatCard
            title="Total Events (24h)"
            value={stats.total.toLocaleString()}
            description="loop events detected"
            icon={<Shield size={20} />}
          />
          <StatCard
            title="Blocked"
            value={stats.blocked}
            description={
              stats.total > 0 ? `${((stats.blocked / stats.total) * 100).toFixed(1)}% of total` : 'No events'
            }
            icon={<AlertTriangle size={20} />}
          />
          <StatCard
            title="Unique Keys"
            value={stats.uniqueKeys}
            description="keys affected by loops"
            icon={<Key size={20} />}
          />
          <StatCard
            title="Avg Repeat Count"
            value={stats.avgRepeat}
            description="repetitions per event"
            icon={<Activity size={20} />}
          />
        </>
      }
      table={
        <>
          {hourlyTrend.length > 0 && (
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Loop Events Over Time</CardTitle>
              </CardHeader>
              <CardContent>
                <LazyChart
                  type="bar"
                  data={hourlyTrend}
                  xKey="time"
                  series={[{ dataKey: 'count', color: CHART_COLORS.brand, name: 'Events' }]}
                  height={200}
                  tooltipFormatter={(value) => `${value} events`}
                />
              </CardContent>
            </Card>
          )}

          <div className="flex flex-wrap items-center gap-3">
            <div className="relative flex-1 min-w-[200px] max-w-sm">
              <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-surface-400" />
              <input
                type="text"
                placeholder="Search by fingerprint, key, or action..."
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                className="w-full rounded-lg border border-surface-300 bg-white py-2 pl-10 pr-3 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
            </div>
            <select
              value={actionFilter}
              onChange={(e) => setActionFilter(e.target.value)}
              className="rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-700 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
            >
              <option value="">All Actions</option>
              <option value="blocked">Blocked</option>
              <option value="throttled">Throttled</option>
              <option value="alerted">Alerted</option>
            </select>
            <a href="/api/loop-export?format=csv" rel="noreferrer">
              <Button variant="outline" size="sm">
                <Download size={14} />
                Export CSV
              </Button>
            </a>
          </div>

          <Card>
            <CardContent className="p-0">
              <DataTable columns={columns} data={filtered.slice(0, 25)} keyExtractor={(evt) => evt.id} />
            </CardContent>
          </Card>
        </>
      }
    />
  )
}

export function LoopDetectionView() {
  return (
    <QueryProvider>
      <LoopDetectionViewContent />
    </QueryProvider>
  )
}
