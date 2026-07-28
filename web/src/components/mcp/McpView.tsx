import { useEffect, useRef, useState } from 'react'
import { McpAccessView } from '../mcp-access/McpAccessView'
import { McpMarketplaceView } from '../mcp-marketplace/McpMarketplaceView'
import { McpServerDetailView } from '../mcp-servers/McpServerDetailView'
import { McpServersView } from '../mcp-servers/McpServersView'

export type Tab = 'servers' | 'permissions' | 'marketplace'

export function McpView({ initialTab }: { initialTab?: Tab }) {
  const [path, setPath] = useState(() => window.location.pathname)
  const initialPath = useRef(window.location.pathname)

  useEffect(() => {
    const onPop = () => setPath(window.location.pathname)
    window.addEventListener('popstate', onPop)
    return () => window.removeEventListener('popstate', onPop)
  }, [])

  const navigate = (p: string) => {
    window.history.pushState(null, '', p)
    setPath(p)
  }

  // Match /mcp/servers/<id>
  const m = path.match(/^\/mcp\/servers\/([^/]+?)\/?$/)
  const selectedServerId = m?.[1] ?? null

  const tab =
    path === initialPath.current && initialTab
      ? initialTab
      : path.startsWith('/mcp/marketplace')
        ? 'marketplace'
        : path.startsWith('/mcp/permissions')
          ? 'permissions'
          : 'servers'

  const tabs = (
    <div role="tablist" className="flex gap-1 border-b border-surface-200 mb-6">
      {(['servers', 'permissions', 'marketplace'] as const).map((t) => (
        <a
          key={t}
          role="tab"
          aria-selected={tab === t && !selectedServerId}
          href={`/mcp/${t}`}
          onClick={(e) => {
            e.preventDefault()
            navigate(`/mcp/${t}`)
          }}
          className={`px-4 py-2.5 text-sm font-medium border-b-2 transition-colors ${
            tab === t && !selectedServerId
              ? 'border-brand-600 text-brand-700'
              : 'border-transparent text-surface-500 hover:text-surface-700 hover:border-surface-300'
          }`}
        >
          {t === 'servers' ? 'Servers' : t === 'permissions' ? 'Permissions' : 'Marketplace'}
        </a>
      ))}
    </div>
  )

  if (tab === 'permissions') {
    return (
      <div>
        {tabs}
        <McpAccessView />
      </div>
    )
  }

  if (tab === 'marketplace') {
    return (
      <div>
        {tabs}
        <McpMarketplaceView />
      </div>
    )
  }

  if (selectedServerId) {
    return (
      <div>
        {tabs}
        <McpServerDetailView serverId={selectedServerId} onBack={() => navigate('/mcp/servers')} />
      </div>
    )
  }

  return (
    <div>
      {tabs}
      <McpServersView onNavigate={navigate} />
    </div>
  )
}
