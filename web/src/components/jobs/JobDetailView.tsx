import { toString as cronToString } from 'cronstrue'
import { AlertTriangle, ArrowLeft, Clock, Globe, Play, RefreshCw, Trash2 } from 'lucide-react'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { QueryProvider } from '@/components/ui/query-provider'
import { Skeleton } from '@/components/ui/skeleton'
import { deleteJob, deleteTrigger, getJob, listJobTriggers, triggerJob, updateJob } from '@/lib/api'
import type { Job, Trigger } from '@/lib/api/types'
import { logger } from '@/lib/logger'
import { queryClient } from '@/lib/queryClient'
import { cn } from '@/lib/utils'
import { ExecutionHistory } from './ExecutionHistory'
import { WebhookInvokePanel } from './WebhookInvokePanel'

interface JobDetailViewProps {
  jobId: string
  initialTab?: Tab
  onBack: () => void
  onEdit?: () => void
  onDelete?: () => void
  onTabChange?: (tab: Tab) => void
}

type Tab = 'config' | 'parameters' | 'executions' | 'triggers'

function describeCron(expr: string): string {
  try {
    return cronToString(expr)
  } catch {
    return ''
  }
}

const statusBadge = (enabled: boolean) => (
  <span
    className={cn(
      'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium',
      enabled
        ? 'bg-success/10 text-success border border-success/20'
        : 'bg-surface-100 text-surface-500 border border-surface-200',
    )}
  >
    {enabled ? 'Active' : 'Disabled'}
  </span>
)

function DetailField({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex justify-between items-start py-2 border-b border-surface-100 last:border-b-0">
      <span className="text-xs font-medium text-surface-500 w-1/3 shrink-0">{label}</span>
      <div className="text-sm text-surface-900 w-2/3 text-right">{children}</div>
    </div>
  )
}

