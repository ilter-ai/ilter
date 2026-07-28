import { Play } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { api, type MCPToolCallResult, type MCPToolDefinition } from '../../lib/api'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { Loader2, X } from '../ui/icons'

interface Props {
  serverId: string
  serverName: string
  onClose: () => void
}

function schemaToPreview(schema: string): string {
  if (!schema) return '{}'
  try {
    const parsed = JSON.parse(schema)
    const properties = parsed.properties as Record<string, { type?: string; default?: unknown }> | undefined
    if (!properties || Object.keys(properties).length === 0) return '{}'
    const defaults: Record<string, unknown> = {}
    for (const [key, val] of Object.entries(properties)) {
      if (val.default !== undefined) {
        defaults[key] = val.default
      } else if (val.type === 'string') {
        defaults[key] = ''
      } else if (val.type === 'number' || val.type === 'integer') {
        defaults[key] = 0
      } else if (val.type === 'boolean') {
        defaults[key] = false
      } else if (val.type === 'array') {
        defaults[key] = []
      } else if (val.type === 'object') {
        defaults[key] = {}
      } else {
        defaults[key] = null
      }
    }
    return JSON.stringify(defaults, null, 2)
  } catch {
    return '{}'
  }
}

function ResultContent({ content }: { content: MCPToolCallResult['content'] }) {
  return (
    <div className="space-y-2">
      {content.map((item, i) => (
        <div key={i}>
          {item.type === 'text' && (
            <pre className="whitespace-pre-wrap break-all rounded-lg bg-surface-50 p-3 font-mono text-xs text-surface-800">
              {item.text}
            </pre>
          )}
          {item.type === 'image' && item.data && (
            <img
              src={`data:${item.mimeType || 'image/png'};base64,${item.data}`}
              alt="Tool result"
              className="max-w-full rounded-lg border border-surface-200"
            />
          )}
          {item.type === 'resource' && (
            <div className="rounded-lg border border-surface-200 bg-surface-50 p-3">
              {item.uri && <p className="mb-1 text-xs font-medium text-surface-500">{item.uri}</p>}
              {item.text && <pre className="whitespace-pre-wrap font-mono text-xs text-surface-800">{item.text}</pre>}
              {item.data && (
                <p className="text-xs text-surface-400">
                  Binary data ({item.mimeType || 'unknown'}, {item.data.length} bytes)
                </p>
              )}
            </div>
          )}
        </div>
      ))}
    </div>
  )
}

export function McpToolsDialog({ serverId, serverName, onClose }: Props) {
  const [tools, setTools] = useState<MCPToolDefinition[]>([])
  const [selected, setSelected] = useState<MCPToolDefinition | null>(null)
  const [args, setArgs] = useState('{}')
  const [loading, setLoading] = useState(true)
  const [result, setResult] = useState<MCPToolCallResult | null>(null)
  const [running, setRunning] = useState(false)

  useEffect(() => {
    api.mcp
      .getServerTools(serverId)
      .then((res) => {
        setTools(res.tools || [])
        if (res.tools && res.tools.length > 0) {
          const first = res.tools[0]
          setSelected(first)
          setArgs(schemaToPreview(first.input_schema))
        }
      })
      .catch(() => toast.error('Failed to load tools'))
      .finally(() => setLoading(false))
  }, [serverId])

  const handleSelect = useCallback((tool: MCPToolDefinition) => {
    setSelected(tool)
    setArgs(schemaToPreview(tool.input_schema))
    setResult(null)
  }, [])

  const handleRun = useCallback(async () => {
    if (!selected) return
    setRunning(true)
    setResult(null)
    try {
      let parsedArgs: Record<string, unknown>
      try {
        parsedArgs = JSON.parse(args)
      } catch {
        toast.error('Invalid JSON in arguments')
        setRunning(false)
        return
      }
      const res = await api.mcp.callServerTool(serverId, selected.name, parsedArgs)
      setResult(res)
    } catch {
      toast.error('Tool call failed')
    } finally {
      setRunning(false)
    }
  }, [selected, args, serverId])

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={onClose}>
      <Card className="w-full max-w-3xl mx-4 max-h-[85vh] flex flex-col" onClick={(e) => e.stopPropagation()}>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle className="text-base">Tools · {serverName}</CardTitle>
            <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close">
              <X size={16} />
            </Button>
          </div>
        </CardHeader>
        <CardContent className="flex-1 overflow-hidden flex flex-col">
          {loading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 size={24} className="animate-spin text-surface-400" />
            </div>
          ) : tools.length === 0 ? (
            <p className="py-12 text-center text-sm text-surface-500">
              No tools found. Use {'"Test"'} to sync tools first.
            </p>
          ) : (
            <div className="flex gap-4 flex-1 min-h-0">
              {/* Tool list sidebar */}
              <div className="w-48 shrink-0 overflow-y-auto space-y-1">
                {tools.map((tool) => (
                  <button
                    key={tool.id}
                    onClick={() => handleSelect(tool)}
                    className={`w-full text-left px-3 py-2 rounded-lg text-sm transition-colors ${
                      selected?.id === tool.id
                        ? 'bg-brand-50 text-brand-700 font-medium'
                        : 'text-surface-600 hover:bg-surface-50'
                    }`}
                  >
                    <span className="block truncate font-mono text-xs">{tool.name}</span>
                    {tool.description && (
                      <span className="block text-[11px] text-surface-400 truncate mt-0.5">{tool.description}</span>
                    )}
                  </button>
                ))}
              </div>

              {/* Tool detail / invocation panel */}
              <div className="flex-1 flex flex-col min-w-0">
                {selected && (
                  <>
                    <div className="mb-3">
                      <h3 className="text-sm font-medium text-surface-900">{selected.name}</h3>
                      {selected.description && (
                        <p className="text-xs text-surface-500 mt-0.5">{selected.description}</p>
                      )}
                    </div>

                    <label className="mb-1 block text-xs font-medium text-surface-600">Arguments (JSON)</label>
                    <textarea
                      value={args}
                      onChange={(e) => setArgs(e.target.value)}
                      className="w-full h-32 rounded-lg border border-surface-300 bg-[#1e1e1e] px-3 py-2 font-mono text-xs text-green-300 placeholder-surface-500 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 resize-none"
                      spellCheck={false}
                      placeholder='{ "param": "value" }'
                    />
                    <p className="mt-1 text-[11px] text-surface-400">
                      Use JSON. Schema: {selected.input_schema || 'no schema defined'}
                    </p>

                    <div className="flex justify-end mt-3">
                      <Button onClick={handleRun} disabled={running}>
                        {running ? (
                          <>
                            <Loader2 size={14} className="animate-spin" />
                            Running...
                          </>
                        ) : (
                          <>
                            <Play size={14} />
                            Run
                          </>
                        )}
                      </Button>
                    </div>

                    {result && (
                      <div
                        className={`mt-4 rounded-lg border p-3 ${result.isError ? 'border-error/30 bg-error/5' : 'border-surface-200 bg-surface-50'}`}
                      >
                        <div className="flex items-center gap-2 mb-2">
                          <span className={`text-xs font-medium ${result.isError ? 'text-error' : 'text-success'}`}>
                            {result.isError ? 'Error' : 'Success'}
                          </span>
                        </div>
                        <ResultContent content={result.content} />
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
