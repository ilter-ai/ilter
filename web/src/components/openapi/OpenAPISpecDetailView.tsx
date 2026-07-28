import { ArrowLeft, CheckCircle, Edit3, Globe, Loader2, RefreshCw, Trash2, XCircle } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { api, type OpenAPISpec } from '../../lib/api'
import { logger } from '../../lib/logger'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { Skeleton } from '../ui/skeleton'
import { Switch } from '../ui/switch'
import { SpecFormModal } from './SpecFormModal'

interface Props {
  specId: string
  onBack: () => void
}

export function OpenAPISpecDetailView({ specId, onBack }: Props) {
  const [spec, setSpec] = useState<OpenAPISpec | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<{ status: string; operations_count?: number; error?: string } | null>(
    null,
  )
  const [showEditModal, setShowEditModal] = useState(false)

  const fetchSpecDetail = useCallback(() => {
    api.openapi
      .getOpenAPISpecs()
      .then((data) => {
        const raw = (Array.isArray(data) ? data : []).find((s: OpenAPISpec) => s.id === specId)
        if (raw) {
          setSpec(raw)
        } else {
          setError('Spec not found')
        }
      })
      .catch((e) => {
        logger.error('Failed to load spec:', e)
        setError('Failed to load spec')
      })
      .finally(() => setLoading(false))
  }, [specId])

  useEffect(() => {
    fetchSpecDetail()
  }, [fetchSpecDetail])

  const handleSync = useCallback(async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const res = await api.openapi.validateOpenAPISpec(specId)
      setTestResult(res)
      if (res.status === 'success') {
        toast.success(`Validation passed — ${res.operations_count ?? 0} operations found`)
        fetchSpecDetail()
      } else {
        toast.error(res.error || 'Validation failed')
      }
    } catch (e) {
      logger.error('Sync failed:', e)
      toast.error('Sync failed')
    } finally {
      setTesting(false)
    }
  }, [specId, fetchSpecDetail])

  const handleDelete = useCallback(() => {
    if (!spec) return
    if (!window.confirm(`Remove "${spec.name}"? This will make its operations unavailable.`)) return
    const prev = spec
    setSpec(null)
    api.openapi
      .deleteOpenAPISpec(specId)
      .then(() => {
        toast.success('Spec removed')
        onBack()
      })
      .catch((e) => {
        logger.error('Failed to remove spec:', e)
        setSpec(prev)
        toast.error('Failed to remove spec')
      })
  }, [spec, specId, onBack])

  const handleToggle = useCallback(async () => {
    if (!spec) return
    const newEnabled = !spec.enabled
    setSpec({ ...spec, enabled: newEnabled })
    try {
      await api.openapi.toggleOpenAPISpec(specId)
      toast.success(newEnabled ? 'Spec enabled' : 'Spec disabled')
    } catch {
      setSpec({ ...spec, enabled: !newEnabled })
      toast.error('Failed to toggle spec')
    }
  }, [spec, specId])

  const handleEditSave = useCallback(
    (data: Record<string, unknown>) => {
      if (!spec) return
      const prev = { ...spec }
      setSpec({ ...spec, ...data } as OpenAPISpec)
      setShowEditModal(false)
      api.openapi
        .updateOpenAPISpec(specId, data)
        .then(() => toast.success('Spec updated'))
        .catch((e) => {
          logger.error('Failed to update spec:', e)
          setSpec(prev)
          toast.error('Failed to update spec')
        })
    },
    [spec, specId],
  )

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-32" />
        <Skeleton className="h-48 rounded-xl" />
      </div>
    )
  }

  if (error || !spec) {
    return (
      <div className="flex flex-col items-center justify-center py-16">
        <p className="text-surface-500">{error || 'Spec not found'}</p>
        <Button variant="outline" size="sm" className="mt-4" onClick={onBack}>
          <ArrowLeft size={14} className="mr-1.5" /> Back
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <Button variant="ghost" size="sm" onClick={onBack}>
        <ArrowLeft size={16} className="mr-1.5" />
        Back to Specs
      </Button>

      <Card>
        <CardHeader>
          <div className="flex items-start justify-between">
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2">
                <Globe size={16} className="text-surface-400 shrink-0" />
                <CardTitle className="text-lg truncate">{spec.name}</CardTitle>
                <span
                  className={cn(
                    'inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-wider shrink-0',
                    spec.enabled
                      ? 'bg-success/10 text-success border border-success/20'
                      : 'bg-surface-100 text-surface-500 border border-surface-200',
                  )}
                >
                  {spec.enabled ? 'Enabled' : 'Disabled'}
                </span>
              </div>
              <CardDescription className="mt-1 font-mono text-xs">{spec.spec_url}</CardDescription>
            </div>
            <div className="flex items-center gap-2">
              <span className="text-xs text-surface-500">{spec.enabled ? 'Enabled' : 'Disabled'}</span>
              <Switch size="sm" checked={spec.enabled} onCheckedChange={handleToggle} />
            </div>
          </div>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={handleSync} disabled={testing}>
              {testing ? <Loader2 size={14} className="animate-spin mr-1" /> : <RefreshCw size={14} className="mr-1" />}
              {testing ? 'Syncing...' : 'Sync'}
            </Button>

            <Button variant="outline" size="sm" onClick={() => setShowEditModal(true)}>
              <Edit3 size={14} className="mr-1" /> Edit
            </Button>
            <Button variant="destructive" size="sm" onClick={handleDelete}>
              <Trash2 size={14} className="mr-1" /> Delete
            </Button>
          </div>
        </CardContent>
      </Card>

      {showEditModal && (
        <SpecFormModal
          open={showEditModal}
          onClose={() => setShowEditModal(false)}
          initial={spec}
          onSave={handleEditSave}
        />
      )}

      {testResult && (
        <Card>
          <CardContent className="pt-4">
            <div className="flex items-center gap-2">
              {testResult.status === 'success' ? (
                <CheckCircle size={16} className="text-success" />
              ) : (
                <XCircle size={16} className="text-error" />
              )}
              <span
                className={cn('text-sm font-medium', testResult.status === 'success' ? 'text-success' : 'text-error')}
              >
                {testResult.status === 'success' ? 'Validation passed' : 'Validation failed'}
              </span>
            </div>
            {testResult.operations_count !== undefined && (
              <p className="text-sm text-surface-500 mt-1 ml-6">
                {testResult.operations_count} operation{testResult.operations_count !== 1 ? 's' : ''} found in spec
              </p>
            )}
            {testResult.error && <p className="text-sm text-error mt-1 ml-6">{testResult.error}</p>}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Operations</CardTitle>
        </CardHeader>
        <CardContent>
          {!spec.operations || spec.operations.length === 0 ? (
            <p className="text-sm text-surface-500 py-4 text-center">No operations filtered for this spec.</p>
          ) : (
            <div className="space-y-1">
              {spec.operations.map((op, i) => (
                <div
                  key={i}
                  className="flex items-center gap-3 rounded-lg border border-surface-200 bg-surface-50 px-3 py-2 text-sm font-mono"
                >
                  <span className="text-surface-700">{op}</span>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      {spec.auth_type && spec.auth_type !== 'none' && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Authentication</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 gap-4 text-sm">
              <div>
                <span className="text-surface-400 block text-xs">Type</span>
                <span className="text-surface-700 font-medium">{spec.auth_type}</span>
              </div>
              <div>
                <span className="text-surface-400 block text-xs">Auth Key</span>
                <span className="text-surface-700 font-mono text-xs">{spec.auth_key || 'Authorization'}</span>
              </div>
              {spec.auth_value && (
                <div className="col-span-2">
                  <span className="text-surface-400 block text-xs">Auth Value</span>
                  <span className="text-surface-700 font-mono text-xs break-all">
                    {spec.auth_type === 'bearer'
                      ? `••••${spec.auth_value.slice(-8)}`
                      : spec.auth_type === 'api_key'
                        ? `••••${spec.auth_value.slice(-8)}`
                        : spec.auth_value}
                  </span>
                </div>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Configuration</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-2 gap-4 text-sm">
            <div>
              <span className="text-surface-400 block text-xs">Timeout</span>
              <span className="text-surface-700 font-medium">{spec.timeout_ms}ms</span>
            </div>
            <div>
              <span className="text-surface-400 block text-xs">Auth</span>
              <span className="text-surface-700 font-medium capitalize">{spec.auth_type || 'none'}</span>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
