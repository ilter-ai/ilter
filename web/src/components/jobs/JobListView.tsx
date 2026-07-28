import { Activity, AlertTriangle, Clock, Play, Plus, RefreshCw } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { JobStatusBadge, StatusBadge, TriggerTypeBadge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { type Column, DataTable } from '@/components/ui/DataTable'
import { EmptyState } from '@/components/ui/empty-state'
import { QueryProvider } from '@/components/ui/query-provider'
import { StatCard } from '@/components/ui/StatCard'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import { listJobs, triggerJob, updateJob } from '@/lib/api'
import type { Job } from '@/lib/api/types'
import { formatRelativeTime } from '@/lib/format'
import { logger } from '@/lib/logger'
import { cn } from '@/lib/utils'

interface JobListViewProps {
  onCreate?: () => void
  onSelect?: (job: Job) => void
}

const POLL_INTERVAL = 30_000

const triggerBadges = (job: Job) => {
  if (!job.triggers || job.triggers.length === 0) {
    return <span className="text-xs text-surface-400">—</span>
  }
  const kinds = new Set(job.triggers.filter((t) => t.enabled).map((t) => t.kind))
  if (kinds.size === 0) {
    return <span className="text-xs text-surface-400">—</span>
  }
  return (
    <span className="inline-flex items-center gap-1">
      {kinds.has('cron') && <TriggerTypeBadge kind="cron" title="Cron schedule" />}
      {kinds.has('webhook') && <TriggerTypeBadge kind="webhook" title="Webhook trigger" />}
    </span>
  )
}

function JobListViewContent({ onCreate, onSelect }: JobListViewProps) {
  const [jobs, setJobs] = useState<Job[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)
  const [runningIds, setRunningIds] = useState<Set<string>>(new Set())

  const fetchJobs = useCallback(async () => {
    try {
      const data = await listJobs()
      setJobs(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      setIsLoading(false)
    }
  }, [])

  useEffect(() => {
    fetchJobs()
    const interval = setInterval(fetchJobs, POLL_INTERVAL)
    return () => clearInterval(interval)
  }, [fetchJobs])

  const handleToggle = async (job: Job) => {
    const prev = jobs
    setJobs((j) => j.map((cj) => (cj.id === job.id ? { ...cj, enabled: !cj.enabled } : cj)))
    try {
      await updateJob(job.id, { enabled: !job.enabled })
      toast.success(job.enabled ? 'Job disabled' : 'Job enabled', {
        description: `"${job.name}" is now ${job.enabled ? 'disabled' : 'active'}.`,
      })
    } catch (err) {
      logger.error('Failed to toggle job:', err)
      setJobs(prev)
      toast.error('Failed to toggle job')
    }
  }

  const handleRunNow = async (id: string) => {
    setRunningIds((prev) => new Set(prev).add(id))
    try {
      const result = await triggerJob(id)
      const shortId = result.run_id.length > 20 ? `${result.run_id.slice(0, 20)}…` : result.run_id
      toast.success('Job triggered', {
        description: `Run started (${shortId}).`,
      })
      // Optimistically update the job row so user sees "running" immediately
      setJobs((prev) =>
        prev.map((j) =>
          j.id === id ? { ...j, last_exec_status: 'running', last_exec_started_at: new Date().toISOString() } : j,
        ),
      )
    } catch (err) {
      logger.error('Failed to trigger job:', err)
      toast.error('Failed to trigger job')
    } finally {
      setRunningIds((prev) => {
        const next = new Set(prev)
        next.delete(id)
        return next
      })
    }
  }

  const columns: Column<Job>[] = [
    {
      key: 'name',
      header: 'Name',
      render: (job) => (
        <div className="space-y-0.5">
          <div className="flex items-center gap-2">
            <span className="font-medium text-surface-900">{job.name}</span>
            {triggerBadges(job)}
          </div>
          {job.description && <p className="text-xs text-surface-500 truncate max-w-[220px]">{job.description}</p>}
        </div>
      ),
    },
    {
      key: 'cron_expr',
      header: 'Schedule',
      render: (job) => (
        <code className="text-xs bg-surface-100 text-surface-700 px-1.5 py-0.5 rounded font-mono">{job.cron_expr}</code>
      ),
    },
    {
      key: 'next_run',
      header: 'Next Run',
      render: (job) => {
        if (!job.next_run) return <span className="text-xs text-surface-400">—</span>
        const next = new Date(job.next_run)
        const now = Date.now()
        const diff = next.getTime() - now
        if (diff < 0) return <span className="text-xs text-surface-400">now</span>
        const mins = Math.floor(diff / 60000)
        let label: string
        if (mins < 60) label = `${mins}m`
        else if (mins < 1440) label = `${Math.floor(mins / 60)}h ${mins % 60}m`
        else label = `${Math.floor(mins / 1440)}d ${Math.floor((mins % 1440) / 60)}h`
        return (
          <span className="text-xs text-surface-600" title={next.toLocaleString()}>
            {label}
          </span>
        )
      },
    },
    {
      key: 'last_exec_status',
      header: 'Last Run',
      render: (job) => {
        const time = job.last_exec_started_at
        return (
          <div className="flex items-center gap-2">
            <JobStatusBadge status={job.last_exec_status} />
            <span className="text-xs text-surface-500">{time ? formatRelativeTime(time) : '—'}</span>
          </div>
        )
      },
    },
    {
      key: 'enabled',
      header: 'Status',
      render: (job) => <StatusBadge isActive={job.enabled} />,
    },
    {
      key: 'actions',
      header: 'Actions',
      className: 'w-20',
      render: (job) => (
        <JobActionsCell job={job} onToggle={handleToggle} onRun={handleRunNow} running={runningIds.has(job.id)} />
      ),
    },
  ]

  if (isLoading) {
    return (
      <div className="rounded-xl border border-surface-200 bg-white shadow-card">
        <div className="flex border-b border-surface-200 px-4 py-3 gap-4">
          {Array.from({ length: 6 }).map((_, ci) => (
            <Skeleton key={`h-${ci}`} className={cn('h-3', ci === 0 ? 'w-1/4' : 'w-1/6')} />
          ))}
        </div>
        {Array.from({ length: 4 }).map((_, ri) => (
          <div key={`r-${ri}`} className="flex border-b border-surface-100 px-4 py-3.5 gap-4 last:border-b-0">
            {Array.from({ length: 6 }).map((_, ci) => (
              <Skeleton key={`c-${ri}-${ci}`} className={cn('h-3', ci === 0 ? 'w-2/5' : 'w-1/5')} />
            ))}
          </div>
        ))}
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center py-16">
        <div className="rounded-full bg-error/10 p-4 mb-4">
          <AlertTriangle size={32} className="text-error" />
        </div>
        <h3 className="text-lg font-semibold text-surface-900 mb-1">Failed to load jobs</h3>
        <p className="text-sm text-surface-500 mb-6 max-w-md text-center">{error.message}</p>
        <Button variant="outline" size="sm" onClick={fetchJobs}>
          <RefreshCw size={14} className="mr-1.5" />
          Retry
        </Button>
      </div>
    )
  }

  const activeCount = jobs.filter((j) => j.enabled).length
  const totalCount = jobs.length

  const nextRunJob = jobs
    .filter((j) => j.enabled && j.next_run)
    .sort((a, b) => new Date(a.next_run!).getTime() - new Date(b.next_run!).getTime())[0]
  const nextRunLabel = nextRunJob ? formatRelativeTime(nextRunJob.next_run!) : '—'

  const failedCount = jobs.filter(
    (j) => j.last_exec_status && ['llm_failed', 'delivery_failed', 'budget_exceeded'].includes(j.last_exec_status),
  ).length

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-3 gap-4">
        <StatCard
          title="Active Jobs"
          value={`${activeCount} / ${totalCount}`}
          description="Enabled / Total"
          icon={<Activity size={20} />}
        />
        <StatCard
          title="Next Run"
          value={nextRunLabel}
          description={nextRunJob?.name ?? 'No upcoming runs'}
          icon={<Clock size={20} />}
        />
        <StatCard
          title="Recent Failures"
          value={failedCount}
          description={failedCount === 1 ? 'job needs attention' : 'jobs need attention'}
          icon={<AlertTriangle size={20} />}
        />
      </div>

      <div className="flex items-center justify-end">
        {onCreate && (
          <Button onClick={onCreate}>
            <Plus size={16} />
            Create Job
          </Button>
        )}
      </div>

      {jobs.length === 0 ? (
        <EmptyState
          title="No jobs yet"
          description="Create one to get started with scheduled AI prompts."
          action={onCreate ? { label: 'Create Job', onClick: onCreate } : undefined}
        />
      ) : (
        <Card>
          <DataTable columns={columns} data={jobs} keyExtractor={(job) => job.id} onRowClick={onSelect} />
        </Card>
      )}
    </div>
  )
}

function JobActionsCell({
  job,
  onToggle,
  onRun,
  running,
}: {
  job: Job
  onToggle: (job: Job) => void
  onRun: (id: string) => void
  running: boolean
}) {
  return (
    <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
      <Switch size="sm" checked={job.enabled} onCheckedChange={() => onToggle(job)} />
      <button
        onClick={() => onRun(job.id)}
        disabled={running}
        className="p-1.5 text-surface-400 hover:text-brand-600 transition-colors rounded-md hover:bg-surface-100 disabled:opacity-40 disabled:cursor-not-allowed"
        title="Run Now"
      >
        <Play size={14} />
      </button>
    </div>
  )
}

export function JobListView({ onCreate, onSelect }: JobListViewProps) {
  return (
    <QueryProvider>
      <JobListViewContent onCreate={onCreate} onSelect={onSelect} />
    </QueryProvider>
  )
}
