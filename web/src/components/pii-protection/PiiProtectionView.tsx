import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'
import { api, type PIIEvent } from '../../lib/api'
import type { PIIPattern } from '../../lib/api/types'
import { qk } from '../../lib/query'
import { CHART_COLORS } from '../../lib/recharts-theme'
import LazyChart from '../charts/LazyChart'
import { FeatureStatus } from '../settings/FeatureStatus'
import { FeatureTabLayout } from '../settings/FeatureTabLayout'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { type Column, DataTable } from '../ui/DataTable'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '../ui/dialog'
import { EmptyState } from '../ui/empty-state'
import {
  AlertTriangle,
  Check,
  CheckCircle,
  Download,
  Edit3,
  Eye,
  Plus,
  RefreshCw,
  Search,
  Shield,
  Trash2,
  XCircle,
} from '../ui/icons'
import { Input } from '../ui/input'
import { QueryProvider } from '../ui/query-provider'
import { StatCard } from '../ui/StatCard'

const statusColors: Record<string, string> = {
  mask: 'bg-info/10 text-info border-info/20',
  block: 'bg-error/10 text-error border-error/20',
  reversible: 'bg-success/10 text-success border-success/20',
}

function StatusBadge({ mode }: { mode: string }) {
  return (
    <span
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium capitalize ${statusColors[mode] || 'bg-surface-100 text-surface-600'}`}
    >
      {mode}
    </span>
  )
}

function PiiProtectionViewContent() {
  const [search, setSearch] = useState('')
  const [modeFilter, setModeFilter] = useState('')
  const [blockedFilter, setBlockedFilter] = useState('')
  const queryClient = useQueryClient()

  const { data: config, isLoading: configLoading } = useQuery({
    queryKey: qk.piiConfig,
    queryFn: () => api.pii.getPIIConfig(),
  })

  const toggleMutation = useMutation({
    mutationFn: (enabled: boolean) => api.pii.togglePII(enabled),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: qk.piiConfig })
    },
  })

  const piiEnabled = config?.enabled ?? true

  const {
    data: events = [],
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: qk.piiProtection,
    queryFn: () => api.pii.getPIIEvents({ per_page: 100 }).then((d) => d.data),
  })

  const { data: stats } = useQuery({
    queryKey: qk.piiStats,
    queryFn: () => api.pii.getPIIStats().catch(() => null),
  })

  // ── Patterns State ──
  const [patternFormOpen, setPatternFormOpen] = useState(false)
  const [editingPattern, setEditingPattern] = useState<PIIPattern | null>(null)
  const [patternFormName, setPatternFormName] = useState('')
  const [patternFormRegex, setPatternFormRegex] = useState('')
  const [patternFormEnabled, setPatternFormEnabled] = useState(true)
  const [patternFormAction, setPatternFormAction] = useState('mask')
  const [deletePatternConfirm, setDeletePatternConfirm] = useState<PIIPattern | null>(null)

  const patternsQuery = useQuery({
    queryKey: qk.piiPatterns,
    queryFn: () => api.pii.listPIIPatterns(),
  })
  const createPatternMutation = useMutation({
    mutationFn: (data: { name: string; regex: string; enabled?: boolean; action?: string }) =>
      api.pii.createPIIPattern(data),
    onSuccess: () => {
      toast.success('Pattern created')
      setPatternFormOpen(false)
      queryClient.invalidateQueries({ queryKey: qk.piiPatterns })
    },
    onError: (e: Error) => toast.error(e.message || 'Failed to create pattern'),
  })

  const updatePatternMutation = useMutation({
    mutationFn: ({
      name,
      regex,
      enabled,
      action,
    }: {
      name: string
      regex?: string
      enabled?: boolean
      action?: string
    }) => api.pii.updatePIIPattern(name, { regex, enabled, action }),
    onSuccess: () => {
      toast.success('Pattern updated')
      queryClient.invalidateQueries({ queryKey: qk.piiPatterns })
    },
    onError: (e: Error) => toast.error(e.message || 'Failed to update pattern'),
  })

  const togglePatternMutation = useMutation({
    mutationFn: ({ name, enabled }: { name: string; enabled: boolean }) => api.pii.updatePIIPattern(name, { enabled }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: qk.piiPatterns })
    },
    onError: (e: Error) => toast.error(e.message || 'Failed to toggle pattern'),
  })

  const setPatternActionMutation = useMutation({
    mutationFn: ({ name, action }: { name: string; action: string }) => api.pii.updatePIIPattern(name, { action }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: qk.piiPatterns })
    },
    onError: (e: Error) => toast.error(e.message || 'Failed to set pattern action'),
  })
  const deletePatternMutation = useMutation({
    mutationFn: (name: string) => api.pii.deletePIIPattern(name),
    onSuccess: () => {
      toast.success('Pattern deleted')
      setDeletePatternConfirm(null)
      queryClient.invalidateQueries({ queryKey: qk.piiPatterns })
    },
    onError: (e: Error) => toast.error(e.message || 'Failed to delete pattern'),
  })

  const reloadPatternsMutation = useMutation({
    mutationFn: () => api.pii.reloadPIIPatterns(),
    onSuccess: () => {
      toast.success('Patterns reloaded from database')
      queryClient.invalidateQueries({ queryKey: qk.piiPatterns })
    },
    onError: (e: Error) => toast.error(e.message || 'Failed to reload patterns'),
  })

  const openCreateForm = () => {
    setEditingPattern(null)
    setPatternFormName('')
    setPatternFormRegex('')
    setPatternFormEnabled(true)
    setPatternFormAction('mask')
    setPatternFormOpen(true)
  }

  const openEditForm = (pattern: PIIPattern) => {
    setEditingPattern(pattern)
    setPatternFormName(pattern.name)
    setPatternFormRegex(pattern.regex)
    setPatternFormEnabled(pattern.enabled)
    setPatternFormAction(pattern.action)
    setPatternFormOpen(true)
  }

  const handleSavePattern = () => {
    if (editingPattern) {
      updatePatternMutation.mutate({
        name: editingPattern.name,
        regex: patternFormRegex,
        enabled: patternFormEnabled,
        action: patternFormAction,
      })
    } else {
      createPatternMutation.mutate({
        name: patternFormName,
        regex: patternFormRegex,
        enabled: patternFormEnabled,
        action: patternFormAction,
      })
    }
  }

  const patternColumns: Column<PIIPattern>[] = [
    {
      key: 'name',
      header: 'Name',
      render: (p) => <span className="font-mono text-sm font-medium text-surface-900">{p.name}</span>,
    },
    {
      key: 'regex',
      header: 'Regex',
      render: (p) => (
        <code className="max-w-[260px] truncate block text-xs text-surface-600 font-mono" title={p.regex}>
          {p.regex}
        </code>
      ),
    },
    {
      key: 'enabled',
      header: 'Enabled',
      render: (p) => (
        <button
          onClick={() => togglePatternMutation.mutate({ name: p.name, enabled: !p.enabled })}
          disabled={togglePatternMutation.isPending}
          className={`inline-flex h-5 w-9 items-center rounded-full transition-colors ${p.enabled ? 'bg-brand-500' : 'bg-surface-200'}`}
          title={p.enabled ? 'Disable' : 'Enable'}
        >
          <span
            className={`inline-block h-4 w-4 transform rounded-full bg-white shadow transition-transform ${p.enabled ? 'translate-x-4' : 'translate-x-0.5'}`}
          />
        </button>
      ),
    },
    {
      key: 'action',
      header: 'Action',
      render: (p) => (
        <select
          value={p.action}
          onChange={(e) => setPatternActionMutation.mutate({ name: p.name, action: e.target.value })}
          disabled={setPatternActionMutation.isPending}
          className="rounded-md border border-surface-300 bg-white px-2 py-1 text-xs text-surface-700 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
        >
          <option value="mask">mask</option>
          <option value="mask_reversible">mask_reversible</option>
          <option value="block">block</option>
          <option value="log_only">log_only</option>
        </select>
      ),
    },
    {
      key: 'actions',
      header: '',
      className: 'w-16',
      render: (p) => (
        <div className="flex items-center gap-1">
          <button
            onClick={() => openEditForm(p)}
            className="p-1 text-surface-400 hover:text-brand-600 transition-colors"
            title="Edit pattern"
          >
            <Edit3 size={14} />
          </button>
          <button
            onClick={() => setDeletePatternConfirm(p)}
            className="p-1 text-surface-400 hover:text-destructive transition-colors"
            title="Delete pattern"
          >
            <Trash2 size={14} />
          </button>
        </div>
      ),
    },
  ]

  const filtered = useMemo(() => {
    let items = [...events]
    if (search) {
      const q = search.toLowerCase()
      items = items.filter(
        (e) =>
          e.entity_type.toLowerCase().includes(q) ||
          e.matched_value_preview.toLowerCase().includes(q) ||
          e.value?.toLowerCase().includes(q),
      )
    }
    if (modeFilter) items = items.filter((e) => e.mode === modeFilter)
    if (blockedFilter === 'blocked') items = items.filter((e) => e.blocked)
    else if (blockedFilter === 'allowed') items = items.filter((e) => !e.blocked)
    return items.sort((a, b) => new Date(b.timestamp).getTime() - new Date(a.timestamp).getTime())
  }, [search, modeFilter, blockedFilter, events])

  const typeDist = useMemo(() => {
    const counts: Record<string, number> = {}
    events.forEach((e) => {
      counts[e.entity_type] = (counts[e.entity_type] || 0) + 1
    })
    return Object.entries(counts)
      .map(([type, count]) => ({ type, count }))
      .sort((a, b) => b.count - a.count)
  }, [events])

  const totalScanned = stats?.total_events ?? 0
  const piiDetected = events.length
  const blocked = stats?.blocked_count ?? 0
  const masked = stats?.masked_count ?? 0

  const renderPatternsSection = () => (
    <>
      <div className="flex items-start justify-between">
        <div>
          <h3 className="text-base font-semibold text-surface-900">PII Detection Patterns</h3>
          <p className="text-sm text-surface-500 mt-0.5">Manage regex patterns used to detect sensitive data.</p>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => reloadPatternsMutation.mutate()}
            disabled={reloadPatternsMutation.isPending}
          >
            <RefreshCw size={14} className={reloadPatternsMutation.isPending ? 'animate-spin' : ''} />
            Reload
          </Button>
          <Button size="sm" onClick={openCreateForm}>
            <Plus size={14} />
            Add Pattern
          </Button>
        </div>
      </div>

      <Card>
        <CardContent className="p-0">
          <DataTable
            columns={patternColumns}
            data={patternsQuery.data ?? []}
            keyExtractor={(p) => p.name}
            loading={patternsQuery.isLoading}
            error={patternsQuery.error as Error | null}
            onRetry={() => patternsQuery.refetch()}
            emptyMessage="No patterns configured. Add one to start detecting PII."
          />
        </CardContent>
      </Card>
    </>
  )

  if (error && events.length === 0) {
    return (
      <EmptyState
        icon={<AlertTriangle size={48} strokeWidth={1.5} />}
        title="Failed to load PII events"
        description={error instanceof Error ? error.message : 'An unexpected error occurred.'}
        action={{ label: 'Retry', onClick: () => refetch() }}
      />
    )
  }
  if (!isLoading && events.length === 0) {
    return (
      <div className="space-y-6">
        <EmptyState
          icon={<Shield size={48} strokeWidth={1.5} />}
          title="No PII detected yet."
          description="PII protection is active and scanning requests. Results will appear here once sensitive data is detected."
        />
        {renderPatternsSection()}
      </div>
    )
  }

  const columns: Column<PIIEvent>[] = [
    {
      key: 'timestamp',
      header: 'Timestamp',
      render: (evt) => (
        <span className="font-mono text-xs text-surface-500">{new Date(evt.timestamp).toLocaleString()}</span>
      ),
    },
    {
      key: 'key',
      header: 'User/Key',
      render: (evt) =>
        evt.key?.key_name ? (
          <div className="flex items-center gap-1.5">
            <span className="font-medium">{evt.key.key_name}</span>
            <span className="text-surface-400 text-xs">#{evt.api_key_id}</span>
          </div>
        ) : (
          <span className="text-surface-400">#{evt.api_key_id ?? '-'}</span>
        ),
    },
    {
      key: 'entity_type',
      header: 'Type',
      render: (evt) => <span className="font-medium text-surface-900">{evt.entity_type}</span>,
    },
    {
      key: 'value',
      header: 'Value',
      render: (evt) => <span className="font-mono text-surface-600">{evt.value ?? evt.matched_value_preview}</span>,
    },
    {
      key: 'mode',
      header: 'Mode',
      render: (evt) => <StatusBadge mode={evt.mode} />,
    },
    {
      key: 'blocked',
      header: 'Blocked',
      render: (evt) =>
        evt.blocked ? (
          <span className="inline-flex items-center gap-1 text-xs font-medium text-error">
            <XCircle size={12} />
            Blocked
          </span>
        ) : (
          <span className="inline-flex items-center gap-1 text-xs font-medium text-success">
            <CheckCircle size={12} />
            Allowed
          </span>
        ),
    },
  ]

  return (
    <>
      <FeatureTabLayout
        title="PII Protection"
        description="Masks sensitive data in requests and responses."
        enabled={piiEnabled}
        status={
          <FeatureStatus
            type="toggle"
            enabled={piiEnabled}
            onToggle={() => toggleMutation.mutate(!piiEnabled)}
            disabled={toggleMutation.isPending || configLoading}
            label={toggleMutation.isPending ? 'Updating...' : piiEnabled ? 'Enabled' : 'Disabled'}
          />
        }
        loading={isLoading}
        stats={
          <>
            <StatCard
              title="Total Events"
              value={totalScanned.toLocaleString()}
              description="PII events recorded"
              icon={<Eye size={20} />}
            />
            <StatCard
              title="PII Detected"
              value={piiDetected}
              description={
                totalScanned > 0 ? `${((piiDetected / totalScanned) * 100).toFixed(1)}% of total` : 'No events'
              }
              icon={<AlertTriangle size={20} />}
            />
            <StatCard
              title="Blocked"
              value={blocked}
              description={
                totalScanned > 0 ? `${((blocked / totalScanned) * 100).toFixed(1)}% of total` : 'Requests denied'
              }
              icon={<Shield size={20} />}
            />
            <StatCard
              title="Masked"
              value={masked}
              description={
                totalScanned > 0 ? `${((masked / totalScanned) * 100).toFixed(1)}% of total` : 'PII redacted'
              }
              icon={<Check size={20} />}
            />
          </>
        }
        table={
          <>
            {renderPatternsSection()}

            <Card>
              <CardHeader>
                <CardTitle className="text-base">PII Type Distribution</CardTitle>
              </CardHeader>
              <CardContent>
                <LazyChart
                  type="bar"
                  data={typeDist}
                  xKey="type"
                  yKey="type"
                  height={256}
                  layout="vertical"
                  xAxisType="number"
                  yAxisType="category"
                  xAxisWidth={100}
                  series={[{ dataKey: 'count', color: CHART_COLORS.brand, name: 'Count' }]}
                  tooltipFormatter={(value) => `${value} events`}
                />
              </CardContent>
            </Card>

            <div className="flex flex-wrap items-center gap-3">
              <div className="relative flex-1 min-w-[200px] max-w-sm">
                <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-surface-400" />
                <input
                  type="text"
                  placeholder="Search by type or value..."
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  className="w-full rounded-lg border border-surface-300 bg-white py-2 pl-10 pr-3 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                />
              </div>
              <select
                value={modeFilter}
                onChange={(e) => setModeFilter(e.target.value)}
                className="rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-700 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              >
                <option value="">All Modes</option>
                <option value="mask">Mask</option>
                <option value="block">Block</option>
                <option value="reversible">Reversible</option>
              </select>
              <select
                value={blockedFilter}
                onChange={(e) => setBlockedFilter(e.target.value)}
                className="rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-700 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              >
                <option value="">All Events</option>
                <option value="blocked">Blocked</option>
                <option value="allowed">Allowed</option>
              </select>
              <a href="/api/pii-export?format=csv" rel="noreferrer">
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

      <Dialog open={patternFormOpen} onOpenChange={setPatternFormOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingPattern ? 'Edit Pattern' : 'Add Pattern'}</DialogTitle>
            <DialogDescription>
              {editingPattern
                ? 'Update the regex. Name cannot be changed; delete and recreate to rename.'
                : 'Define a new regex pattern to detect sensitive data.'}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-surface-700">Name</label>
              <Input
                value={patternFormName}
                onChange={(e) => setPatternFormName(e.target.value)}
                placeholder="e.g. credit_card"
                disabled={!!editingPattern}
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-surface-700">Regex Pattern</label>
              <Input
                value={patternFormRegex}
                onChange={(e) => setPatternFormRegex(e.target.value)}
                placeholder="e.g. \b\d{4}[- ]?\d{4}[- ]?\d{4}[- ]?\d{4}\b"
                className="font-mono text-xs"
              />
            </div>
            <div className="flex gap-4">
              <div className="space-y-1.5 flex-1">
                <label className="text-xs font-medium text-surface-700">Action</label>
                <select
                  value={patternFormAction}
                  onChange={(e) => setPatternFormAction(e.target.value)}
                  className="w-full rounded-md border border-surface-300 bg-white px-3 py-2 text-sm text-surface-700 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                >
                  <option value="mask">mask</option>
                  <option value="mask_reversible">mask_reversible</option>
                  <option value="block">block</option>
                  <option value="log_only">log_only</option>
                </select>
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-surface-700">Enabled</label>
                <div className="flex h-10 items-center">
                  <button
                    type="button"
                    onClick={() => setPatternFormEnabled((v) => !v)}
                    className={`inline-flex h-6 w-11 items-center rounded-full transition-colors ${patternFormEnabled ? 'bg-brand-500' : 'bg-surface-200'}`}
                    aria-pressed={patternFormEnabled}
                  >
                    <span
                      className={`inline-block h-5 w-5 transform rounded-full bg-white shadow transition-transform ${patternFormEnabled ? 'translate-x-5' : 'translate-x-0.5'}`}
                    />
                  </button>
                </div>
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPatternFormOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={handleSavePattern}
              disabled={
                createPatternMutation.isPending ||
                updatePatternMutation.isPending ||
                !patternFormName.trim() ||
                !patternFormRegex.trim()
              }
            >
              {createPatternMutation.isPending || updatePatternMutation.isPending ? 'Saving...' : 'Save Pattern'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={deletePatternConfirm !== null}
        onOpenChange={(o) => {
          if (!o) setDeletePatternConfirm(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <div className="flex items-center gap-3">
              <div className="rounded-full bg-error/10 p-2 text-error">
                <AlertTriangle size={20} />
              </div>
              <div>
                <DialogTitle>Delete Pattern</DialogTitle>
                <DialogDescription>
                  Are you sure you want to delete{' '}
                  <span className="font-medium text-surface-900">&ldquo;{deletePatternConfirm?.name}&rdquo;</span>? This
                  action cannot be undone and will affect PII detection immediately.
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeletePatternConfirm(null)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              onClick={() => deletePatternConfirm && deletePatternMutation.mutate(deletePatternConfirm.name)}
              disabled={deletePatternMutation.isPending}
            >
              {deletePatternMutation.isPending ? 'Deleting...' : 'Yes, Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

export function PiiProtectionView() {
  return (
    <QueryProvider>
      <PiiProtectionViewContent />
    </QueryProvider>
  )
}
