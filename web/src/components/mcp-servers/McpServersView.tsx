import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { api, type MCPServer } from '../../lib/api'
import { getToken } from '../../lib/auth'
import { cn } from '../../lib/utils'
import { FeatureStatus } from '../settings/FeatureStatus'
import { Button } from '../ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { EmptyState } from '../ui/empty-state'
import { FilterBar } from '../ui/FilterBar'
import { Clock, Copy, Edit3, Link, Loader2, Lock, Plus, RefreshCw, Trash2, Wand2, X } from '../ui/icons'
import { StatCard } from '../ui/StatCard'
import { Skeleton } from '../ui/skeleton'
import { Switch } from '../ui/switch'
import { GrantAccessPanel } from './GrantAccessPanel'
import { McpToolsDialog } from './McpToolsDialog'
import type { ServerFormData } from './ServerFormModal'
import { ServerFormModal } from './ServerFormModal'

function relativeTime(dateStr: string): string {
  if (!dateStr) return 'Never'
  const now = Date.now()
  const then = new Date(dateStr).getTime()
  if (Number.isNaN(then)) return 'Never'
  const diffSec = Math.floor((now - then) / 1000)
  if (diffSec < 60) return `${diffSec}s ago`
  const diffMin = Math.floor(diffSec / 60)
  if (diffMin < 60) return `${diffMin}m ago`
  const diffHr = Math.floor(diffMin / 60)
  if (diffHr < 24) return `${diffHr}h ago`
  return `${Math.floor(diffHr / 24)}d ago`
}

function TimeAgo({ date }: { date: string }) {
  const [text, setText] = useState('')
  useEffect(() => {
    setText(relativeTime(date))
  }, [date])
  return <>{text}</>
}

const statusConfig = {
  online: { dot: 'bg-success', label: 'Online', text: 'text-success', bg: 'bg-success/10 border-success/20' },
  offline: { dot: 'bg-error', label: 'Offline', text: 'text-error', bg: 'bg-error/10 border-error/20' },
  error: { dot: 'bg-warning', label: 'Error', text: 'text-warning', bg: 'bg-warning/10 border-warning/20' },
}

type StatusTab = 'all' | 'online' | 'offline' | 'error'

const statusTabs: { key: StatusTab; label: string }[] = [
  { key: 'all', label: 'All' },
  { key: 'online', label: 'Online' },
  { key: 'offline', label: 'Offline' },
  { key: 'error', label: 'Error' },
]

const transportLabels: Record<string, string> = {
  sse: 'SSE',
  stdio: 'STDIO',
  inline: 'Inline',
}

function ConnectGuidePanel({ onClose }: { onClose: () => void }) {
  const endpointUrl = `${typeof window !== 'undefined' ? window.location.origin : ''}/mcp/sse`
  const token = getToken() || ''

  const copyToClipboard = (text: string, label: string) => {
    navigator.clipboard.writeText(text).then(() => toast.success(`${label} copied`))
  }

  const exampleConfig = JSON.stringify(
    {
      mcpServers: {
        'ilter-hub': {
          url: endpointUrl,
          headers: {
            Authorization: `Bearer ${token || '<YOUR_TOKEN>'}`,
          },
        },
      },
    },
    null,
    2,
  )

  return (
    <>
      <div className="fixed inset-0 z-50 bg-black/30 backdrop-blur-sm" onClick={onClose} />
      <div className="fixed right-0 top-0 z-50 flex h-full w-[480px] max-w-full flex-col border-l border-surface-200 bg-white shadow-2xl">
        <div className="flex items-center justify-between border-b border-surface-200 px-6 py-5">
          <h3 className="text-lg font-semibold text-surface-900">Connect to ILTER MCP Hub</h3>
          <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close">
            <X size={16} />
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto px-6 py-6">
          <div className="space-y-6">
            <p className="text-sm leading-relaxed text-surface-600">
              You can connect external MCP clients (like Claude Desktop, Cursor, or VSCode) directly to ILTER. ILTER
              acts as a centralized MCP Hub, exposing all your authorized tools.
            </p>

            <div>
              <label className="mb-1.5 block text-sm font-medium text-surface-700">Endpoint URL</label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={endpointUrl}
                  readOnly
                  className="flex-1 rounded-lg border border-surface-300 bg-surface-50 px-3 py-2 font-mono text-sm text-surface-900"
                />
                <Button
                  variant="outline"
                  size="icon-sm"
                  onClick={() => copyToClipboard(endpointUrl, 'Endpoint URL')}
                  aria-label="Copy endpoint URL"
                >
                  <Copy size={14} />
                </Button>
              </div>
            </div>

            <div>
              <label className="mb-1.5 block text-sm font-medium text-surface-700">Auth Token</label>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={token || 'Not authenticated'}
                  readOnly
                  className="flex-1 rounded-lg border border-surface-300 bg-surface-50 px-3 py-2 font-mono text-sm text-surface-900"
                />
                <Button
                  variant="outline"
                  size="icon-sm"
                  onClick={() => copyToClipboard(token, 'Token')}
                  disabled={!token}
                  aria-label="Copy auth token"
                >
                  <Copy size={14} />
                </Button>
              </div>
              {!token && <p className="mt-1 text-xs text-warning">No token found. Log in to see your admin token.</p>}
            </div>

            <div>
              <label className="mb-1.5 block text-sm font-medium text-surface-700">Example Config (SSE)</label>
              <div className="relative">
                <pre className="overflow-x-auto rounded-lg border border-surface-200 bg-surface-50 p-4 font-mono text-xs text-surface-800">
                  <code>{exampleConfig}</code>
                </pre>
                <Button
                  variant="outline"
                  size="icon-sm"
                  className="absolute right-2 top-2"
                  onClick={() => copyToClipboard(exampleConfig, 'Config')}
                  aria-label="Copy config"
                >
                  <Copy size={14} />
                </Button>
              </div>
              <p className="mt-2 text-xs text-surface-500">
                Use this configuration in any SSE-compatible MCP client (e.g., Cursor, Claude Desktop with SSE proxy, or
                a custom MCP host).
              </p>
            </div>
          </div>
        </div>
      </div>
    </>
  )
}

