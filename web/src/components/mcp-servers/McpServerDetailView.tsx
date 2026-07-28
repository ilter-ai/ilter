import { ArrowLeft, Edit3, Loader2, Lock, Play, RefreshCw, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { api, type MCPServer, type MCPToolCallResult, type MCPToolDefinition } from '../../lib/api'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'
import { Skeleton } from '../ui/skeleton'
import { Switch } from '../ui/switch'
import { GrantAccessPanel } from './GrantAccessPanel'
import { McpToolParamFields } from './McpToolParamFields'
import { ServerFormModal } from './ServerFormModal'

const statusConfig: Record<string, { dot: string; label: string; text: string; bg: string }> = {
  online: { dot: 'bg-success', label: 'Online', text: 'text-success', bg: 'bg-success/10 border-success/20' },
  offline: { dot: 'bg-error', label: 'Offline', text: 'text-error', bg: 'bg-error/10 border-error/20' },
  error: { dot: 'bg-warning', label: 'Error', text: 'text-warning', bg: 'bg-warning/10 border-warning/20' },
}

const transportLabels: Record<string, string> = {
  sse: 'SSE',
  stdio: 'STDIO',
  inline: 'Inline',
}

function ResultContent({ content }: { content: MCPToolCallResult['content'] }) {
  return (
    <div className="space-y-2">
      {content.map((item, i) => (
        <div key={i}>
          {item.type === 'text' && (
            <pre className="whitespace-pre-wrap break-all rounded bg-surface-100 p-2 font-mono text-xs text-surface-700">
              {item.text}
            </pre>
          )}
          {item.type === 'image' && item.data && (
            <img
              src={`data:${item.mimeType || 'image/png'};base64,${item.data}`}
              alt=""
              className="max-w-full rounded border"
            />
          )}
          {item.type === 'resource' && (
            <div className="rounded border border-surface-200 bg-surface-50 p-2 text-xs">
              {item.uri && <p className="font-medium text-surface-500 mb-1">{item.uri}</p>}
              {item.text && <pre className="font-mono text-surface-700 whitespace-pre-wrap">{item.text}</pre>}
            </div>
          )}
        </div>
      ))}
    </div>
  )
}

interface Props {
  serverId: string
  onBack: () => void
}

export function McpServerDetailView({ serverId, onBack }: Props) {
  const [server, setServer] = useState<MCPServer | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [tools, setTools] = useState<MCPToolDefinition[]>([])
  const [loadingTools, setLoadingTools] = useState(true)

  const [selectedTool, setSelectedTool] = useState<MCPToolDefinition | null>(null)
  const [args, setArgs] = useState<Record<string, unknown>>({})
  const [result, setResult] = useState<MCPToolCallResult | null>(null)
  const [running, setRunning] = useState(false)

  const [syncing, setSyncing] = useState(false)

  const [showEditModal, setShowEditModal] = useState(false)
  const [accessServerId, setAccessServerId] = useState<string | null>(null)

  useEffect(() => {
    api.mcp
      .getMCPServers()
      .then((data) => {
        const raw = (Array.isArray(data) ? data : []).find((s: MCPServer) => s.id === serverId)
        if (raw) {
          const s = {
            ...raw,
            status: (raw.status || (raw.enabled === false ? 'offline' : 'online')) as 'online' | 'offline' | 'error',
          }
          setServer(s)
        } else setError('Server not found')
      })
      .catch(() => setError('Failed to load server'))
      .finally(() => setLoading(false))
  }, [serverId])

  useEffect(() => {
    setSelectedTool(null)
    setArgs({})
    setResult(null)
    api.mcp
      .getServerTools(serverId)
      .then((res) => {
        setTools(res.tools || [])
        if (res.tools?.length > 0) {
          const first = res.tools[0]
          setSelectedTool(first)
          try {
            const schema = JSON.parse(first.input_schema)
            if (schema?.properties) {
              const defaults: Record<string, unknown> = {}
              for (const [k, v] of Object.entries(schema.properties as Record<string, unknown>)) {
                const p = v as Record<string, unknown>
                if (p.default !== undefined) defaults[k] = p.default
                else if (p.type === 'string') defaults[k] = ''
                else if (p.type === 'number' || p.type === 'integer') defaults[k] = 0
                else if (p.type === 'boolean') defaults[k] = false
                else if (p.type === 'object') defaults[k] = {}
                else if (p.type === 'array') defaults[k] = []
              }
              setArgs(defaults)
            }
          } catch {
            /* ignore stale tool schema after unmount */
          }
        }
      })
      .catch(() => toast.error('Failed to load tools'))
      .finally(() => setLoadingTools(false))
  }, [serverId])

  const handleRun = useCallback(async () => {
    if (!selectedTool) return

    try {
      const schema = JSON.parse(selectedTool.input_schema)
      if (schema?.required?.length) {
        const missing = schema.required.filter((k: string) => {
          const v = args[k]
          return v === undefined || v === '' || v === null
        })
        if (missing.length > 0) {
          toast.error(`Required fields: ${missing.join(', ')}`)
          return
        }
      }
    } catch {
      /* optional schema validation, skip on parse failure */
    }

    setRunning(true)
    setResult(null)
    try {
      const res = await api.mcp.callServerTool(serverId, selectedTool.name, args)
      setResult(res)
    } catch {
      toast.error('Tool call failed')
    } finally {
      setRunning(false)
    }
  }, [selectedTool, args, serverId])

  const handleSync = useCallback(async () => {
    setSyncing(true)
    try {
      const res = await api.mcp.testMCPServer(serverId)
      if (res.status === 'online') {
        setServer((prev) =>
          prev
            ? {
                ...prev,
                status: 'online',
                tools_count: res.tools_count || 0,
                last_health_check: new Date().toISOString(),
              }
            : prev,
        )
        toast.success('Sync successful')
      } else {
        toast.error(res.error || 'Connection failed')
      }
    } catch {
      toast.error('Sync failed')
    } finally {
      setSyncing(false)
    }
  }, [serverId])

  const toggleServer = async () => {
    if (!server) return
    const newEnabled = !server.enabled
    setServer((prev) => (prev ? { ...prev, enabled: newEnabled } : prev))
    try {
      await api.mcp.updateMCPServer(server.id, { enabled: newEnabled })
      toast.success(newEnabled ? 'Server enabled' : 'Server disabled')
    } catch {
      setServer((prev) => (prev ? { ...prev, enabled: !newEnabled } : prev))
      toast.error('Failed to toggle server')
    }
  }

  if (loading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-32" />
        <Skeleton className="h-48 rounded-xl" />
      </div>
    )
  }

  if (error || !server) {
    return (
      <div className="flex flex-col items-center justify-center py-16">
        <p className="text-surface-500">{error || 'Server not found'}</p>
        <Button variant="outline" size="sm" className="mt-4" onClick={onBack}>
          <ArrowLeft size={14} className="mr-1.5" /> Back
        </Button>
      </div>
    )
  }

  const status = statusConfig[server.status] || statusConfig.offline

  return (
    <div className="space-y-6">
      <Button variant="ghost" size="sm" onClick={onBack}>
        <ArrowLeft size={16} className="mr-1.5" />
        Back to Servers
      </Button>

      {/* Server info header */}
      <Card>
        <CardHeader>
          <div className="flex items-start justify-between">
            <div className="flex-1 min-w-0">
              <div className="flex items-center gap-2">
                <span className={cn('inline-block h-3 w-3 rounded-full', status.dot)} />
                <CardTitle className="text-lg truncate">{server.name}</CardTitle>
                {server.transport && (
                  <span className="text-[10px] font-medium uppercase tracking-wider text-surface-400 border border-surface-200 rounded px-1.5 py-0.5">
                    {transportLabels[server.transport] || server.transport}
                  </span>
                )}
              </div>
              {server.url && <CardDescription className="mt-1 font-mono text-xs">{server.url}</CardDescription>}
            </div>
            <Switch size="sm" checked={server.enabled !== false} onCheckedChange={toggleServer} />
          </div>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={handleSync} disabled={syncing}>
              {syncing ? <Loader2 size={14} className="animate-spin mr-1" /> : <RefreshCw size={14} className="mr-1" />}
              {syncing ? 'Syncing...' : 'Sync'}
            </Button>
            <Button variant="outline" size="sm" onClick={() => setShowEditModal(true)}>
              <Edit3 size={14} className="mr-1" /> Configure
            </Button>
            <Button variant="outline" size="sm" onClick={() => setAccessServerId(serverId)}>
              <Lock size={14} className="mr-1" /> Access
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={() => {
                if (!confirm('Delete this server? All associated tools will be removed.')) return
                api.mcp
                  .deleteMCPServer(server.id)
                  .then(() => {
                    toast.success('Server deleted')
                    onBack()
                  })
                  .catch(() => toast.error('Failed to delete server'))
              }}
            >
              <Trash2 size={14} className="mr-1" /> Delete
            </Button>
          </div>
        </CardContent>
      </Card>

      {showEditModal && (
        <ServerFormModal
          open={showEditModal}
          onClose={() => setShowEditModal(false)}
          initial={server}
          onSave={(data) => {
            if (!server) return
            const prev = server
            setServer({
              ...server,
              name: data.name,
              url: data.url,
              transport: data.transport,
              command: data.command,
              args: data.args,
              env: data.env ?? '',
            })
            setShowEditModal(false)
            api.mcp
              .updateMCPServer(server.id, {
                name: data.name,
                url: data.url,
                transport: data.transport,
                command: data.command,
                args: data.args,
                env: data.env ?? '',
              })
              .then(() => toast.success('Server updated'))
              .catch(() => {
                setServer(prev)
                toast.error('Failed to update server')
              })
          }}
        />
      )}
      {accessServerId && (
        <GrantAccessPanel
          serverId={accessServerId}
          serverName={server?.name ?? ''}
          onClose={() => setAccessServerId(null)}
        />
      )}

      {/* Tool list + invocation */}
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Tools</CardTitle>
        </CardHeader>
        <CardContent>
          {loadingTools ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 size={20} className="animate-spin text-surface-400" />
            </div>
          ) : tools.length === 0 ? (
            <p className="text-sm text-surface-500 py-4 text-center">No tools found. Use "Sync" to sync tools first.</p>
          ) : (
            <div className="flex gap-4 min-h-0">
              {/* Tool list sidebar */}
              <div className="w-48 shrink-0 space-y-1">
                {tools.map((tool) => (
                  <button
                    key={tool.id}
                    onClick={() => {
                      setSelectedTool(tool)
                      setResult(null)
                      try {
                        const schema = JSON.parse(tool.input_schema)
                        if (schema?.properties) {
                          const defaults: Record<string, unknown> = {}
                          for (const [k, v] of Object.entries(schema.properties as Record<string, unknown>)) {
                            const p = v as Record<string, unknown>
                            if (p.default !== undefined) defaults[k] = p.default
                            else if (p.type === 'string') defaults[k] = ''
                            else if (p.type === 'number' || p.type === 'integer') defaults[k] = 0
                            else if (p.type === 'boolean') defaults[k] = false
                            else if (p.type === 'object') defaults[k] = {}
                            else if (p.type === 'array') defaults[k] = []
                          }
                          setArgs(defaults)
                        } else {
                          setArgs({})
                        }
                      } catch {
                        setArgs({})
                      }
                    }}
                    className={cn(
                      'w-full text-left px-3 py-2 rounded-lg text-sm transition-colors',
                      selectedTool?.id === tool.id
                        ? 'bg-brand-50 text-brand-700 font-medium'
                        : 'text-surface-600 hover:bg-surface-50',
                    )}
                  >
                    <span className="block truncate font-mono text-xs">{tool.name}</span>
                    {tool.description && (
                      <span className="block text-[11px] text-surface-400 truncate mt-0.5">{tool.description}</span>
                    )}
                  </button>
                ))}
              </div>

              {/* Invocation panel */}
              <div className="flex-1 min-w-0">
                {selectedTool && (
                  <>
                    <div className="mb-3">
                      <h3 className="text-sm font-medium text-surface-900">{selectedTool.name}</h3>
                      {selectedTool.description && (
                        <p className="text-xs text-surface-500 mt-0.5">{selectedTool.description}</p>
                      )}
                    </div>

                    {(() => {
                      const schema = (() => {
                        try {
                          return JSON.parse(selectedTool.input_schema)
                        } catch {
                          return null
                        }
                      })()
                      if (schema) {
                        return <McpToolParamFields schema={schema} value={args} onChange={setArgs} />
                      }
                      return (
                        <div>
                          <label className="block text-[11px] font-medium text-surface-400 mb-1">Arguments</label>
                          <textarea
                            value={JSON.stringify(args, null, 2)}
                            onChange={(e) => {
                              try {
                                setArgs(JSON.parse(e.target.value))
                              } catch {
                                /* partial JSON while editing */
                              }
                            }}
                            rows={4}
                            className="w-full rounded-lg border border-surface-300 bg-[#1e1e1e] px-3 py-2 font-mono text-xs text-green-300 placeholder-surface-500 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 resize-none"
                            spellCheck={false}
                          />
                        </div>
                      )
                    })()}

                    <div className="flex justify-end mt-3">
                      <Button onClick={handleRun} disabled={running}>
                        {running ? (
                          <>
                            <Loader2 size={14} className="animate-spin mr-1" /> Running...
                          </>
                        ) : (
                          <>
                            <Play size={14} className="mr-1" /> Run
                          </>
                        )}
                      </Button>
                    </div>

                    {result && (
                      <div
                        className={cn(
                          'mt-4 rounded-lg border p-3',
                          result.isError ? 'border-error/30 bg-error/5' : 'border-surface-200 bg-surface-50',
                        )}
                      >
                        <span className={cn('text-xs font-medium', result.isError ? 'text-error' : 'text-success')}>
                          {result.isError ? 'Error' : 'Success'}
                        </span>
                        <div className="mt-2">
                          <ResultContent content={result.content} />
                        </div>
                      </div>
                    )}
                  </>
                )}
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
