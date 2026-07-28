import { useQuery } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { MCP_COMMUNICATION } from '../../data/mcp-communications'
import { MCP_TOP_100 } from '../../data/mcp-top100'
import { api } from '../../lib/api'
import { qk } from '../../lib/query'
import type { MCPServerEntry } from '../../types/mcp'
import { FilterBar } from '../ui/FilterBar'
import { LayoutGrid, List } from '../ui/icons'
import { QueryProvider } from '../ui/query-provider'
import { CategoryFilter } from './CategoryFilter'
import DiscoverInstallModal from './DiscoverInstallModal'
import { MarketplaceCard } from './MarketplaceCard'

const ALL_MCP_SERVERS: MCPServerEntry[] = [...MCP_TOP_100, ...MCP_COMMUNICATION]

const ITEMS_PER_PAGE = 18

function McpMarketplaceViewContent() {
  const [search, setSearch] = useState('')
  const [category, setCategory] = useState<string | null>(null)
  const [viewMode, setViewMode] = useState<'grid' | 'list'>('grid')
  const [page, setPage] = useState(0)
  const [installServer, setInstallServer] = useState<MCPServerEntry | null>(null)

  const { data: installedServers = new Map<string, string>(), refetch } = useQuery({
    queryKey: [...qk.mcpServers, 'marketplace-installed'],
    queryFn: async () => {
      const servers = await api.mcp.getMCPServers()
      return new Map(servers.map((s) => [s.name, s.id]))
    },
  })

  const refetchInstalled = () => {
    refetch()
  }

  const categories = useMemo(() => {
    const counts: Record<string, number> = {}
    ALL_MCP_SERVERS.forEach((s) => {
      counts[s.category] = (counts[s.category] || 0) + 1
    })
    return Object.entries(counts)
      .map(([name, count]) => ({ name, count }))
      .sort((a, b) => b.count - a.count)
  }, [])

  const filtered = useMemo(() => {
    let result = ALL_MCP_SERVERS
    if (category) {
      result = result.filter((s) => s.category === category)
    }
    if (search) {
      const q = search.toLowerCase()
      result = result.filter(
        (s) =>
          s.name.toLowerCase().includes(q) ||
          s.description.toLowerCase().includes(q) ||
          s.category.toLowerCase().includes(q) ||
          s.tools.some((t) => t.toLowerCase().includes(q)),
      )
    }
    return result
  }, [category, search])

  const totalPages = Math.ceil(filtered.length / ITEMS_PER_PAGE)
  const paged = filtered.slice(page * ITEMS_PER_PAGE, (page + 1) * ITEMS_PER_PAGE)

  const handleInstall = (server: MCPServerEntry) => {
    setInstallServer(server)
  }

  const handleUninstall = async (serverId: string) => {
    await api.mcp.deleteMCPServer(serverId)
    refetchInstalled()
  }

  // Refresh the installed-list badge only — don't close the modal. The modal
  // stays open after a successful save so the user can use "Sync Connection"
  // to verify it actually works (and see remediation steps if not) before
  // dismissing it themselves via Cancel/X.
  const handleInstallSuccess = () => {
    refetch()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold text-surface-900">MCP Marketplace</h2>
          <p className="text-sm text-surface-500 mt-0.5">Browse and install MCP servers from the community</p>
        </div>
      </div>

      <div className="flex items-center gap-3">
        <div className="flex-1">
          <FilterBar
            searchPlaceholder="Search servers, tools, categories..."
            searchValue={search}
            onSearchChange={(v) => {
              setSearch(v)
              setPage(0)
            }}
          />
        </div>
        <div className="flex items-center gap-1 rounded-lg border border-surface-200 p-0.5 bg-surface-50">
          <button
            onClick={() => setViewMode('grid')}
            className={`rounded-md p-1.5 transition-colors ${viewMode === 'grid' ? 'bg-white text-surface-900 shadow-sm' : 'text-surface-400 hover:text-surface-600'}`}
            title="Grid view"
          >
            <LayoutGrid size={16} />
          </button>
          <button
            onClick={() => setViewMode('list')}
            className={`rounded-md p-1.5 transition-colors ${viewMode === 'list' ? 'bg-white text-surface-900 shadow-sm' : 'text-surface-400 hover:text-surface-600'}`}
            title="List view"
          >
            <List size={16} />
          </button>
        </div>
      </div>

      <CategoryFilter
        categories={categories}
        selected={category}
        onSelect={(c) => {
          setCategory(c)
          setPage(0)
        }}
      />

      <p className="text-sm text-surface-500">
        Showing {paged.length} of {filtered.length} server{filtered.length !== 1 ? 's' : ''}
        {category && (
          <>
            {' '}
            in <span className="font-medium text-surface-700">{category}</span>
          </>
        )}
      </p>

      {paged.length === 0 ? (
        <div className="rounded-xl border border-surface-200 bg-surface-50 p-12 text-center">
          <p className="text-surface-500 text-sm">No servers match your filters. Try adjusting your search.</p>
        </div>
      ) : viewMode === 'grid' ? (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4">
          {paged.map((server) => (
            <MarketplaceCard
              key={server.name}
              server={server}
              viewMode="grid"
              onInstall={handleInstall}
              installedId={installedServers.get(server.name)}
              onUninstall={handleUninstall}
            />
          ))}
        </div>
      ) : (
        <div className="space-y-2">
          {paged.map((server) => (
            <MarketplaceCard
              key={server.name}
              server={server}
              viewMode="list"
              onInstall={handleInstall}
              installedId={installedServers.get(server.name)}
              onUninstall={handleUninstall}
            />
          ))}
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-between">
          <span className="text-sm text-surface-500">
            Page {page + 1} of {totalPages}
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

      <DiscoverInstallModal
        open={installServer !== null}
        server={installServer}
        onClose={() => setInstallServer(null)}
        onSuccess={handleInstallSuccess}
      />
    </div>
  )
}

export function McpMarketplaceView() {
  return (
    <QueryProvider>
      <McpMarketplaceViewContent />
    </QueryProvider>
  )
}
