import { useQuery } from '@tanstack/react-query'
import { useCallback, useRef, useState } from 'react'
import { toast } from 'sonner'
import { api, type GroupMember, request } from '../../lib/api'
import { logger } from '../../lib/logger'
import { qk } from '../../lib/query'
import { useApiMutation } from '../../lib/useApiMutation'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { AlertTriangle, Plus, Search } from '../ui/icons'
import { QueryProvider } from '../ui/query-provider'
import { Skeleton } from '../ui/skeleton'

interface GroupMembersProps {
  groupId: number
}

interface SearchUser {
  id: number
  name: string
  email: string
}

function GroupMembersContent({ groupId }: GroupMembersProps) {
  const [showAddModal, setShowAddModal] = useState(false)
  const [removeTarget, setRemoveTarget] = useState<GroupMember | null>(null)

  const {
    data: members = [],
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: qk.groupMembers(groupId),
    queryFn: () => api.groups.getGroupMembers(groupId).catch(() => [] as GroupMember[]),
  })

  const removeMember = useApiMutation((memberId: number) => api.groups.removeGroupMember(groupId, memberId), {
    invalidate: [qk.groupMembers(groupId)],
  })

  const handleRemove = async () => {
    if (!removeTarget) return
    try {
      await removeMember.mutateAsync(removeTarget.id)
      setRemoveTarget(null)
      toast.warning('Member removed', { description: `"${removeTarget.name}" was removed from the group.` })
    } catch (err) {
      toast.error('Failed to remove member', { description: String(err) })
    }
  }

  const handleMemberAdded = () => {
    refetch()
    setShowAddModal(false)
  }

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between px-6 py-4">
        <CardTitle className="text-base">Members ({members.length})</CardTitle>
        <Button size="sm" onClick={() => setShowAddModal(true)}>
          <Plus size={14} />
          Add Member
        </Button>
      </CardHeader>
      <CardContent className="p-0">
        {error ? (
          <div className="p-6 text-center">
            <h3 className="text-error font-medium text-sm">Failed to load members</h3>
            <p className="text-surface-500 text-xs mt-1">
              {error instanceof Error ? error.message : 'An error occurred'}
            </p>
            <Button variant="outline" size="sm" className="mt-3" onClick={() => refetch()}>
              Retry
            </Button>
          </div>
        ) : isLoading ? (
          <div className="rounded-xl border border-surface-200 bg-white shadow-card">
            <div className="flex border-b border-surface-200 px-4 py-3 gap-4">
              {Array.from({ length: 4 }).map((_, ci) => (
                <Skeleton key={`h-${ci}`} className={cn('h-3', ci === 0 ? 'w-1/4' : 'w-1/6')} />
              ))}
            </div>
            {Array.from({ length: 3 }).map((_, ri) => (
              <div key={`r-${ri}`} className="flex border-b border-surface-100 px-4 py-3.5 gap-4 last:border-b-0">
                {Array.from({ length: 4 }).map((_, ci) => (
                  <Skeleton key={`c-${ri}-${ci}`} className={cn('h-3', ci === 0 ? 'w-2/5' : 'w-1/5')} />
                ))}
              </div>
            ))}
          </div>
        ) : members.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-10">
            <p className="text-sm text-surface-400">No members yet</p>
            <Button variant="outline" size="sm" className="mt-3" onClick={() => setShowAddModal(true)}>
              Add Member
            </Button>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b border-surface-200">
                  {['Name', 'Email', 'Added', 'Actions'].map((h) => (
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
                {members.map((member) => (
                  <tr key={member.id} className="transition-colors hover:bg-surface-50">
                    <td className="px-4 py-3">
                      <p className="text-sm font-medium text-surface-900">{member.name}</p>
                    </td>
                    <td className="px-4 py-3 text-sm text-surface-600">{member.email}</td>
                    <td className="px-4 py-3 text-sm text-surface-600 whitespace-nowrap">
                      {new Date(member.created_at).toLocaleDateString()}
                    </td>
                    <td className="px-4 py-3">
                      <Button variant="destructive" size="sm" onClick={() => setRemoveTarget(member)}>
                        Remove
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>

      {showAddModal && (
        <AddMemberModal
          groupId={groupId}
          existingMemberIds={members.map((m) => m.id)}
          onAdded={handleMemberAdded}
          onClose={() => setShowAddModal(false)}
        />
      )}

      {removeTarget && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
          onClick={() => setRemoveTarget(null)}
        >
          <div className="w-full max-w-md rounded-xl bg-white p-6 shadow-xl" onClick={(e) => e.stopPropagation()}>
            <div className="flex items-center gap-3 mb-4">
              <div className="rounded-full bg-error/10 p-2 text-error">
                <AlertTriangle size={20} />
              </div>
              <div>
                <h3 className="text-lg font-semibold text-surface-900">Remove Member</h3>
                <p className="text-sm text-surface-500 mt-0.5">
                  Remove <strong className="text-surface-700">{removeTarget.name}</strong> from this group?
                </p>
              </div>
            </div>
            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setRemoveTarget(null)}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={handleRemove}>
                Remove
              </Button>
            </div>
          </div>
        </div>
      )}
    </Card>
  )
}

export function GroupMembers({ groupId }: GroupMembersProps) {
  return (
    <QueryProvider>
      <GroupMembersContent groupId={groupId} />
    </QueryProvider>
  )
}

/* ── Add Member Modal with User Search ── */

function AddMemberModal({
  groupId,
  existingMemberIds,
  onAdded,
  onClose,
}: {
  groupId: number
  existingMemberIds: number[]
  onAdded: () => void
  onClose: () => void
}) {
  const [query, setQuery] = useState('')
  const [users, setUsers] = useState<SearchUser[]>([])
  const [loading, setLoading] = useState(false)
  const [adding, setAdding] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const debounceRef = useRef<ReturnType<typeof setTimeout>>(undefined)

  const searchUsers = useCallback(
    async (q: string) => {
      if (q.length < 1) {
        setUsers([])
        return
      }
      setLoading(true)
      try {
        const data = await request<{ users: SearchUser[] }>('/users')
        const allUsers = data.users || []
        const filtered = allUsers.filter(
          (u) =>
            !existingMemberIds.includes(u.id) &&
            (u.name.toLowerCase().includes(q.toLowerCase()) || u.email.toLowerCase().includes(q.toLowerCase())),
        )
        setUsers(filtered)
      } catch (err) {
        // JSON.parse only throws SyntaxError
        if (err instanceof SyntaxError) {
          setUsers([])
        } else {
          logger.error('Search error', err)
        }
      } finally {
        setLoading(false)
      }
    },
    [existingMemberIds],
  )

  const handleQueryChange = (value: string) => {
    setQuery(value)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => searchUsers(value), 200)
  }

  const handleAdd = async (user: SearchUser) => {
    setAdding(true)
    try {
      await api.groups.addGroupMember(groupId, user.id)
      onAdded()
    } catch (err) {
      toast.error('Failed to add member', { description: String(err) })
    } finally {
      setAdding(false)
    }
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div
        className="w-full max-w-lg rounded-xl bg-white shadow-xl overflow-hidden"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="p-4 border-b border-surface-200">
          <h3 className="text-base font-semibold text-surface-900 mb-3">Add Member</h3>
          <div className="relative">
            <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-surface-400" />
            <input
              ref={inputRef}
              type="text"
              value={query}
              onChange={(e) => handleQueryChange(e.target.value)}
              placeholder="Search users by name or email..."
              className="w-full rounded-lg border border-surface-300 bg-white py-2 pl-10 pr-3 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
            />
          </div>
        </div>

        <div className="max-h-64 overflow-y-auto">
          {loading ? (
            <div className="flex items-center justify-center py-8">
              <div className="animate-spin w-5 h-5 border-2 border-surface-300 border-t-brand-500 rounded-full" />
            </div>
          ) : query.length < 1 ? (
            <p className="text-sm text-surface-400 text-center py-8">Type to search users</p>
          ) : users.length === 0 ? (
            <p className="text-sm text-surface-400 text-center py-8">No users found</p>
          ) : (
            <ul className="divide-y divide-surface-100">
              {users.map((user) => (
                <li
                  key={user.id}
                  className="flex items-center justify-between px-4 py-3 hover:bg-surface-50 transition-colors"
                >
                  <div className="min-w-0 flex-1">
                    <p className="text-sm font-medium text-surface-900 truncate">{user.name}</p>
                    <p className="text-xs text-surface-500 truncate">{user.email}</p>
                  </div>
                  <Button size="sm" onClick={() => handleAdd(user)} disabled={adding}>
                    {adding ? 'Adding...' : 'Add'}
                  </Button>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="p-3 border-t border-surface-200 flex justify-end">
          <Button variant="outline" size="sm" onClick={onClose}>
            Cancel
          </Button>
        </div>
      </div>
    </div>
  )
}
