import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { api, type RequestDetail, type RequestSummary } from '../../lib/api'
import { formatMs } from '../../lib/format'
import { qk } from '../../lib/query'
import { cn } from '../../lib/utils'
import { DetailPanelShell } from '../logs/DetailPanelShell'
import { LogsDetailLoading } from '../logs/LogsTableState'
import { timeRangeToParams } from '../logs/logsTime'
import { DetailField, StatusBadge, TimeAgo, TraceBadge, TraceLink } from '../logs/primitives'
import { TimeRangeFilter } from '../logs/TimeRangeFilter'
import { useLogsPageState } from '../logs/useLogsPageState'
import { Button } from '../ui/button'
import { Card, CardContent } from '../ui/card'
import { type Column, DataTable } from '../ui/DataTable'
import { FilterBar } from '../ui/FilterBar'
import { Activity, AlertTriangle, DollarSign, Download, RefreshCw } from '../ui/icons'
import { Pagination } from '../ui/Pagination'
import { QueryState } from '../ui/QueryState'
import { QueryProvider } from '../ui/query-provider'
import { StatCard } from '../ui/StatCard'
import { useExport } from '../ui/useExport'

const columns: Column<RequestSummary>[] = [
  {
    key: 'timestamp',
    header: 'Time',
    render: (r) => (
      <span className="font-mono text-xs text-surface-500" title={new Date(r.timestamp).toLocaleString()}>
        <TimeAgo date={r.timestamp} />
      </span>
    ),
  },
  {
    key: 'status_code',
    header: 'Status',
    render: (r) => <StatusBadge code={r.status_code} />,
  },
  {
    key: 'model',
    header: 'Model',
    render: (r) => <span className="text-sm text-surface-700 max-w-[120px] truncate block">{r.model}</span>,
  },
  {
    key: 'provider',
    header: 'Provider',
    render: (r) => <span className="text-sm text-surface-600">{r.provider}</span>,
  },
  {
    key: 'tokens',
    header: 'Tokens',
    render: (r) => (
      <span className="font-mono text-xs text-surface-600">
        {r.prompt_tokens.toLocaleString()} / {r.completion_tokens.toLocaleString()}
      </span>
    ),
  },
  {
    key: 'total_cost',
    header: 'Cost',
    render: (r) => <span className="font-mono text-xs text-surface-700">${r.total_cost.toFixed(6)}</span>,
  },
  {
    key: 'latency_ms',
    header: 'Latency',
    render: (r) => <span className="font-mono text-xs text-surface-600">{formatMs(r.latency_ms)}</span>,
  },
  {
    key: 'client_ip',
    header: 'IP',
    render: (r) => <span className="font-mono text-xs text-surface-500">{r.client_ip || '—'}</span>,
  },
  {
    key: 'trace_id',
    header: '',
    render: (r) => (r.trace_id ? <TraceBadge traceId={r.trace_id} /> : null),
    className: 'w-16',
  },
]

function EmptyStateWithCurl() {
  return (
    <div className="flex flex-col items-center justify-center py-16">
      <Activity size={48} strokeWidth={1.5} className="text-surface-300 mb-4" />
      <p className="text-sm font-medium text-surface-500 mb-2">No requests yet</p>
      <p className="text-xs text-surface-400 max-w-md text-center mb-6">
        Route a request through ilter to see it appear here.
      </p>
      <div className="w-full max-w-lg rounded-lg bg-surface-900 p-4 overflow-x-auto">
        <pre className="text-xs text-green-400 font-mono leading-relaxed">
          {`curl http://localhost:8181/v1/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "x-api-key: ilter-your-key" \\
  -d '{"model":"gpt-4o","messages":[{"role":"user","content":"Hello"}]}'`}
        </pre>
      </div>
    </div>
  )
}

