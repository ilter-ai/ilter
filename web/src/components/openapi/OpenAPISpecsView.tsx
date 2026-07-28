import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { api, type OpenAPISpec } from '../../lib/api'
import { logger } from '../../lib/logger'
import { cn } from '../../lib/utils'
import { FeatureStatus } from '../settings/FeatureStatus'
import { Button } from '../ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { EmptyState } from '../ui/empty-state'
import { FilterBar } from '../ui/FilterBar'
import { Edit3, Globe, Loader2, Plus, RefreshCw, Trash2 } from '../ui/icons'
import { StatCard } from '../ui/StatCard'
import { Skeleton } from '../ui/skeleton'
import { Switch } from '../ui/switch'
import { SpecFormModal } from './SpecFormModal'

const statusConfig = {
  online: { dot: 'bg-success', label: 'Online', text: 'text-success', bg: 'bg-success/10 border-success/20' },
  offline: { dot: 'bg-error', label: 'Offline', text: 'text-error', bg: 'bg-error/10 border-error/20' },
}

export function OpenAPISpecsView({ onNavigate }: { onNavigate?: (path: string) => void }) {
  const [specs, setSpecs] = useState<OpenAPISpec[]>([])
  const [showAddModal, setShowAddModal] = useState(false)
  const [editSpecId, setEditSpecId] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [syncingId, setSyncingId] = useState<string | null>(null)
  const [openApiFeatureEnabled, setOpenApiFeatureEnabled] = useState(true)
  const [togglingOpenApiFeature, setTogglingOpenApiFeature] = useState(false)

  useEffect(() => {
    api.features
      .getFeatures()
      .then((flags) => {
        const flag = flags.find((f) => f.feature_key === 'openapi')
        if (flag) setOpenApiFeatureEnabled(flag.enabled)
      })
      .catch(() => {})

    api.openapi
      .getOpenAPISpecs()
      .then((data) => {
        if (!Array.isArray(data)) return setSpecs([])
        setSpecs(data)
      })
      .catch((e) => {
        logger.error('Failed to load OpenAPI specs:', e)
        toast.error('Failed to load OpenAPI specs')
      })
      .finally(() => setLoading(false))
  }, [])

  const handleToggleOpenApiFeature = async () => {
    const next = !openApiFeatureEnabled
    setOpenApiFeatureEnabled(next)
    setTogglingOpenApiFeature(true)
    try {
      await api.features.toggleFeature('openapi', next)
      toast.success(next ? 'OpenAPI feature enabled' : 'OpenAPI feature disabled')
    } catch {
      setOpenApiFeatureEnabled(!next)
      toast.error('Failed to update OpenAPI feature state')
    } finally {
      setTogglingOpenApiFeature(false)
    }
  }

  const editing = specs.find((s) => s.id === editSpecId) ?? null

  const addSpec = (data: Record<string, unknown>) => {
    api.openapi
      .createOpenAPISpec(data as Parameters<typeof api.openapi.createOpenAPISpec>[0])
      .then((res) => {
        const newSpec: OpenAPISpec = {
          id: res.id || String(Date.now()),
          name: String(data.name ?? ''),
          spec_url: String(data.spec_url ?? ''),
          operations: Array.isArray(data.operations) ? (data.operations as string[]) : [],
          auth_type: String(data.auth_type ?? 'none'),
          auth_value: String(data.auth_value ?? ''),
          auth_key: String(data.auth_key ?? ''),
          timeout_ms: Number(data.timeout_ms ?? 30000),
          enabled: Boolean(data.enabled ?? true),
          created_at: new Date().toISOString(),
          updated_at: new Date().toISOString(),
        }
        setSpecs((prev) => [...prev, newSpec])
        toast.success('Spec added')
      })
      .catch((e) => {
        logger.error('Failed to add spec:', e)
        toast.error('Failed to add spec')
      })
  }

  const updateSpec = (data: Record<string, unknown>) => {
    if (!editing) return
    const prev = specs
    setSpecs(specs.map((s) => (s.id === editing.id ? { ...s, ...data, updated_at: new Date().toISOString() } : s)))
    setEditSpecId(null)
    api.openapi
      .updateOpenAPISpec(editing.id, data as Parameters<typeof api.openapi.updateOpenAPISpec>[1])
      .then(() => toast.success('Spec updated'))
      .catch((e) => {
        logger.error('Failed to update spec:', e)
        setSpecs(prev)
        toast.error('Failed to update spec')
      })
  }

  const deleteSpec = (id: string) => {
    const prev = specs
    setSpecs(specs.filter((s) => s.id !== id))
    api.openapi
      .deleteOpenAPISpec(id)
      .then(() => toast.success('Spec removed'))
      .catch((e) => {
        logger.error('Failed to remove spec:', e)
        setSpecs(prev)
        toast.error('Failed to remove spec')
      })
  }

  const testSpec = useCallback(async (id: string) => {
    setSyncingId(id)
    try {
      const res = await api.openapi.validateOpenAPISpec(id)
      if (res.status === 'success') {
        toast.success(`Validation passed — ${res.operations_count ?? 0} operations found`)
        const refreshed = await api.openapi.getOpenAPISpecs()
        if (Array.isArray(refreshed)) setSpecs(refreshed)
      } else {
        toast.error(res.error || 'Validation failed')
      }
    } catch (e) {
      logger.error('Sync failed:', e)
      toast.error('Sync failed')
    } finally {
      setSyncingId(null)
    }
  }, [])

  const toggleSpec = useCallback(async (spec: OpenAPISpec) => {
    const newEnabled = !spec.enabled
    setSpecs((prev) => prev.map((s) => (s.id === spec.id ? { ...s, enabled: newEnabled } : s)))
    try {
      await api.openapi.toggleOpenAPISpec(spec.id)
      toast.success(newEnabled ? 'Spec enabled' : 'Spec disabled')
    } catch (e) {
      setSpecs((prev) => prev.map((s) => (s.id === spec.id ? { ...s, enabled: !newEnabled } : s)))
      const errorMsg = e instanceof Error ? e.message : 'Failed to toggle spec'
      toast.error(errorMsg)
    }
  }, [])

  const filtered = specs.filter((s) => {
    if (!search) return true
    const q = search.toLowerCase()
    return s.name.toLowerCase().includes(q) || s.spec_url.toLowerCase().includes(q)
  })

  const totalOperations = specs.reduce((acc, s) => acc + (s.operations?.length || 0), 0)
  const stats = {
    total: specs.length,
    enabled: specs.filter((s) => s.enabled).length,
    disabled: specs.filter((s) => !s.enabled).length,
    operations: totalOperations,
  }

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard title="Total Specs" value={stats.total} />
        <StatCard title="Active Specs" value={stats.enabled} />
        <StatCard title="Disabled Specs" value={stats.disabled} />
        <StatCard title="Available Operations" value={stats.operations} />
      </div>

      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-surface-900">OpenAPI Specs</h2>
        <div className="flex items-center gap-3">
          <FeatureStatus
            type="toggle"
            enabled={openApiFeatureEnabled}
            onToggle={handleToggleOpenApiFeature}
            disabled={togglingOpenApiFeature}
          />
          <Button onClick={() => setShowAddModal(true)}>
            <Plus size={16} />
            Add Spec
          </Button>
        </div>
      </div>

      <FilterBar searchPlaceholder="Search by name or spec URL..." searchValue={search} onSearchChange={setSearch} />

      {loading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="rounded-xl border border-surface-200 bg-white p-6 shadow-card">
              <Skeleton className="h-4 w-1/3 mb-4" />
              <Skeleton className="h-8 w-1/2 mb-3" />
              <Skeleton className="h-3 w-2/3 mb-2" />
              <Skeleton className="h-3 w-1/2 mb-2" />
              <Skeleton className="h-3 w-3/4" />
            </div>
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState
          title={search ? 'No matching specs' : 'No OpenAPI specs configured'}
          description={
            search ? 'Try different search terms.' : 'Add your first spec to start exposing OpenAPI operations.'
          }
          action={!search ? { label: 'Add Spec', onClick: () => setShowAddModal(true) } : undefined}
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {filtered.map((spec) => (
            <Card key={spec.id}>
              <div className="cursor-pointer" onClick={() => onNavigate?.(`/openapi/specs/${spec.id}`)}>
                <CardHeader>
                  <div className="flex items-start justify-between">
                    <div className="flex-1 min-w-0">
                      <div className="flex items-center gap-2 flex-wrap">
                        <span
                          className={cn(
                            'inline-block h-2.5 w-2.5 rounded-full',
                            spec.enabled ? statusConfig.online.dot : statusConfig.offline.dot,
                          )}
                        />
                        <Globe size={16} className="text-surface-400 shrink-0" />
                        <CardTitle className="text-base truncate">{spec.name}</CardTitle>
                      </div>

                      {spec.description && (
                        <p className="mt-1 text-xs text-surface-600 line-clamp-2">{spec.description}</p>
                      )}
                      <CardDescription className="mt-1 font-mono text-xs truncate">{spec.spec_url}</CardDescription>
                    </div>
                    <div className="flex items-center gap-2 shrink-0 ml-3" onClick={(e) => e.stopPropagation()}>
                      <Switch size="sm" checked={spec.enabled} onCheckedChange={() => toggleSpec(spec)} />
                    </div>
                  </div>
                </CardHeader>

                <CardContent>
                  {spec.operations && spec.operations.length > 0 && (
                    <div className="flex items-center gap-4 text-xs text-surface-500 mb-4">
                      <span className="inline-flex items-center gap-1">
                        {spec.operations.length} operation{spec.operations.length !== 1 ? 's' : ''}
                      </span>
                    </div>
                  )}
                  <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => testSpec(spec.id)}
                      disabled={syncingId === spec.id}
                    >
                      {syncingId === spec.id ? <Loader2 size={14} className="animate-spin" /> : <RefreshCw size={14} />}
                      Sync
                    </Button>
                    <Button variant="outline" size="sm" onClick={() => setEditSpecId(spec.id)}>
                      <Edit3 size={14} />
                      Edit
                    </Button>
                    <Button
                      variant="destructive"
                      size="sm"
                      onClick={() => {
                        if (window.confirm(`Remove "${spec.name}"? This will make its operations unavailable.`)) {
                          deleteSpec(spec.id)
                        }
                      }}
                    >
                      <Trash2 size={14} />
                    </Button>
                  </div>
                </CardContent>
              </div>
            </Card>
          ))}
        </div>
      )}

      <SpecFormModal open={showAddModal} onClose={() => setShowAddModal(false)} onSave={addSpec} />
      {editing && (
        <SpecFormModal open={!!editing} onClose={() => setEditSpecId(null)} initial={editing} onSave={updateSpec} />
      )}
    </div>
  )
}
