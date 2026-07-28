import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { TimeAgo } from '@/components/logs/primitives'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { ChevronDown, ChevronRight, Loader2 } from '@/components/ui/icons'
import { QueryState } from '@/components/ui/QueryState'
import { Skeleton } from '@/components/ui/skeleton'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { api } from '@/lib/api'
import type { JobRun } from '@/lib/api/types'
import { formatMs } from '@/lib/format'
import { qk } from '@/lib/query'
import { cn } from '@/lib/utils'

interface ExecutionHistoryProps {
  jobId: string
}

const statusColors: Record<string, string> = {
  pending: 'bg-sky-100 text-sky-800',
  retrying: 'bg-amber-100 text-amber-800 border border-amber-300',
  running: 'bg-yellow-100 text-yellow-800',
  llm_success: 'bg-blue-100 text-blue-800',
  llm_failed: 'bg-red-100 text-red-800',
  delivery_failed: 'bg-orange-100 text-orange-800',
  success: 'bg-green-100 text-green-800',
  budget_exceeded: 'bg-gray-100 text-gray-800',
  dead_letter: 'bg-red-100 text-red-800',
  failed: 'bg-red-100 text-red-800',
  skipped_locked: 'bg-gray-100 text-gray-600',
}

function StatusBadge({ status }: { status: string }) {
  return (
    <span
      className={cn(
        'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium',
        statusColors[status] || 'bg-surface-100 text-surface-600',
        (status === 'running' || status === 'pending' || status === 'retrying') && 'animate-pulse',
      )}
    >
      {status.replace(/_/g, ' ')}
    </span>
  )
}

function TruncatedText({ text, limit = 500 }: { text: string; limit?: number }) {
  const [expanded, setExpanded] = useState(false)
  const needsTruncation = text.length > limit
  return (
    <div>
      <p className="font-mono text-xs text-surface-700 whitespace-pre-wrap break-all">
        {expanded || !needsTruncation ? text : `${text.slice(0, limit)}…`}
      </p>
      {needsTruncation && (
        <Button variant="link" size="sm" className="h-auto p-0 text-xs mt-1" onClick={() => setExpanded(!expanded)}>
          {expanded ? 'Show less' : 'Show full result'}
        </Button>
      )}
    </div>
  )
}

function StepList({ steps, live }: { steps: NonNullable<JobRun['steps']>; live: boolean }) {
  return (
    <div className="space-y-2">
      <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">
        {live ? 'Step Progress' : `Step Results (${steps.length})`}
      </p>
      {steps.map((step) => {
        const total = steps.length
        const name =
          step.type === 'llm' ? (step.model ?? step.type.toUpperCase()) : (step.tool ?? step.type.toUpperCase())
        let icon = '⏳'
        let label = 'Pending'
        if (step.status === 'done') {
          icon = '✅'
          label = 'Done'
        } else if (step.status === 'running') {
          icon = '🔄'
          label = 'Running'
        } else if (step.status === 'failed') {
          icon = '❌'
          label = 'Failed'
        }
        const stats =
          step.status === 'done'
            ? [
                step.tokens_in != null &&
                  step.tokens_out != null &&
                  `${(step.tokens_in + step.tokens_out).toLocaleString()} tokens`,
                step.cost != null && `$${step.cost.toFixed(4)}`,
              ]
                .filter(Boolean)
                .join(' · ')
            : ''
        return (
          <div key={step.index} className="rounded border border-surface-200 bg-white p-2 text-xs">
            <div className="flex items-center gap-2">
              <span className={live && step.status === 'running' ? 'animate-spin' : ''}>{icon}</span>
              <span className="text-surface-600">
                Step {step.index + 1}/{total}
              </span>
              <span className="text-surface-400">—</span>
              <span className="uppercase font-medium text-surface-500">{step.type}</span>
              <span className="text-surface-400">—</span>
              <span className="font-mono text-surface-700 truncate">{name}</span>
              <span className="text-surface-400">—</span>
              <span
                className={cn(
                  step.status === 'done' && 'text-green-700',
                  step.status === 'running' && 'text-amber-700',
                  step.status === 'failed' && 'text-red-700',
                  step.status === 'pending' && 'text-surface-400',
                )}
              >
                {label}
                {step.status === 'running' ? '…' : ''}
              </span>
              {stats && <span className="text-surface-400 ml-auto">{stats}</span>}
            </div>
            {step.output && (
              <div className="mt-1.5 pl-6">
                <TruncatedText text={step.output} limit={500} />
              </div>
            )}
            {step.error && (
              <p className="mt-1.5 pl-6 font-mono text-red-600 whitespace-pre-wrap break-all">{step.error}</p>
            )}
          </div>
        )
      })}
    </div>
  )
}

