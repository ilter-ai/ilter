import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import { api, type Group } from '../../lib/api'
import { qk } from '../../lib/query'
import { useApiMutation } from '../../lib/useApiMutation'
import { Button } from '../ui/button'
import { Card, CardContent } from '../ui/card'
import { QueryProvider } from '../ui/query-provider'

interface GroupFormProps {
  mode: 'create' | 'edit'
  groupId?: number
  onSaved?: (group: Group) => void
  onCancel?: () => void
}

function GroupFormContent({ mode, groupId, onSaved, onCancel }: GroupFormProps) {
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')

  const { isLoading } = useQuery({
    queryKey: qk.groupDetail(groupId!),
    queryFn: async () => {
      const group = await api.groups.getGroup(groupId!)
      setName(group.name)
      setDescription(group.description || '')
      return group
    },
    enabled: mode === 'edit' && !!groupId,
  })

  const saveGroup = useApiMutation(
    (data: { id?: number; name: string; description?: string }) =>
      data.id
        ? api.groups.updateGroup(data.id, { name: data.name, description: data.description })
        : api.groups.createGroup({ name: data.name, description: data.description }),
    { invalidate: [qk.groups] },
  )

  const handleSubmit = async () => {
    if (!name.trim()) return
    try {
      const group = await saveGroup.mutateAsync({
        id: mode === 'edit' ? groupId : undefined,
        name: name.trim(),
        description: description.trim() || undefined,
      })
      toast.success(mode === 'create' ? 'Group created' : 'Group updated', {
        description: `"${group.name}" was ${mode === 'create' ? 'created' : 'updated'}.`,
      })
      onSaved?.(group)
    } catch (err) {
      toast.error(`Failed to ${mode} group`, { description: String(err) })
    }
  }

  if (isLoading) {
    return (
      <Card>
        <CardContent className="p-6">
          <div className="animate-pulse space-y-3">
            <div className="h-4 w-1/4 bg-surface-200 rounded" />
            <div className="h-10 w-full bg-surface-200 rounded" />
            <div className="h-4 w-1/4 bg-surface-200 rounded" />
            <div className="h-20 w-full bg-surface-200 rounded" />
          </div>
        </CardContent>
      </Card>
    )
  }

  return (
    <Card>
      <CardContent className="p-6 space-y-4">
        <h2 className="text-lg font-semibold text-surface-900">{mode === 'create' ? 'Create Group' : 'Edit Group'}</h2>

        <div>
          <label className="block text-xs font-medium text-surface-500 mb-1">Name *</label>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. engineering-team"
            className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
          />
        </div>

        <div>
          <label className="block text-xs font-medium text-surface-500 mb-1">Description</label>
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Optional description for this group"
            rows={3}
            className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 resize-none"
          />
        </div>

        <div className="flex gap-2 pt-1">
          <Button onClick={handleSubmit} disabled={!name.trim() || saveGroup.isPending}>
            {saveGroup.isPending ? 'Saving...' : mode === 'create' ? 'Create Group' : 'Save Changes'}
          </Button>
          <Button variant="outline" onClick={() => onCancel?.()}>
            Cancel
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

export function GroupForm({ mode, groupId, onSaved, onCancel }: GroupFormProps) {
  return (
    <QueryProvider>
      <GroupFormContent mode={mode} groupId={groupId} onSaved={onSaved} onCancel={onCancel} />
    </QueryProvider>
  )
}
