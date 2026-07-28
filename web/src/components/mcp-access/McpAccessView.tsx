import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Fragment, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { type APIKey, api, type Group, type MCPServer, type McpGrant, type User } from '../../lib/api'
import { logger } from '../../lib/logger'
import { qk } from '../../lib/query'
import { cn } from '../../lib/utils'
import { EffectBadge, TypeBadge } from '../ui/badge'
import { Button } from '../ui/button'
import { Card } from '../ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '../ui/dialog'
import { EmptyState } from '../ui/empty-state'
import { AlertTriangle, ChevronDown, ChevronRight, Globe, Plus, RefreshCw, Search, Trash2 } from '../ui/icons'
import { Input } from '../ui/input'
import { QueryProvider } from '../ui/query-provider'
import { Skeleton } from '../ui/skeleton'
import { Switch } from '../ui/switch'

const PAGE_SIZE = 20

const SUBJECT_TYPE_OPTIONS = [
  { label: 'All Types', value: '' },
  { label: 'Key', value: 'key' },
  { label: 'User', value: 'user' },
  { label: 'Group', value: 'group' },
] as const

const EFFECT_OPTIONS = [
  { label: 'All Effects', value: '' },
  { label: 'Allow', value: 'allow' },
  { label: 'Deny', value: 'deny' },
] as const