export function McpServersView({ onNavigate }: { onNavigate?: (path: string) => void }) {
  const [servers, setServers] = useState<MCPServer[]>([])
  const [showAddModal, setShowAddModal] = useState(false)
  const [editServerId, setEditServerId] = useState<string | null>(null)
  const [syncingId, setSyncingId] = useState<string | null>(null)
  const [syncError, setSyncError] = useState<{ error: string; stderr?: string; oauth_url?: string } | null>(null)
  const [accessServerId, setAccessServerId] = useState<string | null>(null)
  const [toolsServerId, setToolsServerId] = useState<string | null>(null)
  const [showConnectGuide, setShowConnectGuide] = useState(false)
  const [loading, setLoading] = useState(true)
  const [search, setSearch] = useState('')
  const [statusFilter, setStatusFilter] = useState<StatusTab>('all')
  const [mcpFeatureEnabled, setMcpFeatureEnabled] = useState(true)
  const [togglingMcpFeature, setTogglingMcpFeature] = useState(false)

  useEffect(() => {
    api.features
      .getFeatures()
      .then((flags) => {
        const flag = flags.find((f) => f.feature_key === 'mcp')
        if (flag) setMcpFeatureEnabled(flag.enabled)
      })
      .catch(() => {})

    api.mcp
      .getMCPServers()
      .then((data) => {
        if (!Array.isArray(data)) return setServers([])
        setServers(
          data.map((s) => ({
            id: String(s.id || ''),
            name: String(s.name || ''),
            transport: String(s.transport || ''),
            url: String(s.url || ''),
            enabled: Boolean(s.enabled ?? s.status !== 'offline'),
            status: (s.status || (s.enabled === false ? 'offline' : 'online')) as 'online' | 'offline' | 'error',
            tools_count: Number(s.tools_count ?? 0),
            command: String(s.command || ''),
            args: String(s.args || ''),
            env: String(s.env || ''),
            last_health_check: String(s.last_health_check || ''),
          })),
        )
      })
      .catch(() => toast.error('Failed to load MCP servers'))
      .finally(() => setLoading(false))
  }, [])

  const handleToggleMcpFeature = async () => {
    const next = !mcpFeatureEnabled
    setMcpFeatureEnabled(next)
    setTogglingMcpFeature(true)
    try {
      await api.features.toggleFeature('mcp', next)
      toast.success(next ? 'MCP feature enabled' : 'MCP feature disabled')
    } catch {
      setMcpFeatureEnabled(!next)
      toast.error('Failed to update MCP feature state')
    } finally {
      setTogglingMcpFeature(false)
    }
  }

  const editing = servers.find((s) => s.id === editServerId) ?? null

  const stats = {
    total: servers.length,
    online: servers.filter((s) => s.status === 'online').length,
    offline: servers.filter((s) => s.status === 'offline').length,
    error: servers.filter((s) => s.status === 'error').length,
    tools: servers.reduce((sum, s) => sum + s.tools_count, 0),
  }

  const addServer = (data: ServerFormData) => {
    api.mcp
      .createMCPServer({
        name: data.name,
        url: data.url,
        transport: data.transport,
        command: data.command,
        args: data.args,
        env: data.env ?? '',
      })
      .then((res) => {
        const newServer: MCPServer = {
          id: res.id || String(Date.now()),
          name: data.name,
          transport: data.transport,
          url: data.url,
          command: data.command,
          args: data.args,
          env: data.env ?? '',
          enabled: true,
          status: 'offline',
          tools_count: 0,
          last_health_check: new Date().toISOString(),
        }

        setServers((prev) => [...prev, newServer])
        toast.success('Server added')
      })
      .catch(() => toast.error('Failed to add server'))
  }

  const updateServer = (data: ServerFormData) => {
    if (!editing) return
    const id = editing.id
    const previous = editing
    setServers((prev) =>
      prev.map((s) =>
        s.id === id
          ? {
              ...s,
              name: data.name,
              url: data.url,
              transport: data.transport,
              command: data.command,
              args: data.args,
              env: data.env ?? '',
            }
          : s,
      ),
    )
    setEditServerId(null)
    api.mcp
      .updateMCPServer(id, {
        name: data.name,
        url: data.url,
        transport: data.transport,
        command: data.command,
        args: data.args,
        env: data.env ?? '',
      })
      .then(() => toast.success('Server updated'))
      .catch(() => {
        setServers((prev) => prev.map((s) => (s.id === id ? previous : s)))
        toast.error('Failed to update server')
      })
  }

  const deleteServer = (id: string) => {
    const deleted = servers.find((s) => s.id === id)
    setServers((prev) => prev.filter((s) => s.id !== id))
    api.mcp
      .deleteMCPServer(id)
      .then(() => toast.success('Server removed'))
      .catch(() => {
        if (deleted) setServers((prev) => (prev.some((s) => s.id === id) ? prev : [...prev, deleted]))
        toast.error('Failed to remove server')
      })
  }

  const syncConnection = (id: string) => {
    setSyncingId(id)
    setSyncError(null)
    api.mcp
      .testMCPServer(id)
      .then((res) => {
        if (res.status === 'online') {
          setServers((prev) =>
            prev.map((s) =>
              s.id === id
                ? {
                    ...s,
                    status: 'online',
                    tools_count: res.tools_count || 0,
                    last_health_check: new Date().toISOString(),
                  }
                : s,
            ),
          )
          toast.success('Sync successful')
        } else {
          setServers((prev) =>
            prev.map((s) => (s.id === id ? { ...s, status: 'error', last_health_check: new Date().toISOString() } : s)),
          )
          setSyncError({ error: res.error || 'Connection failed', stderr: res.stderr, oauth_url: res.oauth_url })
        }
      })
      .catch(() => {
        setServers((prev) =>
          prev.map((s) =>
            s.id === id ? { ...s, status: 'error' as const, last_health_check: new Date().toISOString() } : s,
          ),
        )
        toast.error('Sync failed')
      })
      .finally(() => setSyncingId(null))
  }

  const toggleServer = async (server: MCPServer) => {
    const newEnabled = !server.enabled
    setServers((prev) =>
      prev.map((s) =>
        s.id === server.id
          ? {
              ...s,
              enabled: newEnabled,
              status: newEnabled ? (s.status === 'error' ? 'error' : 'online') : 'offline',
            }
          : s,
      ),
    )
    try {
      const res = await api.mcp.toggleMCPServer(server.id)
      const actualEnabled = res.enabled ?? newEnabled
      setServers((prev) =>
        prev.map((s) =>
          s.id === server.id
            ? {
                ...s,
                enabled: actualEnabled,
                status: actualEnabled ? (s.status === 'error' ? 'error' : 'online') : 'offline',
              }
            : s,
        ),
      )
      toast.success(actualEnabled ? 'Server enabled' : 'Server disabled')
    } catch {
      setServers((prev) =>
        prev.map((s) =>
          s.id === server.id
            ? {
                ...s,
                enabled: !newEnabled,
                status: !newEnabled ? (s.status === 'error' ? 'error' : 'online') : 'offline',
              }
            : s,
        ),
      )
      toast.error('Failed to toggle server')
    }
  }

  const filtered = servers.filter((s) => {
    if (statusFilter !== 'all' && s.status !== statusFilter) return false
    if (search) {
      const q = search.toLowerCase()
      return (
        s.name.toLowerCase().includes(q) ||
        s.url.toLowerCase().includes(q) ||
        (s.transport || '').toLowerCase().includes(q)
      )
    }
    return true
  })

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard title="Total Servers" value={stats.total} />
        <StatCard title="Online" value={stats.online} />
        <StatCard title="Offline / Error" value={stats.offline + stats.error} />
        <StatCard title="Available Tools" value={stats.tools} />
      </div>

      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <h2 className="text-lg font-semibold text-surface-900">MCP Servers</h2>
          <Button variant="outline" onClick={() => setShowConnectGuide(true)}>
            <Link size={16} />
            How To Connect
          </Button>
        </div>
        <div className="flex items-center gap-3">
          <FeatureStatus
            type="toggle"
            enabled={mcpFeatureEnabled}
            onToggle={handleToggleMcpFeature}
            disabled={togglingMcpFeature}
          />
          <Button onClick={() => setShowAddModal(true)}>
            <Plus size={16} />
            Add Server
          </Button>
        </div>
      </div>

      <div className="flex flex-col sm:flex-row gap-3">
        <div className="flex-1">
          <FilterBar
            searchPlaceholder="Search by name, URL, or transport..."
            searchValue={search}
            onSearchChange={setSearch}
          />
        </div>
        <div className="flex items-center gap-1 bg-surface-50 rounded-lg p-1 border border-surface-200 shrink-0">
          {statusTabs.map((tab) => (
            <button
              key={tab.key}
              onClick={() => setStatusFilter(tab.key)}
              className={cn(
                'px-3 py-1.5 text-xs font-medium rounded-md transition-colors',
                statusFilter === tab.key
                  ? 'bg-white text-surface-900 shadow-sm border border-surface-200'
                  : 'text-surface-500 hover:text-surface-700',
              )}
            >
              {tab.label}
              {tab.key !== 'all' && <span className="ml-1.5 text-surface-400">{stats[tab.key]}</span>}
            </button>
          ))}
        </div>
      </div>

      {loading ? (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <div key={i} className="rounded-xl border border-surface-200 bg-white p-6 shadow-card">
              <Skeleton className="h-4 w-1/3 mb-4" />
              <Skeleton className="h-8 w-1/2 mb-3" />
              <Skeleton className="h-3 w-2/3 mb-2" />
              <Skeleton className="h-3 w-1/2 mb-2" />
              <Skeleton className="h-3 w-3/4" />
            </div>
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <EmptyState
          title={search || statusFilter !== 'all' ? 'No matching servers' : 'No MCP servers configured'}
          description={
            search || statusFilter !== 'all'
              ? 'Try different search terms or filters.'
              : 'Add your first server to start using MCP tools.'
          }
          action={
            !search && statusFilter === 'all'
              ? { label: 'Add Server', onClick: () => setShowAddModal(true) }
              : undefined
          }
        />
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {filtered.map((server) => {
            const status = statusConfig[server.status]
            return (
              <Card key={server.id}>
                <div className="cursor-pointer" onClick={() => onNavigate?.(`/mcp/servers/${server.id}`)}>
                  <CardHeader>
                    <div className="flex items-start justify-between">
                      <div className="flex-1 min-w-0">
                        <div className="flex items-center gap-2 flex-wrap">
                          <span className={cn('inline-block h-2.5 w-2.5 rounded-full', status.dot)} />
                          <CardTitle className="text-base truncate">{server.name}</CardTitle>
                          {server.transport && (
                            <span className="text-[10px] font-medium uppercase tracking-wider text-surface-400 border border-surface-200 rounded px-1.5 py-0.5 shrink-0">
                              {transportLabels[server.transport] || server.transport}
                            </span>
                          )}
                        </div>
                        <CardDescription className="mt-1 font-mono text-xs truncate">{server.url}</CardDescription>
                      </div>
                      <div className="flex items-center gap-2 shrink-0 ml-3" onClick={(e) => e.stopPropagation()}>
                        <Switch size="sm" checked={server.enabled} onCheckedChange={() => toggleServer(server)} />
                      </div>
                    </div>
                  </CardHeader>

                  <CardContent>
                    <div className="flex items-center gap-4 text-xs text-surface-500 mb-4">
                      <button
                        className="inline-flex items-center gap-1 hover:text-brand-600 transition-colors cursor-pointer"
                        onClick={(e) => {
                          e.stopPropagation()
                          setToolsServerId(server.id)
                        }}
                      >
                        <Wand2 size={12} />
                        {server.tools_count} tools
                      </button>
                      <span className="inline-flex items-center gap-1">
                        <Clock size={12} />
                        <TimeAgo date={server.last_health_check} />
                      </span>
                    </div>
                    <div className="flex items-center gap-2" onClick={(e) => e.stopPropagation()}>
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() => syncConnection(server.id)}
                        disabled={syncingId === server.id}
                      >
                        {syncingId === server.id ? (
                          <>
                            <Loader2 size={14} className="animate-spin" />
                            Syncing...
                          </>
                        ) : (
                          <>
                            <RefreshCw size={14} />
                            Sync
                          </>
                        )}
                      </Button>
                      <Button variant="outline" size="sm" onClick={() => setAccessServerId(server.id)}>
                        <Lock size={14} />
                        Access
                      </Button>
                      <Button variant="outline" size="sm" onClick={() => setEditServerId(server.id)}>
                        <Edit3 size={14} />
                        Configure
                      </Button>
                      <Button
                        variant="destructive"
                        size="sm"
                        onClick={() => {
                          if (window.confirm('Remove this MCP server? All associated tools will become unavailable.')) {
                            deleteServer(server.id)
                          }
                        }}
                      >
                        <Trash2 size={14} />
                      </Button>
                    </div>
                  </CardContent>
                </div>
              </Card>
            )
          })}
        </div>
      )}

      <ServerFormModal open={showAddModal} onClose={() => setShowAddModal(false)} onSave={addServer} />
      {editing && (
        <ServerFormModal
          open={!!editing}
          onClose={() => setEditServerId(null)}
          initial={editing}
          onSave={updateServer}
        />
      )}
      {accessServerId && (
        <GrantAccessPanel
          serverId={accessServerId}
          serverName={servers.find((s) => s.id === accessServerId)?.name ?? ''}
          onClose={() => setAccessServerId(null)}
        />
      )}
      {showConnectGuide && <ConnectGuidePanel onClose={() => setShowConnectGuide(false)} />}
      {toolsServerId && (
        <McpToolsDialog
          serverId={toolsServerId}
          serverName={servers.find((s) => s.id === toolsServerId)?.name ?? ''}
          onClose={() => setToolsServerId(null)}
        />
      )}
      {syncError && (
        <div
          className="fixed inset-0 z-50 flex items-center justify-center bg-black/40"
          onClick={() => setSyncError(null)}
        >
          <Card className="w-full max-w-lg mx-4" onClick={(e) => e.stopPropagation()}>
            <CardHeader>
              <CardTitle>Sync Failed</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-3">
                <p className="text-sm text-surface-600">{syncError.error.split(' (stderr:')[0]}</p>
                {syncError.oauth_url ? (
                  <div className="rounded-lg bg-amber-50 border border-amber-200 p-4 space-y-3">
                    <p className="text-sm font-medium text-amber-800">OAuth Authorization Required</p>
                    <p className="text-xs text-amber-700">
                      This server requires browser-based authentication. Open the link below, authorize, then come back
                      and click <strong>Sync</strong> again.
                    </p>
                    <a
                      href={syncError.oauth_url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="block text-xs font-mono text-brand-600 hover:text-brand-700 underline break-all bg-white rounded px-2 py-1.5 border border-amber-100"
                    >
                      {syncError.oauth_url}
                    </a>
                    <p className="text-xs text-amber-600">
                      The token will be cached after first authorization — second sync should succeed.
                    </p>
                  </div>
                ) : syncError.stderr ? (
                  <pre className="max-h-60 overflow-y-auto rounded-lg bg-surface-50 p-3 font-mono text-xs text-surface-700 whitespace-pre-wrap leading-relaxed">
                    {syncError.stderr}
                  </pre>
                ) : null}
                <div className="flex justify-end">
                  <Button onClick={() => setSyncError(null)}>Close</Button>
                </div>
              </div>
            </CardContent>
          </Card>
        </div>
      )}
    </div>
  )
}
