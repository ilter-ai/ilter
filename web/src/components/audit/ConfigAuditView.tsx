import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { api, type ConfigAuditEntry } from '../../lib/api'
import { cn } from '../../lib/utils'
import { DetailPanelShell } from '../logs/DetailPanelShell'
import { LogsEmptyState, LogsTableSkeleton } from '../logs/LogsTableState'
import { timeRangeToParams } from '../logs/logsTime'
import { DetailField, TimeAgo } from '../logs/primitives'
import { TimeRangeFilter } from '../logs/TimeRangeFilter'
import { useLogsPageState } from '../logs/useLogsPageState'
import { Button } from '../ui/button'
import { Card, CardContent } from '../ui/card'
import { Activity, Download, Plus, RefreshCw, Trash2 } from '../ui/icons'
import { Pagination } from '../ui/Pagination'
import { QueryProvider } from '../ui/query-provider'
import { StatCard } from '../ui/StatCard'
import { useExport } from '../ui/useExport'

function actionBadge(action: string) {
  const styles: Record<string, string> = {
    create: 'bg-green-50 text-green-700 border-green-200',
    update: 'bg-blue-50 text-blue-700 border-blue-200',
    delete: 'bg-red-50 text-red-700 border-red-200',
  }
  return `inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${styles[action] || 'bg-surface-100 text-surface-600 border-surface-200'}`
}