export function JobDetailView({ jobId, initialTab, onBack, onEdit, onDelete, onTabChange }: JobDetailViewProps) {
  const [tab, setTab] = useState<Tab>(initialTab || 'executions')
  const [job, setJob] = useState<Job | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<Error | null>(null)
  const [running, setRunning] = useState(false)
  const [triggers, setTriggers] = useState<Trigger[]>([])
  const [triggersLoading, setTriggersLoading] = useState(false)
  const [invokePanelTriggerId, setInvokePanelTriggerId] = useState<string | null>(null)
  const [deleteConfirmId, setDeleteConfirmId] = useState<string | null>(null)
  const [deleting, setDeleting] = useState(false)
  const [showJobDelete, setShowJobDelete] = useState(false)
  const [deletingJob, setDeletingJob] = useState(false)

  useEffect(() => {
    let cancelled = false
    setIsLoading(true)
    setError(null)
    getJob(jobId)
      .then((data) => {
        if (!cancelled) setJob(data)
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err : new Error(String(err)))
      })
      .finally(() => {
        if (!cancelled) setIsLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [jobId])

  useEffect(() => {
    let cancelled = false
    setTriggersLoading(true)
    listJobTriggers(jobId)
      .then((data) => {
        if (!cancelled) setTriggers(data)
      })
      .catch((err) => {
        if (cancelled) return
        logger.error('Failed to load triggers:', err)
        toast.error('Failed to load triggers')
      })
      .finally(() => {
        if (!cancelled) setTriggersLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [jobId])

  const handleDeleteTrigger = async () => {
    if (!deleteConfirmId) return
    setDeleting(true)
    try {
      await deleteTrigger(deleteConfirmId)
      toast.success('Trigger deleted')
      setTriggers((prev) => prev.filter((t) => t.id !== deleteConfirmId))
      setDeleteConfirmId(null)
    } catch (err) {
      logger.error('Failed to delete trigger:', err)
      toast.error('Failed to delete trigger')
    } finally {
      setDeleting(false)
    }
  }

  const handleToggle = async () => {
    if (!job) return
    const prev = { ...job }
    setJob({ ...job, enabled: !job.enabled })
    try {
      await updateJob(job.id, { enabled: !job.enabled })
      toast.success(job.enabled ? 'Job disabled' : 'Job enabled')
    } catch (err) {
      logger.error('Failed to toggle job:', err)
      setJob(prev)
      toast.error('Failed to toggle job')
    }
  }

  const handleDeleteJob = async () => {
    setDeletingJob(true)
    try {
      await deleteJob(jobId)
      toast.success('Job deleted', { description: `"${job?.name}" has been removed.` })
      onDelete?.()
      onBack()
    } catch (err) {
      logger.error('Failed to delete job:', err)
      toast.error('Failed to delete job')
    } finally {
      setDeletingJob(false)
      setShowJobDelete(false)
    }
  }

  if (isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-32" />
        <Skeleton className="h-64 rounded-xl" />
      </div>
    )
  }

  if (error) {
    return (
      <div className="flex flex-col items-center justify-center py-16">
        <div className="rounded-full bg-error/10 p-4 mb-4">
          <AlertTriangle size={32} className="text-error" />
        </div>
        <h3 className="text-lg font-semibold text-surface-900 mb-1">Failed to load job</h3>
        <p className="text-sm text-surface-500 mb-6 max-w-md text-center">{error.message}</p>
        <Button
          variant="outline"
          size="sm"
          onClick={() => {
            setIsLoading(true)
            setError(null)
            getJob(jobId)
              .then(setJob)
              .catch((err) => setError(err instanceof Error ? err : new Error(String(err))))
              .finally(() => setIsLoading(false))
          }}
        >
          <RefreshCw size={14} className="mr-1.5" />
          Retry
        </Button>
      </div>
    )
  }

  if (!job) return null

  const variablesParsed: Record<string, unknown> | null = job.variables_config ?? null

  return (
    <div className="space-y-6">
      <Button variant="ghost" size="sm" onClick={onBack}>
        <ArrowLeft size={16} className="mr-1.5" />
        Back to Jobs
      </Button>

      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h2 className="text-xl font-semibold text-surface-900">{job.name}</h2>
          {statusBadge(job.enabled)}
        </div>
        <div className="flex items-center gap-2">
          <Button
            size="sm"
            type="button"
            onClick={async () => {
              setRunning(true)
              try {
                await triggerJob(jobId)
                toast.success('Job triggered', { description: 'Execution started.' })
                queryClient.invalidateQueries({ queryKey: ['jobs', jobId, 'runs'] })
              } catch (err) {
                logger.error('Failed to trigger job:', err)
                toast.error('Failed to trigger job')
              } finally {
                setRunning(false)
              }
            }}
            disabled={running}
          >
            <Play size={14} className="mr-1.5" />
            {running ? 'Running...' : 'Run Now'}
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={() => {
              setTriggersLoading(true)
              setIsLoading(true)
              Promise.all([
                getJob(jobId)
                  .then(setJob)
                  .catch(() => {}),
                listJobTriggers(jobId)
                  .then(setTriggers)
                  .catch(() => {}),
              ]).finally(() => {
                setTriggersLoading(false)
                setIsLoading(false)
              })
              queryClient.invalidateQueries({ queryKey: ['jobs', jobId, 'runs'] })
            }}
            disabled={triggersLoading}
          >
            <RefreshCw size={14} className={`mr-1.5 ${triggersLoading ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
          {onEdit && (
            <Button variant="outline" size="sm" onClick={onEdit}>
              Edit
            </Button>
          )}
          <Button variant={job.enabled ? 'outline' : 'default'} size="sm" onClick={handleToggle}>
            {job.enabled ? 'Disable' : 'Enable'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setShowJobDelete(true)}
            className="text-destructive hover:bg-destructive/10 hover:text-destructive border-destructive/30"
          >
            <Trash2 size={14} className="mr-1.5" />
            Delete
          </Button>
        </div>
      </div>

      <div role="tablist" className="flex gap-1 border-b border-surface-200">
        {(
          [
            'executions',
            'config',
            'triggers',
            ...(variablesParsed && Object.keys(variablesParsed).length > 0 ? ['parameters' as const] : []),
          ] as Tab[]
        ).map((t) => (
          <button
            key={t}
            role="tab"
            aria-selected={tab === t}
            onClick={() => {
              setTab(t)
              onTabChange?.(t)
            }}
            className={`px-4 py-2.5 text-sm font-medium border-b-2 transition-colors capitalize ${
              tab === t
                ? 'border-brand-600 text-brand-700'
                : 'border-transparent text-surface-500 hover:text-surface-700 hover:border-surface-300'
            }`}
          >
            {t}
          </button>
        ))}
      </div>

      {tab === 'config' ? (
        <Card>
          <CardContent className="pt-4">
            {job.description && (
              <p className="text-sm text-surface-600 mb-4 pb-4 border-b border-surface-100">{job.description}</p>
            )}
            <div className="divide-y divide-surface-100">
              <DetailField label="Schedule">
                <code className="text-xs bg-surface-100 text-surface-700 px-1.5 py-0.5 rounded font-mono">
                  {job.cron_expr}
                </code>
              </DetailField>
              {(() => {
                try {
                  const steps = JSON.parse(job.steps)
                  if (Array.isArray(steps)) {
                    return (
                      <DetailField label={`Steps (${steps.length})`}>
                        <div className="text-right space-y-1">
                          {steps.map((s: { type: string; tool?: string; prompt_id?: number }, i: number) => (
                            <div key={i} className="text-xs text-surface-600">
                              <span className="font-medium">{i + 1}.</span>{' '}
                              {s.type === 'mcp' ? (
                                <span className="font-mono text-[11px]">{s.tool || 'MCP tool'}</span>
                              ) : (
                                <span>LLM {s.prompt_id ? `#${s.prompt_id}` : ''}</span>
                              )}
                            </div>
                          ))}
                        </div>
                      </DetailField>
                    )
                  }
                } catch {
                  // JSON.parse only throws SyntaxError
                }
                return null
              })()}
              <DetailField label="Timeout">{job.timeout_ms}ms</DetailField>
              <DetailField label="API Key ID">{job.api_key_id || '—'}</DetailField>
              <DetailField label="Created">{new Date(job.created_at).toLocaleString()}</DetailField>
              <DetailField label="Updated">{new Date(job.updated_at).toLocaleString()}</DetailField>
            </div>
          </CardContent>
        </Card>
      ) : tab === 'parameters' ? (
        <Card>
          <CardContent className="pt-4">
            <span className="text-xs font-medium text-surface-500 block mb-2">Job Variables</span>
            {variablesParsed && Object.keys(variablesParsed).length > 0 ? (
              <div className="divide-y divide-surface-100 border border-surface-200 rounded-lg overflow-hidden">
                {Object.entries(variablesParsed).map(([key, val]) => {
                  const strVal = typeof val === 'object' && val !== null ? JSON.stringify(val, null, 2) : String(val)
                  const isLong = strVal.length > 100 || strVal.includes('\n')
                  return (
                    <div key={key} className="px-3 py-2.5 space-y-1">
                      <span className="text-xs font-mono font-semibold text-surface-500 uppercase tracking-wide">
                        {key}
                      </span>
                      {isLong ? (
                        <pre className="text-xs text-surface-700 bg-surface-50 rounded-md p-2.5 overflow-x-auto whitespace-pre-wrap break-all font-mono leading-relaxed border border-surface-100">
                          {strVal}
                        </pre>
                      ) : (
                        <p className="text-sm text-surface-900">{strVal}</p>
                      )}
                    </div>
                  )
                })}
              </div>
            ) : (
              <p className="text-sm text-surface-400">No job variables configured</p>
            )}
          </CardContent>
        </Card>
      ) : tab === 'executions' ? (
        <div className="space-y-4">
          <QueryProvider>
            <ExecutionHistory jobId={jobId} />
          </QueryProvider>
        </div>
      ) : tab === 'triggers' ? (
        <div className="space-y-4">
          {triggersLoading && triggers.length === 0 ? (
            <div className="space-y-3">
              <Skeleton className="h-20 rounded-xl" />
              <Skeleton className="h-20 rounded-xl" />
            </div>
          ) : triggers.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-12 text-surface-400">
              <Clock size={32} className="mb-3 opacity-40" />
              <p className="text-sm font-medium">No triggers configured</p>
              <p className="text-xs mt-1">Add a cron or webhook trigger to automate this job.</p>
            </div>
          ) : (
            <div className="space-y-3">
              {triggers.map((trigger) => {
                let cfg: Record<string, string> = {}
                try {
                  cfg = JSON.parse(trigger.config)
                } catch {
                  // JSON.parse only throws SyntaxError
                }
                const isWebhook = trigger.kind === 'webhook'
                const isCron = trigger.kind === 'cron'

                return (
                  <Card key={trigger.id}>
                    <CardContent className="pt-4">
                      <div className="flex items-start justify-between mb-3">
                        <div className="flex items-center gap-2">
                          {isCron ? (
                            <Clock size={16} className="text-brand-600" />
                          ) : isWebhook ? (
                            <Globe size={16} className="text-brand-600" />
                          ) : (
                            <Clock size={16} className="text-surface-400" />
                          )}
                          <span className="text-sm font-medium capitalize text-surface-900">{trigger.kind}</span>
                          {statusBadge(trigger.enabled)}
                        </div>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => setDeleteConfirmId(trigger.id)}
                          className="text-surface-400 hover:text-destructive"
                        >
                          <Trash2 size={14} />
                        </Button>
                      </div>

                      <div className="space-y-1.5 text-sm">
                        {isCron && (
                          <>
                            <div className="flex justify-between">
                              <span className="text-surface-500 text-xs">Expression</span>
                              <code className="text-xs bg-surface-100 text-surface-700 px-1.5 py-0.5 rounded font-mono">
                                {cfg.expr || '—'}
                              </code>
                            </div>
                            {cfg.expr && describeCron(cfg.expr) && (
                              <div className="flex justify-between">
                                <span className="text-surface-500 text-xs">Schedule</span>
                                <span className="text-xs text-surface-700">{describeCron(cfg.expr)}</span>
                              </div>
                            )}
                            {cfg.timezone && (
                              <div className="flex justify-between">
                                <span className="text-surface-500 text-xs">Timezone</span>
                                <span className="text-xs text-surface-700">{cfg.timezone}</span>
                              </div>
                            )}
                          </>
                        )}
                        {isWebhook && (
                          <>
                            {cfg.provider && (
                              <div className="flex justify-between">
                                <span className="text-surface-500 text-xs">Provider</span>
                                <span className="text-xs text-surface-700 capitalize">{cfg.provider}</span>
                              </div>
                            )}
                            <div className="flex justify-end pt-1">
                              <Button variant="outline" size="sm" onClick={() => setInvokePanelTriggerId(trigger.id)}>
                                <Globe size={13} />
                                How to Invoke
                              </Button>
                            </div>
                          </>
                        )}
                      </div>
                    </CardContent>
                  </Card>
                )
              })}
            </div>
          )}
        </div>
      ) : null}

      <Dialog
        open={deleteConfirmId !== null}
        onOpenChange={(open) => {
          if (!open) setDeleteConfirmId(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Trigger</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete this trigger? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteConfirmId(null)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleDeleteTrigger} disabled={deleting}>
              {deleting ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={showJobDelete}
        onOpenChange={(open) => {
          if (!open) setShowJobDelete(false)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete Job</DialogTitle>
            <DialogDescription>
              Are you sure you want to delete "{job?.name}"? This action cannot be undone.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowJobDelete(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleDeleteJob} disabled={deletingJob}>
              {deletingJob ? 'Deleting...' : 'Delete'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {invokePanelTriggerId && (
        <WebhookInvokePanel
          jobId={jobId}
          triggerId={invokePanelTriggerId}
          onClose={() => setInvokePanelTriggerId(null)}
        />
      )}
    </div>
  )
}
