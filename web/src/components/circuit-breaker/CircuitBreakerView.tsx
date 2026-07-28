import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../../lib/api'
import { qk } from '../../lib/query'
import { CHART_COLORS } from '../../lib/recharts-theme'
import { useApiMutation } from '../../lib/useApiMutation'
import LazyChart from '../charts/LazyChart'
import { FeatureStatus } from '../settings/FeatureStatus'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { DataTable } from '../ui/DataTable'
import { AlertCircle, RefreshCw, ShieldCheck, Unlock, ZapOff } from '../ui/icons'
import { QueryProvider } from '../ui/query-provider'
import { StatCard } from '../ui/StatCard'

const circuitStates = ['closed', 'open', 'half-open', 'idle'] as const
type CircuitState = (typeof circuitStates)[number]

function parseCircuitState(val: string): CircuitState {
  if (circuitStates.includes(val as CircuitState)) {
    return val as CircuitState
  }
  return 'idle'
}

interface ChartPoint {
  name: string
  failures: number
}

function CircuitBadge({ state }: { state: CircuitState }) {
  const config: Record<CircuitState, { color: string; bg: string; label: string }> = {
    closed: { color: 'text-success border-success/20', bg: 'bg-success/10', label: 'Closed' },
    'half-open': { color: 'text-warning border-warning/20', bg: 'bg-warning/10', label: 'Half-Open' },
    open: { color: 'text-error border-error/20', bg: 'bg-error/10', label: 'Open' },
    idle: { color: 'text-surface-400 border-surface-200', bg: 'bg-surface-50', label: 'Idle' },
  }
  const s = config[state]
  return (
    <span
      className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-0.5 text-xs font-medium ${s.color} ${s.bg}`}
    >
      <span
        className={`inline-block h-1.5 w-1.5 rounded-full ${
          state === 'closed'
            ? 'bg-success'
            : state === 'half-open'
              ? 'bg-warning'
              : state === 'open'
                ? 'bg-error'
                : 'bg-surface-300'
        }`}
      />
      {s.label}
    </span>
  )
}

function CircuitBreakerViewContent() {
  const { data, isLoading, error } = useQuery({
    queryKey: qk.circuitBreaker,
    queryFn: api.circuitBreaker.getCircuitBreakerSummary,
  })

  const [confirmAction, setConfirmAction] = useState<'reset' | 'force-open' | null>(null)
  const [reason, setReason] = useState('')
  const [reasonError, setReasonError] = useState(false)

  const toggleMutation = useApiMutation((r?: string) => api.circuitBreaker.toggleCircuitBreaker(!summary.enabled, r), {
    invalidate: [qk.circuitBreaker],
  })
  const resetMutation = useApiMutation((r?: string) => api.circuitBreaker.resetAllCircuits(r), {
    invalidate: [qk.circuitBreaker],
  })
  const forceOpenMutation = useApiMutation((r?: string) => api.circuitBreaker.forceOpenAllCircuits(r), {
    invalidate: [qk.circuitBreaker],
  })

  function handleCancel() {
    setConfirmAction(null)
    setReason('')
    setReasonError(false)
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="text-surface-500 text-sm">Loading circuit breaker data...</div>
      </div>
    )
  }

  if (error || !data) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="text-error text-sm">{error?.message || 'No data available'}</div>
      </div>
    )
  }

  const { summary, circuits } = data

  const failureChartData: ChartPoint[] = circuits.map((c) => ({
    name: c.provider,
    failures: c.failures,
  }))

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-surface-900">Circuit Breaker</h2>
        <div className="flex items-center gap-2">
          <FeatureStatus type="toggle" enabled={summary.enabled} onToggle={() => toggleMutation.mutate(undefined)} />
        </div>
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard
          title="Closed Circuits"
          value={summary.closed_count}
          description={`out of ${summary.total_circuits} providers`}
          icon={<ShieldCheck size={20} />}
        />
        <StatCard
          title="Open Circuits"
          value={summary.open_count}
          description="requests being blocked"
          icon={<ZapOff size={20} />}
        />
        <StatCard
          title="Half-Open"
          value={summary.half_open_count}
          description="testing recovery"
          icon={<Unlock size={20} />}
        />
        <StatCard
          title="Total Failures (24h)"
          value={summary.total_failures_24h.toLocaleString()}
          icon={<AlertCircle size={20} />}
        />
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <Card className="lg:col-span-2">
          <CardHeader>
            <CardTitle className="text-base">Provider Failures (24h)</CardTitle>
          </CardHeader>
          <CardContent className="p-4">
            <LazyChart
              type="bar"
              data={failureChartData as unknown as Record<string, unknown>[]}
              xKey="name"
              height={250}
              series={[{ dataKey: 'failures', color: CHART_COLORS.error, name: 'Failures' }]}
            />
          </CardContent>
        </Card>

        <div className="space-y-4">
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Circuit States</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="inline-block h-2.5 w-2.5 rounded-full bg-success" />
                    <span className="text-sm text-surface-600">Closed</span>
                  </div>
                  <span className="text-sm font-semibold text-surface-900">{summary.closed_count}</span>
                </div>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="inline-block h-2.5 w-2.5 rounded-full bg-warning" />
                    <span className="text-sm text-surface-600">Half-Open</span>
                  </div>
                  <span className="text-sm font-semibold text-surface-900">{summary.half_open_count}</span>
                </div>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="inline-block h-2.5 w-2.5 rounded-full bg-error" />
                    <span className="text-sm text-surface-600">Open</span>
                  </div>
                  <span className="text-sm font-semibold text-surface-900">{summary.open_count}</span>
                </div>
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2">
                    <span className="inline-block h-2.5 w-2.5 rounded-full bg-surface-300" />
                    <span className="text-sm text-surface-600">Idle (unused)</span>
                  </div>
                  <span className="text-sm font-semibold text-surface-900">
                    {summary.total_circuits - summary.closed_count - summary.open_count - summary.half_open_count}
                  </span>
                </div>
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm">Quick Actions</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-2">
                <Button
                  variant="outline"
                  size="sm"
                  className="w-full justify-center"
                  onClick={() => setConfirmAction('reset')}
                  disabled={resetMutation.isPending}
                >
                  <RefreshCw size={16} />
                  Reset All Circuits
                </Button>
                <Button
                  variant="destructive"
                  size="sm"
                  className="w-full justify-center"
                  onClick={() => setConfirmAction('force-open')}
                  disabled={forceOpenMutation.isPending}
                >
                  <ZapOff size={20} />
                  Force Open All
                </Button>
              </div>

              {confirmAction && (
                <div className="mt-3 rounded-lg border border-surface-200 bg-surface-50 p-3 space-y-3">
                  <p className="text-sm font-medium text-surface-900">
                    {confirmAction === 'reset'
                      ? 'Reset All Circuits — Are you sure?'
                      : 'Force Open All Circuits — Are you sure?'}
                  </p>
                  <p className="text-xs text-surface-500">
                    {confirmAction === 'reset'
                      ? 'This will clear failure counts and restore normal operation.'
                      : 'This will block all requests to every provider.'}
                  </p>
                  <div>
                    <label htmlFor="cb-reason" className="text-xs font-medium text-surface-700">
                      Reason for this action:
                    </label>
                    <textarea
                      id="cb-reason"
                      rows={2}
                      value={reason}
                      onChange={(e) => {
                        setReason(e.target.value)
                        if (reasonError) setReasonError(false)
                      }}
                      placeholder="e.g. Provider recovered, clearing circuit..."
                      className={`mt-1 w-full rounded-md border bg-white px-3 py-1.5 text-sm text-surface-900 placeholder:text-surface-400 focus:outline-none focus:ring-2 focus:ring-primary/30 ${
                        reasonError ? 'border-error' : 'border-surface-200'
                      }`}
                    />
                    {reasonError && <p className="text-xs text-error mt-1">Reason is required before confirming.</p>}
                  </div>
                  <div className="flex gap-2">
                    <Button variant="outline" size="sm" className="flex-1" onClick={handleCancel}>
                      Cancel
                    </Button>
                    <Button
                      variant={confirmAction === 'reset' ? 'default' : 'destructive'}
                      size="sm"
                      className="flex-1"
                      onClick={() => {
                        if (!reason.trim()) {
                          setReasonError(true)
                          return
                        }
                        const r = reason
                        if (confirmAction === 'reset') resetMutation.mutate(r)
                        else if (confirmAction === 'force-open') forceOpenMutation.mutate(r)
                        setConfirmAction(null)
                        setReason('')
                        setReasonError(false)
                      }}
                      disabled={resetMutation.isPending || forceOpenMutation.isPending}
                    >
                      Confirm
                    </Button>
                  </div>
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Provider Circuit States</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <DataTable
            columns={[
              {
                key: 'provider',
                header: 'Provider',
                render: (c) => <span className="font-medium text-surface-900">{c.provider}</span>,
              },
              {
                key: 'state',
                header: 'State',
                render: (c) => <CircuitBadge state={parseCircuitState(c.state)} />,
              },
              {
                key: 'failures',
                header: 'Failures',
                render: (c) => {
                  const total = c.successes + c.failures
                  return (
                    <div className="flex items-center gap-2">
                      <span className="font-mono text-xs text-surface-700">{c.failures}</span>
                      <div className="w-16 h-1.5 rounded-full bg-surface-100 overflow-hidden">
                        <div
                          className={`h-full rounded-full ${c.consecutive_failures > 0 ? 'bg-error' : 'bg-success'}`}
                          style={{ width: `${Math.min((c.failures / Math.max(total, 1)) * 100, 100)}%` }}
                        />
                      </div>
                    </div>
                  )
                },
              },
              {
                key: 'successRate',
                header: 'Success Rate',
                render: (c) => {
                  const total = c.successes + c.failures
                  if (total === 0) {
                    return <span className="font-mono text-xs text-surface-400">—</span>
                  }
                  const rate = Math.round((c.successes / total) * 1000) / 10
                  return (
                    <span
                      className={`font-mono text-xs font-medium ${rate >= 99 ? 'text-success' : rate >= 95 ? 'text-warning' : 'text-error'}`}
                    >
                      {rate}%
                    </span>
                  )
                },
              },
              {
                key: 'requests',
                header: 'Requests',
                render: (c) => <span className="font-mono text-xs text-surface-600">{c.successes + c.failures}</span>,
              },
              {
                key: 'consecutive_failures',
                header: 'Consecutive Failures',
                render: (c) => <span className="font-mono text-xs text-surface-600">{c.consecutive_failures}</span>,
              },
            ]}
            data={circuits}
            keyExtractor={(c) => c.provider}
          />
        </CardContent>
      </Card>
    </div>
  )
}

export function CircuitBreakerView() {
  return (
    <QueryProvider>
      <CircuitBreakerViewContent />
    </QueryProvider>
  )
}
