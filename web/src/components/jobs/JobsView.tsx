import { ArrowLeft } from 'lucide-react'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { QueryProvider } from '@/components/ui/query-provider'
import { Skeleton } from '@/components/ui/skeleton'
import { createJob, getJob, updateJob } from '@/lib/api'
import type { Job, Trigger } from '@/lib/api/types'
import { JobDetailView } from './JobDetailView'
import { JobForm } from './JobForm'
import { JobListView } from './JobListView'
import { WebhookInvokePanel } from './WebhookInvokePanel'

interface PendingReveal {
  jobId: string
  trigger: Trigger
}

function EditJob({
  jobId,
  onDone,
  onCancel,
}: {
  jobId: string
  onDone: (newTriggers: Trigger[]) => void
  onCancel: () => void
}) {
  const [job, setJob] = useState<Job | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    getJob(jobId)
      .then((data) => {
        if (!cancelled) setJob(data)
      })
      .catch((e) => {
        if (!cancelled) setError(e instanceof Error ? e.message : 'Failed to load')
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [jobId])

  if (loading) {
    return (
      <div className="space-y-6">
        <Button variant="ghost" size="sm" disabled>
          <ArrowLeft size={16} className="mr-1.5" /> Back
        </Button>
        <h2 className="text-xl font-semibold text-surface-900">Edit Job</h2>
        <Skeleton className="h-64 rounded-xl" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="space-y-6">
        <Button variant="ghost" size="sm" onClick={onCancel}>
          <ArrowLeft size={16} className="mr-1.5" /> Back
        </Button>
        <h2 className="text-xl font-semibold text-surface-900">Edit Job</h2>
        <div className="rounded-lg border border-error/20 bg-error/5 p-4 text-center">
          <p className="text-sm text-error mb-3">{error}</p>
          <Button
            variant="outline"
            size="sm"
            onClick={() => {
              setLoading(true)
              setError(null)
              getJob(jobId)
                .then(setJob)
                .catch((e) => setError(e instanceof Error ? e.message : 'Failed to load'))
                .finally(() => setLoading(false))
            }}
          >
            Retry
          </Button>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <Button variant="ghost" size="sm" onClick={onCancel}>
        <ArrowLeft size={16} className="mr-1.5" /> Back
      </Button>
      <h2 className="text-xl font-semibold text-surface-900">Edit Job</h2>
      <JobForm
        initialData={job!}
        onSubmit={async (data) => {
          const updated = await updateJob(jobId, data)
          toast.success('Job updated')
          onDone((updated.triggers ?? []).filter((t): t is Trigger => !!t.token))
        }}
        onCancel={onCancel}
      />
    </div>
  )
}

export function JobsView() {
  const [path, setPath] = useState(() => window.location.pathname)
  // Triggers created by this Create/Edit action, queued to show their
  // one-time-fresh credentials via WebhookInvokePanel — the same panel
  // reachable anytime later from the job detail page's Triggers tab.
  const [revealQueue, setRevealQueue] = useState<PendingReveal[]>([])

  useEffect(() => {
    const onPop = () => setPath(window.location.pathname)
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])

  const navigate = (p: string) => {
    window.history.pushState(null, '', p)
    setPath(p)
  }

  const queueReveals = (jobId: string, triggers: Trigger[]) => {
    const webhookTriggers = triggers.filter((t) => !!t.token)
    if (webhookTriggers.length === 0) return
    setRevealQueue((prev) => [...prev, ...webhookTriggers.map((trigger) => ({ jobId, trigger }))])
  }

  // Match /jobs/<id> or /jobs/<id>/executions|config
  const detailMatch = path.match(/^\/jobs\/([^/]+?)(?:\/(?:executions|config))?\/?$/)
  const editMatch = path.match(/^\/jobs\/([^/]+?)\/edit\/?$/)
  const isNew = path === '/jobs/new' || path === '/jobs/new/'

  const activeReveal = revealQueue[0]
  const revealPanel = activeReveal && (
    <WebhookInvokePanel
      jobId={activeReveal.jobId}
      triggerId={activeReveal.trigger.id}
      initialToken={activeReveal.trigger.token}
      initialSecret={activeReveal.trigger.secret}
      onClose={() => setRevealQueue((prev) => prev.slice(1))}
    />
  )

  if (isNew) {
    return (
      <div className="space-y-6">
        {revealPanel}
        <Button variant="ghost" size="sm" onClick={() => navigate('/jobs')}>
          <ArrowLeft size={16} className="mr-1.5" /> Back to Jobs
        </Button>
        <h2 className="text-xl font-semibold text-surface-900">Create Job</h2>
        <JobForm
          onSubmit={async (data) => {
            const created = await createJob(data)
            queueReveals(created.id, created.triggers ?? [])
            toast.success('Job created')
            navigate('/jobs')
          }}
          onCancel={() => navigate('/jobs')}
        />
      </div>
    )
  }

  if (editMatch) {
    const jobId = editMatch[1]
    return (
      <div className="space-y-6">
        {revealPanel}
        <QueryProvider>
          <EditJob
            jobId={jobId}
            onDone={(triggers) => {
              queueReveals(jobId, triggers)
              navigate(`/jobs/${jobId}`)
            }}
            onCancel={() => navigate(`/jobs/${jobId}`)}
          />
        </QueryProvider>
      </div>
    )
  }

  const selectedJobId = detailMatch?.[1] ?? null

  return (
    <div>
      {revealPanel}
      {!selectedJobId ? (
        <JobListView onCreate={() => navigate('/jobs/new')} onSelect={(job) => navigate(`/jobs/${job.id}`)} />
      ) : (
        <JobDetailView
          jobId={selectedJobId}
          initialTab="executions"
          onBack={() => navigate('/jobs')}
          onEdit={() => navigate(`/jobs/${selectedJobId}/edit`)}
          onDelete={() => navigate('/jobs')}
          onTabChange={(tab) =>
            navigate(tab === 'executions' ? `/jobs/${selectedJobId}/executions` : `/jobs/${selectedJobId}/config`)
          }
        />
      )}
    </div>
  )
}