function McpAccessViewContent() {
  const [search, setSearch] = useState('')
  const [filterSubjectType, setFilterSubjectType] = useState('')
  const [filterEffect, setFilterEffect] = useState('')
  const [page, setPage] = useState(0)
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [createDialogOpen, setCreateDialogOpen] = useState(false)
  const [expandedSubjects, setExpandedSubjects] = useState<Set<string>>(new Set(['__all__']))

  const [formSubjectType, setFormSubjectType] = useState<'key' | 'user' | 'group'>('key')
  const [formSubjectId, setFormSubjectId] = useState('')
  const [formServerId, setFormServerId] = useState('*')
  const [formTools, setFormTools] = useState('*')
  const [formEffect, setFormEffect] = useState<'allow' | 'deny'>('allow')

  const queryClient = useQueryClient()

  const {
    data: grants = [],
    isLoading,
    error,
    refetch,
    isFetching,
  } = useQuery({
    queryKey: qk.access.mcp.all,
    queryFn: () => api.access.listGrants().catch(() => [] as McpGrant[]),
  })

  const { data: servers = [] } = useQuery({
    queryKey: ['mcp-servers-all'],
    queryFn: () => api.mcp.getMCPServers().catch(() => [] as MCPServer[]),
  })

  const { data: users = [] } = useQuery({
    queryKey: ['mcp-access', 'users'],
    queryFn: () => api.users.getUsers().catch(() => [] as User[]),
  })
  const { data: groups = [] } = useQuery({
    queryKey: ['mcp-access', 'groups'],
    queryFn: () => api.groups.getGroups().catch(() => [] as Group[]),
  })
  const { data: apiKeys = [] } = useQuery({
    queryKey: ['mcp-access', 'api-keys'],
    queryFn: () => api.apiKeys.getAPIKeys().catch(() => [] as APIKey[]),
  })

  const subjectLabel = (type: string, id: string): string | null => {
    if (id === '*') return null
    if (type === 'user') return users.find((u) => String(u.id) === id)?.name ?? null
    if (type === 'group') return groups.find((g) => String(g.id) === id)?.name ?? null
    if (type === 'key') return apiKeys.find((k) => k.id === id)?.name ?? null
    return null
  }

  const { data: defaultPolicy, refetch: refetchPolicy } = useQuery({
    queryKey: qk.access.mcp.defaultPolicy,
    queryFn: () => api.access.getDefaultPolicy().catch(() => 'deny'),
  })

  const toggleDefaultPolicy = async () => {
    const newPolicy = defaultPolicy === 'allow' ? 'deny' : 'allow'
    try {
      await api.access.setDefaultPolicy(newPolicy)
      toast.success(`Default policy changed to ${newPolicy}`)
      refetchPolicy()
    } catch (err) {
      logger.error('Failed to update default policy', err)
      toast.error('Failed to update default policy')
    }
  }

  const serverIds = useMemo(() => new Set(servers.map((s) => s.id)), [servers])
  const isZombie = (g: McpGrant) => g.server_id !== '*' && !serverIds.has(g.server_id)

  const filtered = useMemo(() => {
    let result = grants
    if (search) {
      const q = search.toLowerCase()
      result = result.filter(
        (g) =>
          (g.server_name || g.server_id).toLowerCase().includes(q) ||
          g.subject_id.toLowerCase().includes(q) ||
          g.tools.toLowerCase().includes(q),
      )
    }
    if (filterSubjectType) result = result.filter((g) => g.subject_type === filterSubjectType)
    if (filterEffect) result = result.filter((g) => g.effect === filterEffect)
    return result
  }, [grants, search, filterSubjectType, filterEffect])

  const grouped = useMemo(() => {
    const bySubject = new Map<string, McpGrant[]>()
    for (const g of filtered) {
      const key = `${g.subject_type}:${g.subject_id}`
      if (!bySubject.has(key)) bySubject.set(key, [])
      bySubject.get(key)?.push(g)
    }
    return Array.from(bySubject.entries()).sort(([a], [b]) => a.localeCompare(b))
  }, [filtered])

  const totalPages = Math.max(1, Math.ceil(grouped.length / PAGE_SIZE))
  const paged = grouped.slice(page * PAGE_SIZE, (page + 1) * PAGE_SIZE)
  const allSelected = paged.length > 0 && paged.every(([, gs]) => gs.every((g) => selected.has(g.id)))

  const toggleAll = () => {
    const allOnPage = paged.flatMap(([, gs]) => gs.map((g) => g.id))
    if (allSelected) setSelected(new Set())
    else setSelected(new Set(allOnPage))
  }

  const toggleOne = (id: string) => {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const handleToggle = async (grant: McpGrant) => {
    try {
      await api.access.toggleGrant(grant.id)
      toast.success(grant.enabled ? 'Grant disabled' : 'Grant enabled')
      refetch()
    } catch (err) {
      logger.error('Failed to toggle grant', err)
      toast.error('Failed to toggle grant')
    }
  }

  const handleDeleteOne = (grant: McpGrant) => {
    setSelected(new Set([grant.id]))
    setDeleteDialogOpen(true)
  }

  const handleBatchDelete = async () => {
    try {
      await Promise.all([...selected].map((id) => api.access.deleteGrant(id)))
      toast.warning(`${selected.size} grant${selected.size !== 1 ? 's' : ''} removed`)
      setSelected(new Set())
      setDeleteDialogOpen(false)
      refetch()
    } catch (err) {
      logger.error('Failed to remove some grants', err)
      toast.error('Failed to remove some grants')
    }
  }

  const createMutation = useMutation({
    mutationFn: () =>
      api.access.createGrant({
        subject_type: formSubjectType,
        subject_id: formSubjectId,
        server_id: formServerId,
        tools: formTools,
        effect: formEffect,
      }),
    onSuccess: () => {
      toast.success('Grant created')
      setCreateDialogOpen(false)
      setFormSubjectType('key')
      setFormSubjectId('')
      setFormServerId('*')
      setFormTools('*')
      setFormEffect('allow')
      queryClient.invalidateQueries({ queryKey: qk.access.mcp.all })
    },
    onError: (e: Error) => {
      toast.error(e.message || 'Failed to create grant')
    },
  })

  const toggleGroup = (key: string) => {
    setExpandedSubjects((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  const renderGrantRow = (grant: McpGrant) => (
    <tr
      key={grant.id}
      className={cn('transition-colors hover:bg-surface-50', selected.has(grant.id) && 'bg-brand-50/40')}
    >
      <td className="px-4 py-3">
        <input
          type="checkbox"
          checked={selected.has(grant.id)}
          onChange={() => toggleOne(grant.id)}
          className="rounded border-surface-300 text-brand-600 focus:ring-brand-500"
        />
      </td>
      <td className="px-4 py-3 text-sm font-medium text-surface-900 whitespace-nowrap">
        {grant.server_id === '*' ? (
          <span className="flex items-center gap-1">
            <Globe size={12} className="text-surface-400" />
            All Servers
          </span>
        ) : (
          <span className="flex items-center gap-1">
            {grant.server_name || grant.server_id}
            {isZombie(grant) && (
              <span
                title="Server no longer exists"
                className="inline-flex items-center rounded bg-amber-100 px-1 py-0.5 text-[9px] font-medium text-amber-700 ml-1"
              >
                zombie
              </span>
            )}
          </span>
        )}
      </td>
      <td className="px-4 py-3">
        <EffectBadge effect={grant.effect} />
      </td>
      <td className="px-4 py-3 font-mono text-xs text-surface-600 max-w-[140px] truncate" title={grant.tools}>
        {grant.tools}
      </td>
      <td className="px-4 py-3 text-xs text-surface-400 whitespace-nowrap">
        {new Date(grant.created_at).toLocaleDateString()}
      </td>
      <td className="px-4 py-3">
        <Switch size="sm" checked={grant.enabled} onCheckedChange={() => handleToggle(grant)} />
      </td>
      <td className="px-4 py-3">
        <button
          onClick={() => handleDeleteOne(grant)}
          className="text-surface-400 hover:text-destructive transition-colors p-1"
          title="Delete grant"
        >
          <Trash2 size={14} />
        </button>
      </td>
    </tr>
  )

  return (
    <div className="space-y-4">
      <div className="flex items-start justify-between">
        <div>
          <h2 className="text-lg font-semibold text-surface-900">MCP Access Grants</h2>
          <p className="text-sm text-surface-500 mt-0.5">Manage tool-level access permissions for MCP servers</p>
        </div>
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-3 rounded-lg border border-surface-200 bg-surface-50 px-3 py-2 text-sm">
            <span className="text-surface-600">Default policy:</span>
            <span
              className={cn(
                'inline-flex items-center rounded px-1.5 py-0.5 text-xs font-semibold',
                defaultPolicy === 'allow' ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700',
              )}
            >
              {defaultPolicy || 'deny'}
            </span>
            <button
              onClick={toggleDefaultPolicy}
              className="text-xs text-brand-600 hover:text-brand-700 font-medium underline-offset-2 hover:underline transition-colors"
            >
              Switch to {defaultPolicy === 'allow' ? 'deny' : 'allow'}
            </button>
          </div>
          <div className="flex items-center gap-2">
            <Button size="sm" onClick={() => setCreateDialogOpen(true)}>
              <Plus size={14} />
              Add Grant
            </Button>
            {selected.size > 0 && (
              <Button variant="destructive" size="sm" onClick={() => setDeleteDialogOpen(true)}>
                <Trash2 size={14} />
                Delete {selected.size}
              </Button>
            )}
            <Button variant="outline" size="sm" onClick={() => refetch()} disabled={isFetching}>
              <RefreshCw size={14} className={cn(isFetching && 'animate-spin')} />
              Refresh
            </Button>
          </div>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <div className="relative flex-1 min-w-[200px] max-w-sm">
          <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-surface-400" />
          <Input
            type="text"
            placeholder="Search server, subject, tools..."
            value={search}
            onChange={(e) => {
              setSearch(e.target.value)
              setPage(0)
            }}
            className="pl-10"
          />
        </div>
        <select
          value={filterSubjectType}
          onChange={(e) => {
            setFilterSubjectType(e.target.value)
            setPage(0)
          }}
          className="h-8 min-w-[120px] rounded-lg border border-input bg-transparent px-2.5 py-1 text-sm text-surface-900 focus:border-ring focus:ring-3 focus:ring-ring/50"
        >
          {SUBJECT_TYPE_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <select
          value={filterEffect}
          onChange={(e) => {
            setFilterEffect(e.target.value)
            setPage(0)
          }}
          className="h-8 min-w-[120px] rounded-lg border border-input bg-transparent px-2.5 py-1 text-sm text-surface-900 focus:border-ring focus:ring-3 focus:ring-ring/50"
        >
          {EFFECT_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      </div>

      {error ? (
        <Card>
          <div className="p-6 text-center">
            <AlertTriangle size={32} strokeWidth={1.5} className="mx-auto text-error" />
            <h3 className="mt-3 text-sm font-medium text-surface-700">Failed to load grants</h3>
            <p className="mt-1 text-xs text-surface-400">
              {error instanceof Error ? error.message : 'An error occurred'}
            </p>
            <Button variant="outline" size="sm" className="mt-3" onClick={() => refetch()}>
              Retry
            </Button>
          </div>
        </Card>
      ) : isLoading ? (
        <Card>
          <div className="rounded-xl border border-surface-200 bg-white shadow-card">
            <div className="flex border-b border-surface-200 px-4 py-3 gap-4">
              {Array.from({ length: 8 }).map((_, ci) => (
                <Skeleton key={`h-${ci}`} className={cn('h-3', ci === 0 ? 'w-8' : 'w-1/6')} />
              ))}
            </div>
            {Array.from({ length: 5 }).map((_, ri) => (
              <div key={`r-${ri}`} className="flex border-b border-surface-100 px-4 py-3.5 gap-4 last:border-b-0">
                {Array.from({ length: 8 }).map((_, ci) => (
                  <Skeleton key={`c-${ri}-${ci}`} className={cn('h-3', ci === 0 ? 'w-8' : 'w-1/6')} />
                ))}
              </div>
            ))}
          </div>
        </Card>
      ) : paged.length === 0 && grouped.length === 0 ? (
        <Card>
          <EmptyState
            title={search || filterSubjectType || filterEffect ? 'No matching grants' : 'No grants configured'}
            description={
              search || filterSubjectType || filterEffect
                ? 'Try adjusting your filters.'
                : 'Grant rules control which subjects can access which MCP tools. Click "Add Grant" to create one.'
            }
          />
        </Card>
      ) : (
        <Card>
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b border-surface-200">
                  <th className="px-4 py-3 w-10">
                    <input
                      type="checkbox"
                      checked={allSelected}
                      onChange={toggleAll}
                      className="rounded border-surface-300 text-brand-600 focus:ring-brand-500"
                    />
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-surface-500">
                    Subject
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-surface-500">
                    Effect
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-surface-500">
                    Server / Tools
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-surface-500">
                    Created
                  </th>
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-surface-500">
                    Status
                  </th>
                  <th className="px-4 py-3 w-16" />
                </tr>
              </thead>
              <tbody className="divide-y divide-surface-100">
                {paged.map(([subjectKey, subjectGrants]) => {
                  const expanded = expandedSubjects.has(subjectKey) || expandedSubjects.has('__all__')
                  const [type, ...idParts] = subjectKey.split(':')
                  const subjectId = idParts.join(':')
                  const grantsInGroupSelected = subjectGrants.every((g) => selected.has(g.id))

                  return (
                    <Fragment key={subjectKey}>
                      <tr
                        className="bg-surface-50 cursor-pointer hover:bg-surface-100 transition-colors"
                        onClick={() => toggleGroup(subjectKey)}
                      >
                        <td className="px-4 py-2">
                          <input
                            type="checkbox"
                            checked={grantsInGroupSelected}
                            onChange={(e) => {
                              e.stopPropagation()
                              if (grantsInGroupSelected) {
                                setSelected((prev) => {
                                  const next = new Set(prev)
                                  subjectGrants.forEach((g) => next.delete(g.id))
                                  return next
                                })
                              } else {
                                setSelected((prev) => {
                                  const next = new Set(prev)
                                  subjectGrants.forEach((g) => next.add(g.id))
                                  return next
                                })
                              }
                            }}
                            className="rounded border-surface-300 text-brand-600 focus:ring-brand-500"
                          />
                        </td>
                        <td className="px-4 py-2" colSpan={6}>
                          <div className="flex items-center gap-3">
                            {expanded ? (
                              <ChevronDown size={14} className="text-surface-400" />
                            ) : (
                              <ChevronRight size={14} className="text-surface-400" />
                            )}
                            <TypeBadge type={type} />
                            <span className="font-mono text-sm font-medium text-surface-900">{subjectId}</span>
                            {subjectLabel(type, subjectId) && (
                              <span className="text-sm text-surface-500">{subjectLabel(type, subjectId)}</span>
                            )}
                            <span className="text-xs text-surface-400">
                              ({subjectGrants.length} rule{subjectGrants.length !== 1 ? 's' : ''})
                            </span>
                          </div>
                        </td>
                      </tr>
                      {expanded && subjectGrants.map(renderGrantRow)}
                    </Fragment>
                  )
                })}
              </tbody>
            </table>
          </div>

          {totalPages > 1 && (
            <div className="flex items-center justify-between border-t border-surface-200 px-4 py-3">
              <span className="text-sm text-surface-500">
                {grouped.length} subject{grouped.length !== 1 ? 's' : ''} ({filtered.length} rules)
                {totalPages > 1 && ` — page ${page + 1} of ${totalPages}`}
              </span>
              <div className="flex items-center gap-2">
                <button
                  disabled={page === 0}
                  onClick={() => setPage(page - 1)}
                  className="rounded-lg border border-surface-200 bg-white px-3 py-1.5 text-sm text-surface-700 hover:bg-surface-50 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                >
                  Previous
                </button>
                <button
                  disabled={page >= totalPages - 1}
                  onClick={() => setPage(page + 1)}
                  className="rounded-lg border border-surface-200 bg-white px-3 py-1.5 text-sm text-surface-700 hover:bg-surface-50 disabled:opacity-40 disabled:cursor-not-allowed transition-colors"
                >
                  Next
                </button>
              </div>
            </div>
          )}
        </Card>
      )}

      <Dialog open={createDialogOpen} onOpenChange={setCreateDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Create Access Grant</DialogTitle>
            <DialogDescription>Define which tools a subject can access on an MCP server.</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-2">
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-surface-700">Subject Type</label>
              <select
                value={formSubjectType}
                onChange={(e) => {
                  setFormSubjectType(e.target.value as 'key' | 'user' | 'group')
                  setFormSubjectId('')
                }}
                className="w-full h-9 rounded-lg border border-input bg-transparent px-3 text-sm text-surface-900 focus:border-ring focus:ring-3 focus:ring-ring/50"
              >
                <option value="key">Key</option>
                <option value="user">User</option>
                <option value="group">Group</option>
              </select>
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-surface-700">
                {formSubjectType === 'key' ? 'API Key' : formSubjectType === 'user' ? 'User' : 'Group'}
              </label>
              <select
                value={formSubjectId}
                onChange={(e) => setFormSubjectId(e.target.value)}
                className="w-full h-9 rounded-lg border border-input bg-transparent px-3 text-sm text-surface-900 focus:border-ring focus:ring-3 focus:ring-ring/50"
              >
                <option value="">Select...</option>
                <option value="*">
                  All {formSubjectType === 'key' ? 'Keys' : formSubjectType === 'user' ? 'Users' : 'Groups'}
                </option>
                {formSubjectType === 'key' &&
                  apiKeys.map((k) => (
                    <option key={k.id} value={k.id}>
                      {k.name}
                    </option>
                  ))}
                {formSubjectType === 'user' &&
                  users.map((u) => (
                    <option key={u.id} value={u.id}>
                      {u.name}
                      {u.email ? ` (${u.email})` : ''}
                    </option>
                  ))}
                {formSubjectType === 'group' &&
                  groups.map((g) => (
                    <option key={g.id} value={g.id}>
                      {g.name}
                    </option>
                  ))}
              </select>
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-surface-700">Server</label>
              <div className="flex gap-2">
                <Input
                  value={formServerId}
                  onChange={(e) => setFormServerId(e.target.value)}
                  placeholder="Server ID or * for all"
                  className="font-mono text-xs"
                />
                {servers.length > 0 && (
                  <select
                    value={servers.some((s) => s.id === formServerId) ? formServerId : ''}
                    onChange={(e) => {
                      if (e.target.value) setFormServerId(e.target.value)
                    }}
                    className="h-9 rounded-lg border border-input bg-transparent px-2 text-sm text-surface-900 focus:border-ring focus:ring-3 focus:ring-ring/50"
                  >
                    <option value="">Pick...</option>
                    {servers.map((s) => (
                      <option key={s.id} value={s.id}>
                        {s.name}
                      </option>
                    ))}
                  </select>
                )}
              </div>
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-surface-700">
                Tools <span className="text-surface-400 font-normal">(comma-separated, * for all)</span>
              </label>
              <Input
                value={formTools}
                onChange={(e) => setFormTools(e.target.value)}
                placeholder="*"
                className="font-mono text-xs"
              />
            </div>
            <div className="space-y-1.5">
              <label className="text-xs font-medium text-surface-700">Effect</label>
              <div className="flex gap-3">
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="radio"
                    name="effect"
                    value="allow"
                    checked={formEffect === 'allow'}
                    onChange={() => setFormEffect('allow')}
                    className="text-green-600 focus:ring-green-500"
                  />
                  <span className="text-sm text-surface-700">Allow</span>
                </label>
                <label className="flex items-center gap-2 cursor-pointer">
                  <input
                    type="radio"
                    name="effect"
                    value="deny"
                    checked={formEffect === 'deny'}
                    onChange={() => setFormEffect('deny')}
                    className="text-red-600 focus:ring-red-500"
                  />
                  <span className="text-sm text-surface-700">Deny</span>
                </label>
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateDialogOpen(false)}>
              Cancel
            </Button>
            <Button
              onClick={() => createMutation.mutate()}
              disabled={createMutation.isPending || !formSubjectId.trim()}
            >
              {createMutation.isPending ? 'Creating...' : 'Create Grant'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <div className="flex items-center gap-3">
              <div className="rounded-full bg-error/10 p-2 text-error">
                <AlertTriangle size={20} />
              </div>
              <div>
                <DialogTitle>
                  Remove {selected.size} Grant{selected.size !== 1 ? 's' : ''}
                </DialogTitle>
                <DialogDescription>
                  This action cannot be undone. The selected subjects will lose access to the configured tools.
                </DialogDescription>
              </div>
            </div>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteDialogOpen(false)}>
              Cancel
            </Button>
            <Button variant="destructive" onClick={handleBatchDelete}>
              Yes, Remove
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

export function McpAccessView() {
  return (
    <QueryProvider>
      <McpAccessViewContent />
    </QueryProvider>
  )
}
