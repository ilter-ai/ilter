import { useQuery } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { api, type User } from '../../lib/api'
import { qk } from '../../lib/query'
import { useApiMutation } from '../../lib/useApiMutation'
import { cn } from '../../lib/utils'
import { StatusBadge } from '../ui/badge'
import { Button } from '../ui/button'
import { Card } from '../ui/card'
import { EmptyState } from '../ui/empty-state'
import { FilterBar } from '../ui/FilterBar'
import { AlertTriangle, ChevronDown, ChevronsUpDown, ChevronUp, Edit3, Plus, Trash2 } from '../ui/icons'
import { Pagination } from '../ui/Pagination'
import { QueryProvider } from '../ui/query-provider'
import { Skeleton } from '../ui/skeleton'

type SortField = 'name' | 'email' | 'status' | 'created_at'
type SortDir = 'asc' | 'desc'

function SortIcon({ field, currentField, sortDir }: { field: SortField; currentField: SortField; sortDir: SortDir }) {
  if (currentField !== field) {
    return <ChevronsUpDown size={12} className="ml-1 text-surface-400" />
  }
  return sortDir === 'asc' ? (
    <ChevronUp size={12} className="ml-1 text-brand-600" />
  ) : (
    <ChevronDown size={12} className="ml-1 text-brand-600" />
  )
}

interface UserListProps {
  onNavigate: (path: string) => void
}

function UserListContent({ onNavigate }: { onNavigate: (path: string) => void }) {
  const [search, setSearch] = useState('')
  const [sortField, setSortField] = useState<SortField>('created_at')
  const [sortDir, setSortDir] = useState<SortDir>('desc')
  const [page, setPage] = useState(1)
  const [deleteTarget, setDeleteTarget] = useState<User | null>(null)
  const perPage = 10

  const {
    data: users = [],
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: qk.users,
    queryFn: () => api.users.getUsers(),
  })

  const filtered = useMemo(() => {
    let result = users

    if (search.trim()) {
      const q = search.toLowerCase()
      result = result.filter((u) => u.name.toLowerCase().includes(q) || u.email.toLowerCase().includes(q))
    }

    result.sort((a, b) => {
      let cmp = 0
      switch (sortField) {
        case 'name':
          cmp = a.name.localeCompare(b.name)
          break
        case 'email':
          cmp = a.email.localeCompare(b.email)
          break
        case 'status':
          cmp = a.status.localeCompare(b.status)
          break
        case 'created_at':
          cmp = new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
          break
      }
      return sortDir === 'asc' ? cmp : -cmp
    })

    return result
  }, [users, search, sortField, sortDir])

  const totalPages = Math.max(1, Math.ceil(filtered.length / perPage))
  const paginated = filtered.slice((page - 1) * perPage, page * perPage)

  useEffect(() => {
    setPage(1)
  }, [])

  const handleSort = (field: SortField) => {
    if (sortField === field) {
      setSortDir((d) => (d === 'asc' ? 'desc' : 'asc'))
    } else {
      setSortField(field)
      setSortDir('asc')
    }
  }

  const deleteUser = useApiMutation((id: number) => api.users.deleteUser(id), { invalidate: [qk.users] })

  const handleDelete = async () => {
    if (!deleteTarget) return
    try {
      await deleteUser.mutateAsync(deleteTarget.id)
      setDeleteTarget(null)
      toast.success('User deleted', { description: `User "${deleteTarget.name}" has been deleted.` })
    } catch (err) {
      toast.error('Failed to delete user', { description: String(err) })
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <FilterBar
          searchPlaceholder="Search by name or email..."
          searchValue={search}
          onSearchChange={setSearch}
          className="flex-1 max-w-md"
        />
        <Button onClick={() => onNavigate('/users/new')}>
          <Plus size={16} />
          Create User
        </Button>
      </div>

      <Card>
        {error ? (
          <div className="p-6 text-center">
            <h3 className="text-error font-medium">Failed to load users</h3>
            <p className="text-surface-500 text-sm mt-1">{error.message}</p>
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
        ) : filtered.length === 0 ? (
          <EmptyState
            title={search ? 'No users match your search' : 'No users yet'}
            description={search ? 'Try a different search term.' : 'Create your first user to get started.'}
            action={search ? undefined : { label: 'Create User', onClick: () => onNavigate('/users/new') }}
          />
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full">
              <thead>
                <tr className="border-b border-surface-200">
                  {[
                    { key: 'name', label: 'Name' },
                    { key: 'email', label: 'Email' },
                    { key: 'status', label: 'Status' },
                    { key: 'created_at', label: 'Created' },
                    { key: 'actions', label: 'Actions', sortable: false },
                  ].map((h) => (
                    <th
                      key={h.key}
                      className={`px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-surface-500 ${
                        h.sortable !== false ? 'cursor-pointer hover:text-surface-700 select-none' : ''
                      }`}
                      onClick={() => h.sortable !== false && handleSort(h.key as SortField)}
                    >
                      <span className="inline-flex items-center">
                        {h.label}
                        {h.sortable !== false && (
                          <SortIcon field={h.key as SortField} currentField={sortField} sortDir={sortDir} />
                        )}
                      </span>
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody className="divide-y divide-surface-100">
                {paginated.map((user) => (
                  <tr key={user.id} className="transition-colors hover:bg-surface-50">
                    <td className="px-4 py-3">
                      <p className="text-sm font-medium text-surface-900">{user.name}</p>
                    </td>
                    <td className="px-4 py-3 text-sm text-surface-600">{user.email}</td>
                    <td className="px-4 py-3">
                      <StatusBadge status={user.status} />
                    </td>
                    <td className="px-4 py-3 text-sm text-surface-600">
                      {new Date(user.created_at).toLocaleDateString()}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex items-center gap-2">
                        <Button variant="outline" size="sm" onClick={() => onNavigate(`/users/${user.id}/edit`)}>
                          <Edit3 size={14} />
                          Edit
                        </Button>
                        <Button variant="destructive" size="sm" onClick={() => setDeleteTarget(user)}>
                          <Trash2 size={14} />
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

      <Pagination
        page={page}
        totalPages={totalPages}
        total={filtered.length}
        perPage={perPage}
        onPageChange={setPage}
      />

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
                <h3 className="text-lg font-semibold text-surface-900">Delete User</h3>
                <p className="text-sm text-surface-500 mt-0.5">
                  Are you sure you want to delete <strong className="text-surface-700">{deleteTarget.name}</strong>?
                </p>
              </div>
            </div>

            <p className="text-sm text-surface-600 bg-surface-50 rounded-lg p-3 mb-4 border border-surface-200">
              This action cannot be undone. The user will be permanently removed from the system.
            </p>

            <div className="flex justify-end gap-2">
              <Button variant="outline" onClick={() => setDeleteTarget(null)}>
                Cancel
              </Button>
              <Button variant="destructive" onClick={handleDelete}>
                Yes, Delete User
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export function UserList({ onNavigate }: UserListProps) {
  return (
    <QueryProvider>
      <UserListContent onNavigate={onNavigate} />
    </QueryProvider>
  )
}
