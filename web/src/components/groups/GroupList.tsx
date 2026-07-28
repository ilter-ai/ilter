import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import { api, type Group } from '../../lib/api'
import { qk } from '../../lib/query'
import { useApiMutation } from '../../lib/useApiMutation'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { Card } from '../ui/card'
import { EmptyState } from '../ui/empty-state'
import { FilterBar } from '../ui/FilterBar'
import { AlertTriangle, Plus } from '../ui/icons'
import { QueryProvider } from '../ui/query-provider'
import { Skeleton } from '../ui/skeleton'

interface GroupListProps {
  onNavigate: (path: string) => void
}

function GroupListContent({ onNavigate }: { onNavigate: (path: string) => void }) {
  const [search, setSearch] = useState('')
  const [deleteTarget, setDeleteTarget] = useState<Group | null>(null)

  const {
    data: groups = [],
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: qk.groups,
    queryFn: () => api.groups.getGroups().catch(() => [] as Group[]),
  })

  const filteredGroups = groups.filter((g) => g.name.toLowerCase().includes(search.toLowerCase()))

  const deleteGroup = useApiMutation((id: number) => api.groups.deleteGroup(id), { invalidate: [qk.groups] })

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      await deleteGroup.mutateAsync(deleteTarget.id)
      setDeleteTarget(null)
      toast.warning('Group deleted', { description: `"${deleteTarget.name}" has been deleted.` })
    } catch (err) {
      toast.error('Failed to delete group', { description: String(err) })
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <FilterBar
          searchPlaceholder="Search by name..."
          searchValue={search}
          onSearchChange={setSearch}
          className="flex-1 max-w-md"
        />
        <Button onClick={() => onNavigate('/groups/new')}>
          <Plus size={16} />
          Create Group
        </Button>
      </div>

      <Card>
        {error ? (
          <div className="p-6 text-center">
            <h3 className="text-error font-medium">Failed to load groups</h3>
            <p className="text-surface-500 text-sm mt-1">
              {error instanceof Error ? error.message : 'An error occurred'}
            </p>
            <Button variant="outline" size="sm" className="mt-3" onClick={() => refetch()}>
              Retry
            </Button>
          </div>
        ) : isLoading ? (
          <div className="rounded-xl border border-surface-200 bg-white shadow-card">
            <div className="flex border-b border-surface-200 px-4 py-3 gap-4">
              {Array.from({ length: 5 }).map((_, ci) => (
                <Skeleton key={`h-${ci}`} className={cn('h-3', ci === 0 ? 'w-1/4' : 'w-1/6')} />
              ))}
            </div>
            {Array.from({ length: 5 }).map((_, ri) => (
              <div key={`r-${ri}`} className="flex border-b border-surface-100 px-4 py-3.5 gap-4 last:border-b-0">
                {Array.from({ length: 5 }).map((_, ci) => (
                  <Skeleton key={`c-${ri}-${ci}`} className={cn('h-3', ci === 0 ? 'w-2/5' : 'w-1/5')} />
                ))}
              </div>
            ))}
          </div>
        ) : filteredGroups.length === 0 ? (
          <EmptyState
            title={search ? 'No matching groups' : 'No groups yet'}
            description={search ? 'Try a different search term.' : 'Create your first group to organize users.'}
            action={!search ? { label: 'Create Group', onClick: () => onNavigate('/groups/new') } : undefined}
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b border-surface-200">
                  {['Name', 'Description', 'Created At', 'Actions'].map((h) => (
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
                {filteredGroups.map((group) => (
                  <tr
                    key={group.id}
                    className="transition-colors hover:bg-surface-50 cursor-pointer"
                    onClick={() => onNavigate(`/groups/${group.id}/edit`)}
                  >
                    <td className="px-4 py-3">
                      <p className="text-sm font-medium text-surface-900">{group.name}</p>
                    </td>
                    <td className="px-4 py-3 text-sm text-surface-600 max-w-xs truncate">
                      {group.description || <span className="text-surface-400">—</span>}
                    </td>
                    <td className="px-4 py-3 text-sm text-surface-600 whitespace-nowrap">
                      {new Date(group.created_at).toLocaleDateString()}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-1" onClick={(e) => e.stopPropagation()}>
                        <Button variant="outline" size="sm" onClick={() => onNavigate(`/groups/${group.id}/edit`)}>
                          Edit
                        </Button>
                        <Button variant="destructive" size="sm" onClick={() => setDeleteTarget(group)}>
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
                <h3 className="text-lg font-semibold text-surface-900">Delete Group</h3>
                <p className="text-sm text-surface-500 mt-0.5">
                  Are you sure you want to delete <strong className="text-surface-700">{deleteTarget.name}</strong>?
                </p>
              </div>
            </div>
            <p className="text-sm text-surface-600 bg-surface-50 rounded-lg p-3 mb-4 border border-surface-200">
              This action cannot be undone. All memberships will be removed.
            </p>
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setDeleteTarget(null)}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={handleDelete}>
                Yes, Delete Group
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export function GroupList({ onNavigate }: GroupListProps) {
  return (
    <QueryProvider>
      <GroupListContent onNavigate={onNavigate} />
    </QueryProvider>
  )
}