function ExpandableRow({ execution }: { execution: JobRun }) {
  const [open, setOpen] = useState(false)
  const isRunning = execution.status === 'running' || execution.status === 'pending' || execution.status === 'retrying'
  const hasDetail =
    isRunning ||
    Boolean(execution.llm_result) ||
    Boolean(execution.llm_error) ||
    Boolean(execution.last_error) ||
    Boolean(execution.delivery_result) ||
    Boolean(execution.delivery_error) ||
    Boolean(execution.request_body)

  const duration = execution.duration_ms
    ? formatMs(execution.duration_ms)
    : execution.started_at && execution.completed_at
      ? formatMs(new Date(execution.completed_at).getTime() - new Date(execution.started_at).getTime())
      : '—'

  const model = execution.request_body?.model ?? execution.steps?.find((s) => s.type === 'llm' && s.model)?.model ?? '—'

  const row = (withToggle: boolean) => (
    <TableRow className={cn(withToggle && 'cursor-pointer')} onClick={withToggle ? () => setOpen(!open) : undefined}>
      <TableCell className="pl-4 w-8">
        {withToggle ? (
          open ? (
            <ChevronDown size={14} className="text-surface-400" />
          ) : (
            <ChevronRight size={14} className="text-surface-400" />
          )
        ) : null}
      </TableCell>
      <TableCell>
        <div className="flex flex-col gap-0.5 items-start">
          <StatusBadge status={execution.status} />
          {execution.attempts && execution.attempts > 1 ? (
            <span className="text-[10px] text-surface-500 font-mono">Attempt #{execution.attempts}</span>
          ) : null}
        </div>
      </TableCell>
      <TableCell className="font-mono text-xs text-surface-500" title={new Date(execution.started_at).toLocaleString()}>
        <div className="flex flex-col">
          <TimeAgo date={execution.started_at} />
          {execution.status === 'retrying' && execution.next_retry_at && (
            <span className="text-[10px] text-amber-600 font-medium">
              {new Date(execution.next_retry_at).getTime() <= Date.now() ? (
                <span className="animate-pulse">Retry due — waiting for runner...</span>
              ) : (
                <>
                  Next retry: <TimeAgo date={execution.next_retry_at} />
                </>
              )}
            </span>
          )}
        </div>
      </TableCell>
      <TableCell className="font-mono text-xs text-surface-600">{duration}</TableCell>
      <TableCell className="font-mono text-xs text-surface-600 max-w-[120px] truncate">{model}</TableCell>
      <TableCell className="font-mono text-xs text-surface-600">
        Pr: {(execution.prompt_tokens ?? 0).toLocaleString()} / Co:{' '}
        {(execution.completion_tokens ?? 0).toLocaleString()}
      </TableCell>
      <TableCell className="font-mono text-xs text-surface-700">
        {execution.cost_usd == null ? '—' : execution.cost_usd < 0.0001 ? '$0.00' : `$${execution.cost_usd.toFixed(6)}`}
      </TableCell>
    </TableRow>
  )

  if (!hasDetail) return row(false)

  return (
    <>
      {row(true)}
      {open && (
        <tr>
          <td colSpan={7} className="p-0">
            <div className="bg-surface-50 border-b border-surface-200 px-6 py-4 space-y-3">
              {execution.steps && execution.steps.length > 0 ? (
                <StepList steps={execution.steps} live={isRunning} />
              ) : (
                isRunning && (
                  <div className="flex items-center gap-2 rounded border border-amber-200 bg-amber-50/80 p-3 text-xs text-amber-950">
                    <Loader2 size={16} className="animate-spin text-amber-600 shrink-0" />
                    <div>
                      <p className="font-medium capitalize">Execution status: {execution.status}</p>
                      <p className="text-[11px] text-amber-800 mt-0.5">
                        {execution.status === 'running'
                          ? 'Job is currently running step logic...'
                          : execution.status === 'retrying'
                            ? `Scheduled for retry attempt #${execution.attempts ?? 1}.`
                            : 'Job is queued in runner.'}
                      </p>
                    </div>
                  </div>
                )
              )}
              {execution.request_body && (
                <div>
                  <p className="text-xs font-medium text-surface-500 uppercase tracking-wider mb-1">LLM Input</p>
                  <div className="space-y-2">
                    <p className="text-xs text-surface-500">
                      Model: <span className="font-medium text-surface-700">{execution.request_body.model}</span>
                    </p>
                    <div className="font-mono text-xs text-surface-700 whitespace-pre-wrap break-all bg-white rounded p-2 border border-surface-200 max-h-60 overflow-y-auto">
                      <TruncatedText text={execution.request_body.rendered_prompt} />
                    </div>
                    {execution.request_body.variables && Object.keys(execution.request_body.variables).length > 0 && (
                      <div>
                        <p className="text-xs font-medium text-surface-500 uppercase tracking-wider mb-1">
                          Resolved Variables
                        </p>
                        <pre className="font-mono text-xs text-surface-700 whitespace-pre-wrap break-all bg-white rounded p-2 border border-surface-200">
                          {JSON.stringify(execution.request_body.variables, null, 2)}
                        </pre>
                      </div>
                    )}
                  </div>
                </div>
              )}
              {/* LLM Result/step errors are shown inline per-step above (StepList) when steps exist.
                  Only fall back to raw fields here for job-level failures with no per-step detail. */}
              {!execution.steps?.length && execution.llm_result && (
                <div>
                  <p className="text-xs font-medium text-surface-500 uppercase tracking-wider mb-1">LLM Result</p>
                  <div className="font-mono text-xs text-surface-700 whitespace-pre-wrap break-all bg-white rounded p-2 border border-surface-200 max-h-60 overflow-y-auto">
                    <TruncatedText text={execution.llm_result} />
                  </div>
                </div>
              )}
              {((!execution.steps?.length && (execution.llm_error || execution.last_error)) ||
                execution.delivery_error) && (
                <div>
                  <p className="text-xs font-medium text-surface-500 uppercase tracking-wider mb-1">Errors</p>
                  <div className="space-y-1">
                    {!execution.steps?.length && execution.llm_error && (
                      <p className="text-xs text-error">{execution.llm_error}</p>
                    )}
                    {!execution.steps?.length && !execution.llm_error && execution.last_error && (
                      <p className="text-xs text-error">{execution.last_error}</p>
                    )}
                    {/* Delivery happens after all steps, so its error is never shown by StepList. */}
                    {execution.delivery_error && <p className="text-xs text-error">{execution.delivery_error}</p>}
                  </div>
                </div>
              )}
              {execution.delivery_result && (
                <div>
                  <p className="text-xs font-medium text-surface-500 uppercase tracking-wider mb-1">Delivery</p>
                  <div className="text-xs text-surface-700 whitespace-pre-wrap break-all">
                    {execution.delivery_result}
                  </div>
                </div>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  )
}

function TableSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead className="w-8" />
          <TableHead className="text-xs font-medium uppercase tracking-wider text-surface-500">Status</TableHead>
          <TableHead className="text-xs font-medium uppercase tracking-wider text-surface-500">Started At</TableHead>
          <TableHead className="text-xs font-medium uppercase tracking-wider text-surface-500">Duration</TableHead>
          <TableHead className="text-xs font-medium uppercase tracking-wider text-surface-500">Model</TableHead>
          <TableHead className="text-xs font-medium uppercase tracking-wider text-surface-500">Tokens</TableHead>
          <TableHead className="text-xs font-medium uppercase tracking-wider text-surface-500">Cost</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {Array.from({ length: rows }).map((_, i) => (
          <TableRow key={i}>
            <TableCell className="pl-4 w-8" />
            <TableCell>
              <Skeleton className="h-5 w-16 rounded-full" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-3 w-14" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-3 w-12" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-3 w-20" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-3 w-24" />
            </TableCell>
            <TableCell>
              <Skeleton className="h-3 w-16" />
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  )
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center py-16">
      <p className="text-sm font-medium text-surface-500 mb-1">No executions yet</p>
      <p className="text-xs text-surface-400">Executions will appear here once the job runs.</p>
    </div>
  )
}

export function ExecutionHistory({ jobId }: ExecutionHistoryProps) {
  const [page, setPage] = useState(1)
  const perPage = 15

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: qk.jobRuns(jobId, page),
    queryFn: () => api.jobs.listJobRuns(jobId, { limit: perPage, offset: (page - 1) * perPage }),
    placeholderData: (prev) => prev,
    retry: 1,
    refetchInterval: (query) => {
      const execs = query.state.data?.runs ?? []
      const hasActive = execs.some((e) => e.status === 'running' || e.status === 'pending' || e.status === 'retrying')
      return hasActive ? 1_000 : 3_000
    },
  })

  const executions = data?.runs ?? []
  const total = data?.total ?? 0
  const totalPages = Math.max(1, Math.ceil(total / perPage))

  return (
    <div className="space-y-4">
      {isLoading ? (
        <Card>
          <CardContent className="p-0">
            <TableSkeleton />
          </CardContent>
        </Card>
      ) : error ? (
        <Card>
          <CardContent>
            <QueryState.Error title="Failed to load executions" message={error.message} onRetry={() => refetch()} />
          </CardContent>
        </Card>
      ) : executions.length === 0 ? (
        <Card>
          <CardContent>
            <EmptyState />
          </CardContent>
        </Card>
      ) : (
        <>
          <Card>
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-8" />
                    <TableHead className="text-xs font-medium uppercase tracking-wider text-surface-500">
                      Status
                    </TableHead>
                    <TableHead className="text-xs font-medium uppercase tracking-wider text-surface-500">
                      Started At
                    </TableHead>
                    <TableHead className="text-xs font-medium uppercase tracking-wider text-surface-500">
                      Duration
                    </TableHead>
                    <TableHead className="text-xs font-medium uppercase tracking-wider text-surface-500">
                      Model
                    </TableHead>
                    <TableHead className="text-xs font-medium uppercase tracking-wider text-surface-500">
                      Tokens
                    </TableHead>
                    <TableHead className="text-xs font-medium uppercase tracking-wider text-surface-500">
                      Cost
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {executions.map((ex) => (
                    <ExpandableRow key={ex.id} execution={ex} />
                  ))}
                </TableBody>
              </Table>
            </CardContent>
          </Card>

          {totalPages > 1 && (
            <div className="flex items-center justify-between">
              <p className="text-sm text-surface-500">
                Page <span className="font-medium">{page}</span> of <span className="font-medium">{totalPages}</span>
              </p>
              <div className="flex items-center gap-2">
                <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => setPage(page - 1)}>
                  Previous
                </Button>
                <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => setPage(page + 1)}>
                  Next
                </Button>
              </div>
            </div>
          )}
        </>
      )}
    </div>
  )
}