function DetailPanel({ requestId, onClose }: { requestId: number; onClose: () => void }) {
  const {
    data: detail,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: qk.requestsDetail(requestId),
    queryFn: () => api.requests.getRequestDetail(requestId),
    enabled: requestId > 0,
  })

  return (
    <DetailPanelShell title="Request Details" onClose={onClose}>
      {isLoading ? (
        <LogsDetailLoading />
      ) : error ? (
        <QueryState.Error title="Failed to load details" message={error.message} onRetry={() => refetch()} />
      ) : detail ? (
        <>
          <div className="grid grid-cols-2 gap-4">
            <DetailField label="ID" value={detail.id} mono />
            <DetailField label="Timestamp" value={new Date(detail.timestamp).toLocaleString()} />
            <DetailField label="Model" value={detail.model} />
            <DetailField label="Provider" value={detail.provider} />
            <DetailField label="Status" value={<StatusBadge code={detail.status_code} />} />
            <DetailField label="Cost" value={`$${detail.total_cost.toFixed(6)}`} mono />
            <DetailField label="Tokens In" value={detail.prompt_tokens.toLocaleString()} mono />
            <DetailField label="Tokens Out" value={detail.completion_tokens.toLocaleString()} mono />
            <DetailField label="Latency" value={formatMs(detail.latency_ms)} mono />
            <DetailField label="IP Address" value={detail.client_ip} mono />
            <DetailField label="Cache Hit" value={detail.cache_hit ? 'Yes' : 'No'} />
            {detail.trace_id && <DetailField label="Trace ID" value={<TraceLink traceId={detail.trace_id} />} mono />}
          </div>

          {(detail.phase_latencies.guardrail_latency_ms > 0 ||
            detail.phase_latencies.llm_latency_ms > 0 ||
            detail.phase_latencies.queued_latency_ms > 0) && (
            <div>
              <h4 className="text-sm font-medium text-surface-700 mb-2">Phase Latencies</h4>
              <div className="flex gap-4">
                {detail.phase_latencies.guardrail_latency_ms > 0 && (
                  <div className="rounded-lg bg-surface-50 px-3 py-2">
                    <p className="text-xs text-surface-500">Guardrail</p>
                    <p className="font-mono text-sm text-surface-800">
                      {formatMs(detail.phase_latencies.guardrail_latency_ms)}
                    </p>
                  </div>
                )}
                {detail.phase_latencies.llm_latency_ms > 0 && (
                  <div className="rounded-lg bg-surface-50 px-3 py-2">
                    <p className="text-xs text-surface-500">LLM</p>
                    <p className="font-mono text-sm text-surface-800">
                      {formatMs(detail.phase_latencies.llm_latency_ms)}
                    </p>
                  </div>
                )}
                {detail.phase_latencies.queued_latency_ms > 0 && (
                  <div className="rounded-lg bg-surface-50 px-3 py-2">
                    <p className="text-xs text-surface-500">Queued</p>
                    <p className="font-mono text-sm text-surface-800">
                      {formatMs(detail.phase_latencies.queued_latency_ms)}
                    </p>
                  </div>
                )}
              </div>
            </div>
          )}

          {(detail.request_body || detail.response_body) && <RequestResponseTabs detail={detail} />}
        </>
      ) : null}
    </DetailPanelShell>
  )
}

function formatJsonBody(raw: string | null | undefined): string {
  if (raw == null) return ''
  try {
    return JSON.stringify(JSON.parse(raw), null, 2)
  } catch {
    return raw
  }
}

function RequestResponseTabs({ detail }: { detail: RequestDetail }) {
  const [tab, setTab] = useState<'request' | 'response'>('request')
  const hasRequest = detail.request_body !== null && detail.request_body !== undefined
  const hasResponse = detail.response_body !== null && detail.response_body !== undefined

  return (
    <div>
      <div className="flex gap-1 border-b border-surface-200 mb-3">
        <button
          onClick={() => setTab('request')}
          className={cn(
            'px-3 py-1.5 text-xs font-medium border-b-2 transition-colors',
            tab === 'request'
              ? 'border-brand-600 text-brand-700'
              : 'border-transparent text-surface-500 hover:text-surface-700',
          )}
        >
          Request Body
        </button>
        {hasResponse && (
          <button
            onClick={() => setTab('response')}
            className={cn(
              'px-3 py-1.5 text-xs font-medium border-b-2 transition-colors',
              tab === 'response'
                ? 'border-brand-600 text-brand-700'
                : 'border-transparent text-surface-500 hover:text-surface-700',
            )}
          >
            Response Body
          </button>
        )}
      </div>
      <pre className="rounded-lg bg-surface-50 p-3 text-xs font-mono text-surface-700 overflow-x-auto max-h-64 overflow-y-auto whitespace-pre-wrap">
        {tab === 'request'
          ? hasRequest
            ? formatJsonBody(detail.request_body)
            : 'No request body captured.'
          : hasResponse
            ? formatJsonBody(detail.response_body)
            : 'No response body captured.'}
      </pre>
    </div>
  )
}

