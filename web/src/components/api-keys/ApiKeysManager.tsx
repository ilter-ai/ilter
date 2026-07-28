import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import { type APIKey, api, type Group, type User } from '../../lib/api'
import { qk } from '../../lib/query'
import { useApiMutation } from '../../lib/useApiMutation'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { Card, CardContent } from '../ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '../ui/dialog'
import { EmptyState } from '../ui/empty-state'
import { FilterBar } from '../ui/FilterBar'
import { AlertTriangle, CheckCircle, Copy, Download, Key, Plus, X } from '../ui/icons'
import { MultiSelect } from '../ui/MultiSelect'
import { QueryProvider } from '../ui/query-provider'
import { StatCard } from '../ui/StatCard'
import { Skeleton } from '../ui/skeleton'
import { useExport } from '../ui/useExport'
import { GroupField, UserField } from './GroupUserFields'

function ApiKeysManagerContent() {
  const { exportCsv } = useExport()
  const [search, setSearch] = useState('')
  const [showCreateForm, setShowCreateForm] = useState(false)
  const [createdKey, setCreatedKey] = useState<string | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<APIKey | null>(null)
  const [editTarget, setEditTarget] = useState<APIKey | null>(null)
  const [groupFilter, setGroupFilter] = useState<number | undefined>(undefined)
  const [userFilter, setUserFilter] = useState<number | undefined>(undefined)

  const {
    data: keys = [],
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: qk.apiKeys,
    queryFn: () => api.apiKeys.getAPIKeys().catch(() => [] as APIKey[]),
  })

  const { data: groups = [] } = useQuery({
    queryKey: ['api-keys', 'groups'] as const,
    queryFn: () => api.groups.getGroups().catch(() => [] as Group[]),
  })

  const { data: users = [] } = useQuery({
    queryKey: ['api-keys', 'users'] as const,
    queryFn: () => api.users.getUsers().catch(() => [] as User[]),
  })

  const { data: modelProviders = [] } = useQuery({
    queryKey: ['api-keys', 'model-providers'] as const,
    queryFn: () => api.models.getModelProviders().catch(() => []),
  })
  const { data: rawProviders = [] } = useQuery({
    queryKey: ['api-keys', 'providers'] as const,
    queryFn: () => api.providers.getProviders().catch(() => []),
  })

  const modelOptions = modelProviders.map((m) => ({
    value: m.model,
    label: m.name,
    provider: m.provider,
    tier: m.tier,
  }))
  const providerOptions = rawProviders.map((p) => ({ value: p.name, label: p.name }))

  const toggleKey = useApiMutation(
    ({ id, enabled: newEnabled }: { id: string; enabled: boolean }) =>
      api.apiKeys.updateAPIKey(id, { enabled: newEnabled }),
    { invalidate: [qk.apiKeys] },
  )

  const deleteKey = useApiMutation((id: string) => api.apiKeys.deleteAPIKey(id), { invalidate: [qk.apiKeys] })

  const updateKey = useApiMutation(
    ({
      id,
      data,
    }: {
      id: string
      data: Partial<Omit<APIKey, 'group_id' | 'user_id'>> & { group_id?: number | null; user_id?: number | null }
    }) => api.apiKeys.updateAPIKey(id, data),
    { invalidate: [qk.apiKeys] },
  )

  const filteredKeys = keys.filter((k) => {
    if (search && !k.name.toLowerCase().includes(search.toLowerCase())) return false
    if (groupFilter !== undefined && k.group_id !== groupFilter) return false
    if (userFilter !== undefined && k.user_id !== userFilter) return false
    return true
  })

  const summary = {
    total: keys.length,
    enabled: keys.filter((k) => k.enabled).length,
  }

  const handleToggleEnable = async (key: APIKey) => {
    try {
      await toggleKey.mutateAsync({ id: key.id, enabled: !key.enabled })
      toast.success(`Access key ${key.enabled ? 'disabled' : 'enabled'}`, { description: `"${key.name}"` })
    } catch (err) {
      toast.error('Failed to update key', { description: String(err) })
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      await deleteKey.mutateAsync(deleteTarget.id)
      setDeleteTarget(null)
      toast.warning('Access key deleted', { description: `"${deleteTarget.name}"` })
    } catch (err) {
      toast.error('Failed to delete key', { description: String(err) })
    }
  }

  const handleSaveEdit = async () => {
    if (!editTarget) return
    try {
      await updateKey.mutateAsync({
        id: editTarget.id,
        data: { ...editTarget, group_id: editTarget.group_id ?? null, user_id: editTarget.user_id ?? null },
      })
      setEditTarget(null)
      toast.success('Access key updated', { description: `"${editTarget.name}"` })
    } catch (err) {
      toast.error('Failed to update key', { description: String(err) })
    }
  }

  const handleCreated = () => {
    refetch()
    setShowCreateForm(false)
  }

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <StatCard title="Total Keys" value={summary.total} icon={<Key size={18} />} />
        <StatCard title="Enabled" value={summary.enabled} icon={<CheckCircle size={18} />} />
      </div>

      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3 flex-1 max-w-md">
          <FilterBar
            searchPlaceholder="Search by name..."
            searchValue={search}
            onSearchChange={setSearch}
            className="flex-1"
          />
          <select
            value={groupFilter ?? ''}
            onChange={(e) => setGroupFilter(e.target.value ? Number(e.target.value) : undefined)}
            className="rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
          >
            <option value="">All Groups</option>
            {groups.map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
          <select
            value={userFilter ?? ''}
            onChange={(e) => setUserFilter(e.target.value ? Number(e.target.value) : undefined)}
            className="rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
          >
            <option value="">All Users</option>
            {users.map((u) => (
              <option key={u.id} value={u.id}>
                {u.name}
                {u.email ? ` (${u.email})` : ''}
              </option>
            ))}
          </select>
        </div>
        <div className="flex-center-gap-2">
          {filteredKeys.length > 0 && (
            <Button
              variant="outline"
              size="sm"
              onClick={() =>
                exportCsv(
                  filteredKeys.map(({ name, group_id, user_id, enabled, allowed_models }) => {
                    const group = groups.find((g) => g.id === group_id)
                    const user = users.find((u) => u.id === user_id)
                    return {
                      Name: name,
                      Group: group?.name || '-',
                      User: user ? `${user.name}${user.email ? ` (${user.email})` : ''}` : '-',
                      Status: enabled ? 'Active' : 'Disabled',
                      'Allowed Models': allowed_models?.join(', ') || '-',
                    }
                  }),
                  [
                    { key: 'Name' as const, header: 'Name' },
                    { key: 'Group' as const, header: 'Group' },
                    { key: 'User' as const, header: 'User' },
                    { key: 'Status' as const, header: 'Status' },
                    { key: 'Allowed Models' as const, header: 'Allowed Models' },
                  ],
                  'api-keys.csv',
                )
              }
            >
              <Download size={14} />
              Export
            </Button>
          )}
          <Button onClick={() => setShowCreateForm(true)}>
            <Plus size={16} />
            Create API Key
          </Button>
        </div>
      </div>

      <Dialog
        open={showCreateForm}
        onOpenChange={(o) => {
          if (!o) setShowCreateForm(false)
        }}
      >
        <DialogContent className="max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Create API Key</DialogTitle>
            <DialogDescription>Generate a new API key with the following permissions.</DialogDescription>
          </DialogHeader>
          <CreateAPIKeyForm
            groups={groups}
            users={users}
            onClose={() => setShowCreateForm(false)}
            onCreated={handleCreated}
            onKeyCreated={setCreatedKey}
          />
        </DialogContent>
      </Dialog>

      {createdKey && (
        <Card className="border-warning/30 bg-warning/5">
          <CardContent className="p-4">
            <div className="flex items-start gap-3">
              <div className="rounded-full bg-warning/20 p-1.5 text-warning shrink-0 mt-0.5">
                <AlertTriangle size={16} />
              </div>
              <div className="flex-1 min-w-0">
                <p className="text-sm font-semibold text-warning mb-1">⚠️ Copy your access key now</p>
                <p className="text-xs text-surface-600 mb-2">
                  You won't be able to see it again after you dismiss this message.
                </p>
                <div className="flex-center-gap-2">
                  <code className="flex-1 rounded-lg bg-surface-900 px-3 py-2 text-sm font-mono text-green-400 break-all">
                    {createdKey}
                  </code>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => navigator.clipboard.writeText(createdKey)}
                    title="Copy to clipboard"
                  >
                    <Copy size={14} />
                  </Button>
                </div>
              </div>
              <button
                onClick={() => setCreatedKey(null)}
                className="shrink-0 rounded-lg p-1 text-surface-400 hover:bg-surface-100 hover:text-surface-600"
              >
                <X size={16} />
              </button>
            </div>
          </CardContent>
        </Card>
      )}

      <Card>
        {error ? (
          <div className="p-6 text-center">
            <h3 className="text-error font-medium">Failed to load access keys</h3>
            <p className="text-surface-500 text-sm mt-1">{error.message}</p>
            <Button variant="outline" size="sm" className="mt-3" onClick={() => refetch()}>
              Retry
            </Button>
          </div>
        ) : isLoading ? (
          <div className="rounded-xl border border-surface-200 bg-white shadow-card">
            <div className="flex border-b border-surface-200 px-4 py-3 gap-4">
              {Array.from({ length: 7 }).map((_, ci) => (
                <Skeleton key={`h-${ci}`} className={cn('h-3', ci === 0 ? 'w-1/4' : 'w-1/6')} />
              ))}
            </div>
            {Array.from({ length: 5 }).map((_, ri) => (
              <div key={`r-${ri}`} className="flex border-b border-surface-100 px-4 py-3.5 gap-4 last:border-b-0">
                {Array.from({ length: 7 }).map((_, ci) => (
                  <Skeleton key={`c-${ri}-${ci}`} className={cn('h-3', ci === 0 ? 'w-2/5' : 'w-1/5')} />
                ))}
              </div>
            ))}
          </div>
        ) : filteredKeys.length === 0 ? (
          <EmptyState
            title={search || groupFilter !== undefined ? 'No matching access keys' : 'No access keys yet'}
            description={
              search || groupFilter !== undefined
                ? 'Try a different search term or filter.'
                : 'Create your first access key to get started.'
            }
            action={!search ? { label: 'Create API Key', onClick: () => setShowCreateForm(true) } : undefined}
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b border-surface-200">
                  {['Name', 'Group', 'User', 'Allowed Models', 'Status', 'Actions'].map((h) => (
                    <th
                      key={h}
                      className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-surface-500"
                    >
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-surface-100">
                {filteredKeys.map((key) => (
                  <tr key={key.id} className="transition-colors hover:bg-surface-50">
                    <td className="px-4 py-3">
                      <p className="text-sm font-medium text-surface-900">{key.name}</p>
                    </td>
                    <td className="px-4 py-3 text-sm text-surface-600">
                      {key.group_id ? (
                        groups.find((g) => g.id === key.group_id)?.name || (
                          <span className="text-surface-400">Unknown</span>
                        )
                      ) : (
                        <span className="text-surface-400">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm text-surface-600">
                      {key.user_id ? (
                        users.find((u) => u.id === key.user_id)?.name || (
                          <span className="text-surface-400">Unknown</span>
                        )
                      ) : (
                        <span className="text-surface-400">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-sm text-surface-600 max-w-[200px] truncate">
                      {key.allowed_models?.length ? (
                        key.allowed_models.join(', ')
                      ) : (
                        <span className="text-surface-400">All</span>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      <span
                        className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${
                          key.enabled
                            ? 'bg-success/10 text-success border-success/20'
                            : 'bg-error/10 text-error border-error/20'
                        }`}
                      >
                        {key.enabled ? 'Active' : 'Disabled'}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-1">
                        <Button variant="outline" size="sm" onClick={() => handleToggleEnable(key)}>
                          {key.enabled ? 'Disable' : 'Enable'}
                        </Button>
                        <Button variant="outline" size="sm" onClick={() => setEditTarget(key)}>
                          Edit
                        </Button>
                        <Button variant="destructive" size="sm" onClick={() => setDeleteTarget(key)}>
                          Delete
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </Card>

      {deleteTarget && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          onClick={() => setDeleteTarget(null)}
        >
          <div className="w-full max-w-md rounded-xl bg-white p-6 shadow-xl" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center gap-3 mb-4">
              <div className="rounded-full bg-error/10 p-2 text-error">
                <AlertTriangle size={20} />
              </div>
              <div>
                <h3 className="text-lg font-semibold text-surface-900">Delete Access Key</h3>
                <p className="text-sm text-surface-500 mt-0.5">
                  Are you sure you want to delete <strong className="text-surface-700">{deleteTarget.name}</strong>?
                </p>
              </div>
            </div>

            <p className="text-sm text-surface-600 bg-surface-50 rounded-lg p-3 mb-4 border border-surface-200">
              This action cannot be undone. Any services using this key will immediately lose access.
            </p>

            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setDeleteTarget(null)}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={handleDelete}>
                Yes, Delete Key
              </Button>
            </div>
          </div>
        </div>
      )}

      {editTarget && (
        <Dialog open onOpenChange={() => setEditTarget(null)}>
          <DialogContent className="max-h-[85vh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle>Edit Access Key</DialogTitle>
              <DialogDescription>Update permissions for "{editTarget.name}".</DialogDescription>
            </DialogHeader>
            <div className="space-y-4 py-2">
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-surface-700">Key Name</label>
                <input
                  type="text"
                  value={editTarget.name}
                  onChange={(e) => setEditTarget({ ...editTarget, name: e.target.value })}
                  className="w-full h-9 rounded-lg border border-input bg-transparent px-3 text-sm text-surface-900 focus:border-ring focus:ring-3 focus:ring-ring/50"
                />
              </div>
              <div className="grid grid-cols-2 gap-3">
                <GroupField
                  groups={groups}
                  groupId={editTarget.group_id}
                  onChange={(id) => setEditTarget({ ...editTarget, group_id: id })}
                />
                <UserField
                  users={users}
                  userId={editTarget.user_id}
                  onChange={(id) => setEditTarget({ ...editTarget, user_id: id })}
                />
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-surface-700">Allowed Models</label>
                <MultiSelect
                  options={modelOptions}
                  value={editTarget.allowed_models?.join(',') || ''}
                  onChange={(csv) => setEditTarget({ ...editTarget, allowed_models: csv ? csv.split(',') : [] })}
                  placeholder="All models"
                  groupByProvider
                />
              </div>
              <div className="space-y-1.5">
                <label className="text-xs font-medium text-surface-700">Allowed Providers</label>
                <MultiSelect
                  options={providerOptions}
                  value={editTarget.allowed_providers?.join(',') || ''}
                  onChange={(csv) => setEditTarget({ ...editTarget, allowed_providers: csv ? csv.split(',') : [] })}
                  placeholder="All providers"
                />
              </div>
            </div>
            <div className="flex justify-end gap-2 pt-2">
              <Button variant="outline" onClick={() => setEditTarget(null)}>
                Cancel
              </Button>
              <Button onClick={handleSaveEdit} disabled={!editTarget.name.trim()}>
                Save Changes
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      )}
    </div>
  )
}

export function ApiKeysManager() {
  return (
    <QueryProvider>
      <ApiKeysManagerContent />
    </QueryProvider>
  )
}

function CreateAPIKeyForm({
  onClose,
  onCreated,
  onKeyCreated,
  groups,
  users,
}: {
  onClose: () => void
  onCreated: (key: APIKey) => void
  onKeyCreated: (rawKey: string) => void
  groups: Group[]
  users: User[]
}) {
  const [name, setName] = useState('')
  const [groupId, setGroupId] = useState<number | undefined>(undefined)
  const [userId, setUserId] = useState<number | undefined>(undefined)
  const [allowedModels, setAllowedModels] = useState('')
  const [allowedProviders, setAllowedProviders] = useState('')
  const [saving, setSaving] = useState(false)

  const handleSubmit = async () => {
    if (!name.trim()) return
    setSaving(true)
    try {
      const result = await api.apiKeys.createAPIKey({
        name: name.trim(),
        group_id: groupId,
        user_id: userId,
        allowed_models: allowedModels
          ? allowedModels
              .split(',')
              .map((s) => s.trim())
              .filter(Boolean)
          : undefined,
        allowed_providers: allowedProviders
          ? allowedProviders
              .split(',')
              .map((s) => s.trim())
              .filter(Boolean)
          : undefined,
      })

      const newKey: APIKey = {
        id: result.id,
        name: result.name,
        group_id: groupId,
        user_id: userId,
        tags: {},
        rate_limit_rpm: 0,
        rate_limit_tpm: 0,
        allowed_models: allowedModels
          ? allowedModels
              .split(',')
              .map((s) => s.trim())
              .filter(Boolean)
          : [],
        allowed_providers: allowedProviders
          ? allowedProviders
              .split(',')
              .map((s) => s.trim())
              .filter(Boolean)
          : [],
        enabled: true,
        created_at: new Date().toISOString(),
        updated_at: new Date().toISOString(),
      }

      onKeyCreated(result.key)
      onCreated(newKey)
    } finally {
      setSaving(false)
    }
  }

  const { data: modelProviders = [] } = useQuery({
    queryKey: ['models', 'all'],
    queryFn: () => api.models.getModelProviders().catch(() => []),
  })
  const { data: providers = [] } = useQuery({
    queryKey: ['providers'],
    queryFn: () => api.providers.getProviders().catch(() => []),
  })

  const modelOptions = modelProviders.map((m) => ({
    value: m.model,
    label: m.name,
    provider: m.provider,
    tier: m.tier,
  }))
  const providerOptions = providers.map((p) => ({ value: p.name, label: p.name }))

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label className="block text-xs font-medium text-surface-500 mb-1">Key Name *</label>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. production-api-key"
            className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
          />
        </div>
        <GroupField groups={groups} groupId={groupId} onChange={setGroupId} />
      </div>

      <UserField users={users} userId={userId} onChange={setUserId} />

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
        <div>
          <label className="block text-xs font-medium text-surface-500 mb-1">Allowed Models</label>
          <MultiSelect
            options={modelOptions}
            value={allowedModels}
            onChange={setAllowedModels}
            placeholder="All models"
            groupByProvider
          />
        </div>
        <div>
          <label className="block text-xs font-medium text-surface-500 mb-1">Allowed Providers</label>
          <MultiSelect
            options={providerOptions}
            value={allowedProviders}
            onChange={setAllowedProviders}
            placeholder="All providers"
          />
        </div>
      </div>

      <div className="flex gap-2 pt-1">
        <Button onClick={handleSubmit} disabled={!name.trim() || saving}>
          {saving ? 'Creating...' : 'Create Key'}
        </Button>
        <Button variant="outline" onClick={onClose}>
          Cancel
        </Button>
      </div>
    </div>
  )
}
