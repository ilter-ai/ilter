import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { api, type McpAuditEntry } from '../../lib/api'
import { formatMs } from '../../lib/format'
import { logger } from '../../lib/logger'
import { qk } from '../../lib/query'
import { cn } from '../../lib/utils'
import { DetailPanelShell } from '../logs/DetailPanelShell'
import { LogsEmptyState, LogsTableSkeleton } from '../logs/LogsTableState'
import { timeRangeToParams } from '../logs/logsTime'
import { DetailField, StatusBadge, TimeAgo } from '../logs/primitives'
import { TimeRangeFilter } from '../logs/TimeRangeFilter'
import { useLogsPageState } from '../logs/useLogsPageState'
import { Button } from '../ui/button'
import { Card, CardContent } from '../ui/card'
import { Activity, Clock, Download, RefreshCw, XCircle } from '../ui/icons'
import { Pagination } from '../ui/Pagination'
import { QueryProvider } from '../ui/query-provider'
import { StatCard } from '../ui/StatCard'
import { useExport } from '../ui/useExport'

// OpenAPI-bridge calls (openapi_search/describe/call) are logged into the
// same mcp_audit_log table as real MCP tool calls, under the server_id
// "openapi" sentinel — but they don't have a real MCP server or a varying
// HTTP method (every row is tools/call), so a Server/Method column would
// just repeat the same value on every row. The gateway instead writes the
// actual operation (e.g. "Petstore_getPetById") into the tool column, which
// is what actually distinguishes one call from another — so this view drops
// Server/Method and gives that column more room, calling it "Operation".

function DetailPanel({ entry, onClose }: { entry: McpAuditEntry; onClose: () => void }) {
  return (
    <DetailPanelShell title="OpenAPI Call Details" onClose={onClose}>
      <div className="grid grid-cols-2 gap-4">
        <DetailField label="Entry ID" value={entry.id} mono />
        <DetailField label="Created" value={new Date(entry.created_at).toLocaleString()} />
        <DetailField label="Operation" value={entry.tool} />
        <DetailField label="Status" value={<StatusBadge code={entry.status_code ?? (entry.success ? 200 : 500)} />} />
        <DetailField label="Duration" value={formatMs(entry.duration_ms)} mono />
        <DetailField label="User/Key" value={entry.key?.key_prefix || 'system'} mono />
        <DetailField label="Key Name" value={entry.key?.key_name || '—'} />
        {entry.api_key_id != null && <DetailField label="API Key ID" value={entry.api_key_id} mono />}
      </div>

      {entry.params && entry.params !== '{}' && (
        <div>
          <p className="text-xs font-medium text-surface-500 uppercase tracking-wider mb-1">Params</p>
          <pre className="rounded-lg bg-surface-50 p-3 text-xs font-mono text-surface-700 overflow-x-auto max-h-48 overflow-y-auto whitespace-pre-wrap">
            {(() => {
              try {
                return JSON.stringify(JSON.parse(entry.params!), null, 2)
              } catch {
                logger.error('OpenApiAuditView: failed to parse params JSON for entry', entry.id)
                return entry.params
              }
            })()}
          </pre>
        </div>
      )}

      {entry.error_msg && (
        <div>
          <p className="text-xs font-medium text-error uppercase tracking-wider mb-1">Error</p>
          <pre className="rounded-lg bg-red-50 border border-red-200 p-3 text-xs font-mono text-red-700 max-h-24 overflow-y-auto whitespace-pre-wrap">
            {entry.error_msg}
          </pre>
        </div>
      )}
    </DetailPanelShell>
  )
}

