import { useState } from 'react'
import type { Group, User } from '../../lib/api'

export function GroupField({
  groups,
  groupId,
  onChange,
}: {
  groups: Group[]
  groupId: number | undefined
  onChange: (id: number | undefined) => void
}) {
  const [search, setSearch] = useState(() => groups.find((g) => g.id === groupId)?.name ?? '')
  const [showDropdown, setShowDropdown] = useState(false)

  return (
    <div className="relative">
      <label className="block text-xs font-medium text-surface-500 mb-1">Group</label>
      <input
        type="text"
        value={search}
        onChange={(e) => {
          setSearch(e.target.value)
          setShowDropdown(true)
        }}
        onFocus={() => setShowDropdown(true)}
        onBlur={() => setTimeout(() => setShowDropdown(false), 200)}
        placeholder="Search or select a group..."
        className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
      />
      {showDropdown && (
        <div className="absolute z-10 mt-1 max-h-48 w-full overflow-y-auto rounded-lg border border-surface-200 bg-white shadow-lg">
          <button
            type="button"
            onMouseDown={() => {
              onChange(undefined)
              setSearch('')
              setShowDropdown(false)
            }}
            className={`w-full px-3 py-2 text-left text-sm hover:bg-surface-100 ${groupId === undefined ? 'bg-brand-50 text-brand-700' : 'text-surface-700'}`}
          >
            No Group
          </button>
          {groups
            .filter((g) => !search || g.name.toLowerCase().includes(search.toLowerCase()))
            .map((g) => (
              <button
                key={g.id}
                type="button"
                onMouseDown={() => {
                  onChange(g.id)
                  setSearch(g.name)
                  setShowDropdown(false)
                }}
                className={`w-full px-3 py-2 text-left text-sm hover:bg-surface-100 ${groupId === g.id ? 'bg-brand-50 text-brand-700' : 'text-surface-700'}`}
              >
                {g.name}
              </button>
            ))}
          {groups.length === 0 && <div className="px-3 py-2 text-sm text-surface-400">No groups available</div>}
        </div>
      )}
    </div>
  )
}

export function UserField({
  users,
  userId,
  onChange,
}: {
  users: User[]
  userId: number | undefined
  onChange: (id: number | undefined) => void
}) {
  const initialUser = users.find((u) => u.id === userId)
  const [search, setSearch] = useState(() =>
    initialUser ? `${initialUser.name}${initialUser.email ? ` (${initialUser.email})` : ''}` : '',
  )
  const [showDropdown, setShowDropdown] = useState(false)

  return (
    <div className="relative">
      <label className="block text-xs font-medium text-surface-500 mb-1">User</label>
      <input
        type="text"
        value={search}
        onChange={(e) => {
          setSearch(e.target.value)
          setShowDropdown(true)
        }}
        onFocus={() => setShowDropdown(true)}
        onBlur={() => setTimeout(() => setShowDropdown(false), 200)}
        placeholder="Search or select a user..."
        className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
      />
      {showDropdown && (
        <div className="absolute z-10 mt-1 max-h-48 w-full overflow-y-auto rounded-lg border border-surface-200 bg-white shadow-lg">
          <button
            type="button"
            onMouseDown={() => {
              onChange(undefined)
              setSearch('')
              setShowDropdown(false)
            }}
            className={`w-full px-3 py-2 text-left text-sm hover:bg-surface-100 ${userId === undefined ? 'bg-brand-50 text-brand-700' : 'text-surface-700'}`}
          >
            No User (group-only key)
          </button>
          {users
            .filter(
              (u) =>
                !search ||
                u.name.toLowerCase().includes(search.toLowerCase()) ||
                u.email.toLowerCase().includes(search.toLowerCase()),
            )
            .map((u) => (
              <button
                key={u.id}
                type="button"
                onMouseDown={() => {
                  onChange(u.id)
                  setSearch(`${u.name}${u.email ? ` (${u.email})` : ''}`)
                  setShowDropdown(false)
                }}
                className={`w-full px-3 py-2 text-left text-sm hover:bg-surface-100 ${userId === u.id ? 'bg-brand-50 text-brand-700' : 'text-surface-700'}`}
              >
                {u.name}
                {u.email ? <span className="text-surface-400 ml-1">({u.email})</span> : null}
              </button>
            ))}
          {users.length === 0 && <div className="px-3 py-2 text-sm text-surface-400">No users available</div>}
        </div>
      )}
    </div>
  )
}
