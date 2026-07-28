import { useEffect, useState } from 'react'
import { ApiKeysManager } from '../api-keys/ApiKeysManager'
import { GroupList } from '../groups/GroupList'
import { GroupManagementView } from '../groups/GroupManagementView'
import { UserList } from '../users/UserList'
import { UserManagementView } from '../users/UserManagementView'

export type Tab = 'keys' | 'users' | 'groups'

const TABS: { key: Tab; label: string }[] = [
  { key: 'keys', label: 'API Keys' },
  { key: 'users', label: 'Users' },
  { key: 'groups', label: 'Groups' },
]

const VALID_TABS = new Set<string>(['keys', 'users', 'groups'])

function tabFromPath(path: string): Tab {
  const match = path.match(/^\/access\/(\w+)/)
  const raw = match?.[1]
  return raw && VALID_TABS.has(raw) ? (raw as Tab) : 'keys'
}

export function AccessManagementView({ initialTab: _initialTab }: { initialTab?: Tab }) {
  const [path, setPath] = useState(() => window.location.pathname)

  useEffect(() => {
    const onPop = () => setPath(window.location.pathname)
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])

  const navigate = (p: string) => {
    window.history.pushState(null, '', p)
    setPath(p)
  }

  // Navigated away from /access/ to a standalone SPA view
  if (path.startsWith('/users/')) {
    return <UserManagementView />
  }
  if (path.startsWith('/groups/')) {
    return <GroupManagementView />
  }

  const tab = tabFromPath(path)

  return (
    <div>
      <div role="tablist" className="flex gap-1 border-b border-surface-200 mb-6">
        {TABS.map((t) => (
          <button
            key={t.key}
            role="tab"
            aria-selected={tab === t.key}
            onClick={() => navigate(`/access/${t.key}`)}
            className={`px-4 py-2.5 text-sm font-medium border-b-2 transition-colors cursor-pointer ${
              tab === t.key
                ? 'border-brand-600 text-brand-700'
                : 'border-transparent text-surface-500 hover:text-surface-700 hover:border-surface-300'
            }`}
          >
            {t.label}
          </button>
        ))}
      </div>
      {tab === 'keys' && <ApiKeysManager />}
      {tab === 'users' && <UserList onNavigate={navigate} />}
      {tab === 'groups' && <GroupList onNavigate={navigate} />}
    </div>
  )
}