function OpenApiAuditViewContent() {
  const [page, setPage] = useState(1)
  const [operationFilter, setOperationFilter] = useState('')
  const [operationSearch, setOperationSearch] = useState('')
  const [selectedEntry, setSelectedEntry] = useState<McpAuditEntry | null>(null)
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

  const queryParams = useMemo(() => {
    const p: { limit?: number; offset?: number; tool?: string; from?: string; to?: string; source: 'openapi' } = {
      limit,
      offset: (page - 1) * limit,
      source: 'openapi',
    }
    if (operationFilter) p.tool = operationFilter
    if (from) p.from = from
    if (to) p.to = to
    return p
  }, [page, operationFilter, from, to])

  const {
    data: response,
    isLoading,
    refetch,
  } = useQuery({
    queryKey: [...qk.mcpAudit, 'openapi', page, operationFilter, timeRange, customFrom || '', customTo || ''],
    queryFn: () => api.mcp.getMcpAuditLog(queryParams),
    refetchInterval: polling ? 30000 : false,
  })

  const entries = response?.items ?? []
  const total = response?.total ?? 0
  const totalPages = Math.ceil(total / limit)

  const operations = useMemo(() => [...new Set(entries.map((e) => e.tool))].sort(), [entries])

  const stats = useMemo(() => {
    const totalCalls = entries.length
    const errorCalls = entries.filter((e) => !e.success).length
    const errorRate = totalCalls > 0 ? (errorCalls / totalCalls) * 100 : 0
    const avgDuration = totalCalls > 0 ? entries.reduce((sum, e) => sum + e.duration_ms, 0) / totalCalls : 0
    return { totalCalls, errorCalls, errorRate: errorRate.toFixed(1), avgDuration: Math.round(avgDuration) }
  }, [entries])

  const handleExportJson = () => {
    const exportData = entries.map((e) => ({
      id: e.id,
      timestamp: e.created_at,
      operation: e.tool,
      success: e.success,
      status_code: e.status_code,
      duration_ms: e.duration_ms,
      user: e.key?.key_prefix || 'system',
      key_name: e.key?.key_name || '',
      error: e.error_msg || '',
    }))
    exportJson(exportData, `openapi-audit-${new Date().toISOString().slice(0, 10)}.json`)
  }

  const handleExportCsv = () => {
    const exportData = entries.map((e) => ({
      ID: e.id,
      Timestamp: new Date(e.created_at).toLocaleString(),
      Operation: e.tool,
      Success: e.success ? 'Yes' : 'No',
      StatusCode: e.status_code ?? '',
      DurationMs: e.duration_ms,
      User: e.key?.key_prefix || 'system',
      KeyName: e.key?.key_name || '',
      Error: e.error_msg || '',
    }))
    exportCsv(exportData, [
      { key: 'ID', header: 'ID' },
      { key: 'Timestamp', header: 'Timestamp' },
      { key: 'Operation', header: 'Operation' },
      { key: 'Success', header: 'Success' },
      { key: 'StatusCode', header: 'Status Code' },
      { key: 'DurationMs', header: 'Duration (ms)' },
      { key: 'User', header: 'User' },
      { key: 'KeyName', header: 'Key Name' },
      { key: 'Error', header: 'Error' },
    ])
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2 flex-wrap">
        {operationFilter && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setOperationFilter('')
              setPage(0)
            }}
          >
            Clear filter
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
        <Button variant="outline" size="sm" onClick={() => handleExportCsv()}>
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

      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard
          title="Total Calls"
          value={isLoading ? '...' : stats.totalCalls}
          description={total > 0 ? `${total} total in range` : undefined}
          icon={<Activity className="w-5 h-5" />}
        />
        <StatCard
          title="Errors"
          value={isLoading ? '...' : stats.errorCalls}
          description={stats.totalCalls > 0 ? `${stats.errorRate}% error rate` : 'No data'}
          icon={<XCircle className="w-5 h-5" />}
        />
        <StatCard
          title="Avg Duration"
          value={isLoading ? '...' : formatMs(stats.avgDuration)}
          description="Per call"
          icon={<Clock className="w-5 h-5" />}
        />
        <StatCard
          title="Operations Used"
          value={isLoading ? '...' : operations.length}
          description="Unique operations in range"
          icon={
            <svg
              xmlns="http://www.w3.org/2000/svg"
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z" />
            </svg>
          }
        />
      </div>

      {operations.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative">
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
              className="absolute left-2.5 top-1/2 -translate-y-1/2 text-surface-400"
            >
              <circle cx="11" cy="11" r="8" />
              <path d="m21 21-4.3-4.3" />
            </svg>
            <input
              type="text"
              value={operationSearch}
              onChange={(e) => setOperationSearch(e.target.value)}
              placeholder="Filter operations..."
              className="w-44 rounded-lg border border-surface-200 bg-white pl-8 pr-3 py-1.5 text-xs text-surface-700 placeholder:text-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
            />
          </div>
          {operations
            .filter((op) => !operationSearch || op.toLowerCase().includes(operationSearch.toLowerCase()))
            .map((op) => (
              <button
                key={op}
                onClick={() => {
                  setOperationFilter(op === operationFilter ? '' : op)
                  setPage(0)
                }}
                className={cn(
                  'text-xs px-2.5 py-1 rounded-full border transition-colors inline-flex items-center gap-1 max-w-[220px]',
                  operationFilter === op
                    ? 'bg-brand-50 text-brand-700 border-brand-200'
                    : 'bg-white text-surface-500 border-surface-200 hover:border-surface-300',
                )}
              >
                <span className="truncate">{op}</span>
                {operationFilter === op && (
                  <svg
                    xmlns="http://www.w3.org/2000/svg"
                    width="12"
                    height="12"
                    viewBox="0 0 24 24"
                    fill="none"
                    stroke="currentColor"
                    strokeWidth="2.5"
                    strokeLinecap="round"
                    strokeLinejoin="round"
                    className="shrink-0"
                  >
                    <line x1="18" y1="6" x2="6" y2="18" />
                    <line x1="6" y1="6" x2="18" y2="18" />
                  </svg>
                )}
              </button>
            ))}
        </div>
      )}

      {isLoading ? (
        <LogsTableSkeleton rows={5} />
      ) : entries.length === 0 ? (
        <LogsEmptyState
          title="No audit entries"
          description={operationFilter ? 'Try a different filter.' : 'No OpenAPI bridge calls have been recorded yet.'}
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <div className="divide-y divide-surface-100">
              <div className="grid grid-cols-12 gap-3 px-4 py-3 text-xs font-medium text-surface-500 uppercase tracking-wider bg-surface-50 rounded-t-xl">
                <span className="col-span-2">Time</span>
                <span className="col-span-1">Status</span>
                <span className="col-span-5">Operation</span>
                <span className="col-span-3">User/Key</span>
                <span className="col-span-1">Duration</span>
              </div>
              {entries.map((entry) => (
                <button
                  key={entry.id}
                  onClick={() => setSelectedEntry(entry)}
                  className={cn(
                    'w-full grid grid-cols-12 gap-3 px-4 py-3 text-sm items-center hover:bg-surface-50 transition-colors text-left',
                    !entry.success && 'bg-red-50/30 hover:bg-red-50/60',
                  )}
                >
                  <span
                    className="col-span-2 font-mono text-xs text-surface-500"
                    title={new Date(entry.created_at).toLocaleString()}
                  >
                    <TimeAgo date={entry.created_at} />
                  </span>
                  <span className="col-span-1">
                    <StatusBadge code={entry.status_code ?? (entry.success ? 200 : 500)} />
                  </span>
                  <span className="col-span-5 font-mono text-sm text-brand-600 truncate">{entry.tool}</span>
                  <span className="col-span-3 text-sm text-surface-700 flex items-center gap-1.5 min-w-0">
                    {entry.key?.key_prefix ? (
                      <>
                        <span className="font-mono truncate" title={entry.key.key_prefix}>
                          {entry.key.key_prefix}
                        </span>
                        {entry.key.key_name && (
                          <span className="shrink-0 text-[10px] font-medium px-1.5 py-0.5 rounded-full bg-brand-50 text-brand-700 border border-brand-200">
                            {entry.key.key_name}
                          </span>
                        )}
                      </>
                    ) : (
                      <span className="text-surface-400 italic">system</span>
                    )}
                  </span>
                  <span className="col-span-1 font-mono text-xs text-surface-600">{formatMs(entry.duration_ms)}</span>
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

export function OpenApiAuditView() {
  return (
    <QueryProvider>
      <OpenApiAuditViewContent />
    </QueryProvider>
  )
}