function formatJson(raw: string | null | undefined): string {
  if (raw == null) return ''
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function DetailPanel({ entry, onClose }: { entry: ConfigAuditEntry; onClose: () => void }) {
  return (
    <DetailPanelShell title="Config Change Details" onClose={onClose}>
      <div className="grid grid-cols-2 gap-4">
        <DetailField label="Entry ID" value={entry.id} mono />
        <DetailField label="When" value={new Date(entry.performed_at).toLocaleString()} />
        <DetailField label="Entity Type" value={entry.entity_type} />
        <DetailField label="Entity ID" value={entry.entity_id} mono />
        <DetailField label="Action" value={<span className={actionBadge(entry.action)}>{entry.action}</span>} />
        <DetailField label="Performed By" value={entry.performed_by || 'system'} mono />
      </div>

      {entry.old_values && (
        <div>
          <p className="text-xs font-medium text-surface-500 uppercase tracking-wider mb-1">Before</p>
          <pre className="rounded-lg bg-surface-50 p-3 text-xs font-mono text-surface-700 overflow-x-auto max-h-48 overflow-y-auto whitespace-pre-wrap">
            {formatJson(entry.old_values)}
          </pre>
        </div>
      )}

      {entry.new_values && (
        <div>
          <p className="text-xs font-medium text-surface-500 uppercase tracking-wider mb-1">After</p>
          <pre className="rounded-lg bg-surface-50 p-3 text-xs font-mono text-surface-700 overflow-x-auto max-h-48 overflow-y-auto whitespace-pre-wrap">
            {formatJson(entry.new_values)}
          </pre>
        </div>
      )}
    </DetailPanelShell>
  )
}

const ENTITY_TYPES = ['api_key', 'user', 'group', 'group_membership', 'mcp_grant', 'mcp_default_policy']

function ConfigAuditViewContent() {
  const [page, setPage] = useState(1)
  const [entityTypeFilter, setEntityTypeFilter] = useState('')
  const [actionFilter, setActionFilter] = useState('')
  const [selectedEntry, setSelectedEntry] = useState<ConfigAuditEntry | null>(null)
  const limit = 20

  const { timeRange, customFrom, customTo, polling, setPolling, handleTimeRange, handleCustomFrom, handleCustomTo } =
    useLogsPageState()

  const { exportJson, exportCsv } = useExport()

  const handleTimeRangeWithReset = (range: typeof timeRange) => {
    handleTimeRange(range)
    setPage(1)
  }

  const handleCustomFromWithReset = (val: string) => {
    handleCustomFrom(val)
    setPage(1)
  }

  const handleCustomToWithReset = (val: string) => {
    handleCustomTo(val)
    setPage(1)
  }

  const { from, to } = useMemo(
    () => timeRangeToParams(timeRange, customFrom, customTo),
    [timeRange, customFrom, customTo],
  )

  const queryParams = useMemo(
    () => ({
      page,
      limit,
      ...(entityTypeFilter ? { entity_type: entityTypeFilter } : {}),
      ...(actionFilter ? { action: actionFilter } : {}),
      ...(from ? { start: from } : {}),
      ...(to ? { end: to } : {}),
    }),
    [page, entityTypeFilter, actionFilter, from, to],
  )

  const {
    data: response,
    isLoading,
    refetch,
  } = useQuery({
    queryKey: ['config-audit', queryParams],
    queryFn: () => api.access.listConfigAuditLog(queryParams),
    refetchInterval: polling ? 30000 : false,
  })

  const entries = response?.items ?? []
  const total = response?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / limit))

  const stats = useMemo(() => {
    const creates = entries.filter((e) => e.action === 'create').length
    const updates = entries.filter((e) => e.action === 'update').length
    const deletes = entries.filter((e) => e.action === 'delete').length
    return { creates, updates, deletes }
  }, [entries])

  const handleExportJson = () => {
    exportJson(entries, `admin-activity-${new Date().toISOString().slice(0, 10)}.json`)
  }

  const handleExportCsv = () => {
    const exportData = entries.map((e) => ({
      ID: e.id,
      When: new Date(e.performed_at).toLocaleString(),
      EntityType: e.entity_type,
      EntityID: e.entity_id,
      Action: e.action,
      PerformedBy: e.performed_by || 'system',
    }))
    exportCsv(
      exportData,
      [
        { key: 'ID', header: 'ID' },
        { key: 'When', header: 'When' },
        { key: 'EntityType', header: 'Entity Type' },
        { key: 'EntityID', header: 'Entity ID' },
        { key: 'Action', header: 'Action' },
        { key: 'PerformedBy', header: 'Performed By' },
      ],
      `admin-activity-${new Date().toISOString().slice(0, 10)}.csv`,
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2 flex-wrap">
        {(entityTypeFilter || actionFilter) && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setEntityTypeFilter('')
              setActionFilter('')
              setPage(1)
            }}
          >
            Clear filters
          </Button>
        )}
        <label className="flex items-center gap-1.5 text-xs text-surface-500 cursor-pointer select-none">
          <input
            type="checkbox"
            checked={polling}
            onChange={(e) => setPolling(e.target.checked)}
            className="rounded border-surface-300 text-brand-600 focus:ring-brand-500"
          />
          Auto-refresh
        </label>
        <Button variant="outline" size="sm" onClick={handleExportCsv}>
          <Download className="w-3.5 h-3.5" />
          CSV
        </Button>
        <Button variant="outline" size="sm" onClick={handleExportJson}>
          <svg
            xmlns="http://www.w3.org/2000/svg"
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
          >
            <polyline points="16 18 22 12 16 6" />
            <polyline points="8 6 2 12 8 18" />
          </svg>
          JSON
        </Button>
        <Button variant="outline" size="sm" onClick={() => refetch()}>
          <RefreshCw className="w-3.5 h-3.5" />
          Refresh
        </Button>
      </div>

      <TimeRangeFilter
        value={timeRange}
        onChange={handleTimeRangeWithReset}
        customFrom={customFrom}
        customTo={customTo}
        onCustomFromChange={handleCustomFromWithReset}
        onCustomToChange={handleCustomToWithReset}
      />

      <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
        <StatCard title="Created" value={isLoading ? '…' : stats.creates} icon={<Plus className="w-5 h-5" />} />
        <StatCard title="Updated" value={isLoading ? '…' : stats.updates} icon={<Activity className="w-5 h-5" />} />
        <StatCard title="Deleted" value={isLoading ? '…' : stats.deletes} icon={<Trash2 className="w-5 h-5" />} />
      </div>

      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-xs font-medium text-surface-400 mr-1">Entity:</span>
        {ENTITY_TYPES.map((t) => (
          <button
            key={t}
            onClick={() => {
              setEntityTypeFilter(t === entityTypeFilter ? '' : t)
              setPage(1)
            }}
            className={cn(
              'text-xs px-2.5 py-1 rounded-full border transition-colors',
              entityTypeFilter === t
                ? 'bg-brand-50 text-brand-700 border-brand-200'
                : 'bg-white text-surface-500 border-surface-200 hover:border-surface-300',
            )}
          >
            {t}
          </button>
        ))}
      </div>

      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-xs font-medium text-surface-400 mr-1">Action:</span>
        {['create', 'update', 'delete'].map((a) => (
          <button
            key={a}
            onClick={() => {
              setActionFilter(a === actionFilter ? '' : a)
              setPage(1)
            }}
            className={cn(
              'text-xs px-2.5 py-1 rounded-full border transition-colors',
              actionFilter === a
                ? 'bg-brand-50 text-brand-700 border-brand-200'
                : 'bg-white text-surface-500 border-surface-200 hover:border-surface-300',
            )}
          >
            {a}
          </button>
        ))}
      </div>

      {isLoading ? (
        <LogsTableSkeleton rows={5} />
      ) : entries.length === 0 ? (
        <LogsEmptyState
          title="No admin activity"
          description={
            entityTypeFilter || actionFilter
              ? 'Try a different filter.'
              : 'No API key, user, group, or grant changes have been recorded yet.'
          }
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <div className="divide-y divide-surface-100">
              <div className="grid grid-cols-12 gap-3 px-4 py-3 text-xs font-medium text-surface-500 uppercase tracking-wider bg-surface-50 rounded-t-xl">
                <span className="col-span-2">When</span>
                <span className="col-span-2">Action</span>
                <span className="col-span-2">Entity Type</span>
                <span className="col-span-3">Entity ID</span>
                <span className="col-span-3">Performed By</span>
              </div>
              {entries.map((entry) => (
                <button
                  key={entry.id}
                  onClick={() => setSelectedEntry(entry)}
                  className="w-full grid grid-cols-12 gap-3 px-4 py-3 text-sm items-center hover:bg-surface-50 transition-colors text-left"
                >
                  <span
                    className="col-span-2 font-mono text-xs text-surface-500"
                    title={new Date(entry.performed_at).toLocaleString()}
                  >
                    <TimeAgo date={entry.performed_at} />
                  </span>
                  <span className="col-span-2">
                    <span className={actionBadge(entry.action)}>{entry.action}</span>
                  </span>
                  <span className="col-span-2 text-sm text-surface-700">{entry.entity_type}</span>
                  <span className="col-span-3 font-mono text-xs text-surface-600 truncate">{entry.entity_id}</span>
                  <span className="col-span-3 font-mono text-xs text-surface-600 truncate">
                    {entry.performed_by || <span className="italic text-surface-400">system</span>}
                  </span>
                </button>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {totalPages > 1 && (
        <Pagination page={page} totalPages={totalPages} total={total} perPage={limit} onPageChange={setPage} />
      )}

      {selectedEntry && <DetailPanel entry={selectedEntry} onClose={() => setSelectedEntry(null)} />}
    </div>
  )
}

export function ConfigAuditView() {
  return (
    <QueryProvider>
      <ConfigAuditViewContent />
    </QueryProvider>
  )
}