function RequestsViewContent() {
  const [page, setPage] = useState(1)
  const [statusFilter, setStatusFilter] = useState('')
  const [providerFilter, setProviderFilter] = useState('')
  const [modelSearch, setModelSearch] = useState('')
  const [selectedId, setSelectedId] = useState<number | null>(null)
  const perPage = 15

  const {
    timeRange,
    customFrom,
    customTo,
    polling,
    setPolling,
    handleTimeRange,
    handleCustomFrom,
    handleCustomTo,
    clearTimeRange,
  } = useLogsPageState()

  const timeParams = useMemo(
    () => timeRangeToParams(timeRange, customFrom, customTo),
    [timeRange, customFrom, customTo],
  )

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

  const {
    data: overview,
    isLoading: overviewLoading,
    error: overviewError,
    refetch: refetchOverview,
  } = useQuery({
    queryKey: qk.requestsOverview,
    queryFn: api.requests.getAnalyticsOverview,
    retry: 1,
  })

  const queryParams = {
    page,
    limit: perPage,
    ...(statusFilter ? { status: statusFilter } : {}),
    ...(modelSearch ? { model: modelSearch } : {}),
    ...(providerFilter ? { provider: providerFilter } : {}),
    ...(timeParams.from ? { start: timeParams.from } : {}),
    ...(timeParams.to ? { end: timeParams.to } : {}),
  }

  const {
    data: listData,
    isLoading: listLoading,
    error: listError,
    refetch: refetchList,
  } = useQuery({
    queryKey: qk.requestsList(queryParams as Record<string, unknown>),
    queryFn: () => api.requests.getRequests(queryParams),
    placeholderData: (prev) => prev,
    retry: 1,
    refetchInterval: polling ? 30000 : false,
  })

  const { data: allProviders = [] } = useQuery({
    queryKey: ['requests', 'providers'],
    queryFn: () => api.providers.getProviders().catch(() => []),
  })

  const items = listData?.items ?? []
  const total = listData?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / perPage))

  const providers = useMemo(() => [...new Set(allProviders.map((p) => p.name))].sort(), [allProviders])

  const handleExportJson = () => {
    const exportData = items.map((r) => ({
      id: r.id,
      timestamp: r.timestamp,
      status_code: r.status_code,
      model: r.model,
      provider: r.provider,
      prompt_tokens: r.prompt_tokens,
      completion_tokens: r.completion_tokens,
      total_cost: r.total_cost,
      latency_ms: r.latency_ms,
      client_ip: r.client_ip,
      trace_id: r.trace_id,
    }))
    exportJson(exportData, `requests-${new Date().toISOString().slice(0, 10)}.json`)
  }

  const handleExportCsv = () => {
    const exportData = items.map((r) => ({
      ID: r.id,
      Timestamp: new Date(r.timestamp).toLocaleString(),
      Status: r.status_code,
      Model: r.model,
      Provider: r.provider,
      PromptTokens: r.prompt_tokens,
      CompletionTokens: r.completion_tokens,
      Cost: r.total_cost.toFixed(6),
      LatencyMs: r.latency_ms,
      IP: r.client_ip || '',
      TraceID: r.trace_id || '',
    }))
    exportCsv(
      exportData,
      [
        { key: 'ID', header: 'ID' },
        { key: 'Timestamp', header: 'Timestamp' },
        { key: 'Status', header: 'Status' },
        { key: 'Model', header: 'Model' },
        { key: 'Provider', header: 'Provider' },
        { key: 'PromptTokens', header: 'Prompt Tokens' },
        { key: 'CompletionTokens', header: 'Completion Tokens' },
        { key: 'Cost', header: 'Cost' },
        { key: 'LatencyMs', header: 'Latency (ms)' },
        { key: 'IP', header: 'IP' },
        { key: 'TraceID', header: 'Trace ID' },
      ],
      `requests-${new Date().toISOString().slice(0, 10)}.csv`,
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-2 flex-wrap">
        {(statusFilter || providerFilter || modelSearch || timeRange !== '24h' || customFrom) && (
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setStatusFilter('')
              setProviderFilter('')
              setModelSearch('')
              clearTimeRange()
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
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            refetchList()
            refetchOverview()
          }}
        >
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

      {overviewError && !overviewLoading ? (
        <QueryState.Error
          title="Failed to load overview"
          message={overviewError.message}
          onRetry={() => refetchOverview()}
        />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
          <StatCard
            title="Total Requests"
            value={overviewLoading ? '…' : (overview?.total_requests ?? 0).toLocaleString()}
            icon={<Activity className="h-5 w-5" />}
          />
          <StatCard
            title="Error Rate"
            value={overviewLoading ? '…' : `${(overview?.error_rate ?? 0).toFixed(2)}%`}
            icon={<AlertTriangle className="h-5 w-5" />}
          />
          <StatCard
            title="Cost"
            value={overviewLoading ? '…' : `$${(overview?.cost ?? 0).toFixed(6)}`}
            icon={<DollarSign className="h-5 w-5" />}
          />
          <StatCard
            title="Cache Hit Rate"
            value={overviewLoading ? '…' : `${((overview?.cache_hit_rate ?? 0) * 100).toFixed(1)}%`}
            icon={<RefreshCw className="h-5 w-5" />}
          />
        </div>
      )}

      <FilterBar
        searchPlaceholder="Search by model, provider, IP..."
        searchValue={modelSearch}
        onSearchChange={(v) => {
          setModelSearch(v)
          setPage(1)
        }}
      />

      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-xs font-medium text-surface-400 mr-1">Status:</span>
        {[
          { label: 'All', value: '' },
          { label: 'Success', value: 'success' },
          { label: 'Error', value: 'error' },
        ].map((s) => (
          <button
            key={s.value}
            onClick={() => {
              setStatusFilter(s.value)
              setPage(1)
            }}
            className={cn(
              'text-xs px-2.5 py-1 rounded-full border transition-colors',
              statusFilter === s.value
                ? 'bg-brand-50 text-brand-700 border-brand-200'
                : 'bg-white text-surface-500 border-surface-200 hover:border-surface-300',
            )}
          >
            {s.label}
            {(s.value === 'success' || s.value === 'error') && (
              <span
                className={cn(
                  'ml-1.5 w-1.5 h-1.5 rounded-full inline-block',
                  s.value === 'success' ? 'bg-success' : 'bg-error',
                )}
              />
            )}
          </button>
        ))}
      </div>

      {providers.length > 0 && (
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-xs font-medium text-surface-400 mr-1">Provider:</span>
          {providers.map((p) => (
            <button
              key={p}
              onClick={() => {
                setProviderFilter(p === providerFilter ? '' : p)
                setPage(1)
              }}
              className={cn(
                'text-xs px-2.5 py-1 rounded-full border transition-colors inline-flex items-center gap-1',
                providerFilter === p
                  ? 'bg-brand-50 text-brand-700 border-brand-200'
                  : 'bg-white text-surface-500 border-surface-200 hover:border-surface-300',
              )}
            >
              {p}
              {providerFilter === p && (
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

      {listError ? (
        <Card>
          <CardContent>
            <QueryState.Error
              title="Failed to load requests"
              message={listError.message}
              onRetry={() => refetchList()}
            />
          </CardContent>
        </Card>
      ) : items.length === 0 && !listLoading ? (
        <Card>
          <CardContent>
            <EmptyStateWithCurl />
          </CardContent>
        </Card>
      ) : (
        <>
          <Card>
            <CardContent className="p-0">
              <DataTable
                columns={columns}
                data={listLoading ? [] : items}
                keyExtractor={(r) => String(r.id)}
                onRowClick={(r) => setSelectedId(r.id)}
                emptyMessage="No requests match the current filters."
                loading={listLoading}
              />
            </CardContent>
          </Card>
          <Pagination page={page} totalPages={totalPages} total={total} perPage={perPage} onPageChange={setPage} />
        </>
      )}

      {selectedId !== null && <DetailPanel requestId={selectedId} onClose={() => setSelectedId(null)} />}
    </div>
  )
}

export function RequestsView() {
  return (
    <QueryProvider>
      <RequestsViewContent />
    </QueryProvider>
  )
}
