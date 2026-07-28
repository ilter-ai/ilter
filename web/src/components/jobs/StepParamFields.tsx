import { Info, Server } from 'lucide-react'
import { useEffect, useRef } from 'react'
import type { JobStep, MCPServer, MCPToolDefinition, ModelProvider, PromptTemplate } from '../../lib/api'
import { ModelSelector } from '../chat/ModelSelector'
import { McpToolParamFields } from '../mcp-servers/McpToolParamFields'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../ui/select'

interface Props {
  step: JobStep
  stepIndex: number
  onUpdate: (step: JobStep) => void
  templates: PromptTemplate[]
  models: ModelProvider[]
  mcpServers: MCPServer[]
  selectedServerTools: Record<string, MCPToolDefinition[]>
  loadingTools: Record<string, boolean>
  onLoadServerTools: (serverId: string) => void
  variableValues: Record<string, string>
  onVariableChange: (key: string, value: string) => void
}

function parseToolName(stored: string): { serverId: string; toolName: string } {
  const parts = stored.split('__', 2)
  return parts.length === 2 ? { serverId: parts[0], toolName: parts[1] } : { serverId: '', toolName: parts[0] }
}

function RefsBar({
  refs,
  stepIndex,
  onInsert,
}: {
  refs: { token: string; label: string }[]
  stepIndex: number
  onInsert: (token: string) => void
}) {
  if (refs.length === 0) return null
  return (
    <div className="flex flex-wrap items-center gap-1 mt-1.5 text-[11px] text-surface-400">
      <Info size={12} className="shrink-0" />
      <span className="mr-0.5">Insert:</span>
      {refs.map((r) => (
        <button
          key={r.token}
          type="button"
          title={r.label}
          onMouseDown={(e) => e.preventDefault()}
          onClick={() => onInsert(r.token)}
          className="rounded-md bg-surface-100 px-1.5 py-0.5 font-mono text-[11px] text-surface-600 hover:bg-surface-200 hover:text-surface-800 transition-colors cursor-pointer"
        >
          {r.token}
        </button>
      ))}
      {stepIndex > 0 && (
        <span title="Append .field to read one key from a step result, e.g. {{.step0.id}}">· add .field</span>
      )}
    </div>
  )
}

export function StepParamFields({
  step,
  stepIndex,
  onUpdate,
  templates,
  models,
  mcpServers,
  selectedServerTools,
  loadingTools,
  onLoadServerTools,
  variableValues,
  onVariableChange,
}: Props) {
  const { serverId, toolName } = parseToolName(step.tool || '')

  const loadedRef = useRef(new Set<string>())

  useEffect(() => {
    if (serverId && !loadedRef.current.has(serverId)) {
      loadedRef.current.add(serverId)
      onLoadServerTools(serverId)
    }
  }, [serverId, onLoadServerTools])

  const insertAtCursor = (token: string) => {
    const el = document.activeElement
    if (!(el instanceof HTMLTextAreaElement || el instanceof HTMLInputElement) || typeof el.setRangeText !== 'function')
      return
    const start = el.selectionStart ?? el.value.length
    const end = el.selectionEnd ?? el.value.length
    el.setRangeText(token, start, end, 'end')
    el.dispatchEvent(new Event('input', { bubbles: true }))
  }

  const varToken = (name: string) => (/^[A-Za-z_]\w*$/.test(name) ? `{{.${name}}}` : `{{v ${JSON.stringify(name)}}}`)

  const refs: { token: string; label: string }[] = []
  Object.keys(variableValues).forEach((k) => {
    refs.push({ token: varToken(k), label: k })
  })
  for (let i = 0; i < stepIndex; i++) {
    refs.push({ token: `{{.step${i}}}`, label: `step ${i} result` })
  }
  if (stepIndex > 0) {
    refs.push({ token: `{{.prev}}`, label: `previous step result` })
  }

  if (step.type === 'llm') {
    return (
      <div className="space-y-2">
        <div>
          <label className="block text-[11px] font-medium text-surface-400 mb-1">Prompt Template</label>
          <Select
            value={step.prompt_id !== undefined ? String(step.prompt_id) : undefined}
            onValueChange={(val) => onUpdate({ ...step, prompt_id: Number(val) })}
          >
            <SelectTrigger className="w-full h-8 text-xs">
              <SelectValue placeholder="Select a prompt template">
                {step.prompt_id !== undefined &&
                  (() => {
                    const t = templates.find((t) => t.id === String(step.prompt_id))
                    return t ? t.name : String(step.prompt_id)
                  })()}
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              {templates.map((t) => (
                <SelectItem key={t.id} value={t.id}>
                  <span className="font-medium">{t.name}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div>
          <label className="block text-[11px] font-medium text-surface-400 mb-1">Model</label>
          <ModelSelector
            models={models}
            value={step.model ?? ''}
            onChange={(val) => onUpdate({ ...step, model: val })}
          />
        </div>

        {Object.keys(variableValues).length > 0 && (
          <div>
            <label className="block text-[11px] font-medium text-surface-400 mb-1">Variables</label>
            <div className="space-y-1.5">
              {Object.entries(variableValues).map(([key, val]) => (
                <div key={key} className="flex items-center gap-2">
                  <span className="text-[11px] font-mono font-semibold text-surface-500 min-w-[80px] uppercase tracking-wide">
                    {key}
                  </span>
                  <input
                    type="text"
                    value={val}
                    onChange={(e) => onVariableChange(key, e.target.value)}
                    placeholder={`Value for ${key}`}
                    className="flex-1 h-7 rounded-md border border-surface-300 bg-white px-2 text-xs text-surface-700 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                  />
                </div>
              ))}
            </div>
          </div>
        )}
        <RefsBar refs={refs} stepIndex={stepIndex} onInsert={insertAtCursor} />
      </div>
    )
  }

  return (
    <div className="space-y-2">
      <div className="flex gap-2">
        <div className="flex-1">
          <label className="block text-[11px] font-medium text-surface-400 mb-1">Server</label>
          <Select
            value={serverId}
            onValueChange={(sid) => {
              const s = sid ?? ''
              onLoadServerTools(s)
              onUpdate({ ...step, tool: s ? `${s}__${toolName}` : toolName })
            }}
          >
            <SelectTrigger className="h-7 w-full text-xs">
              <SelectValue placeholder="Select server" />
            </SelectTrigger>
            <SelectContent>
              {mcpServers.map((sv) => (
                <SelectItem key={sv.id} value={sv.id}>
                  <span className="flex items-center gap-1.5">
                    <Server size={12} className="text-surface-400 shrink-0" />
                    {sv.name}
                  </span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="flex-1">
          <label className="block text-[11px] font-medium text-surface-400 mb-1">Tool</label>
          <Select
            value={toolName}
            onValueChange={(name) =>
              onUpdate({ ...step, tool: serverId ? `${serverId}__${name ?? ''}` : (name ?? '') })
            }
            disabled={!serverId || loadingTools[serverId]}
          >
            <SelectTrigger className="h-7 w-full text-xs">
              <SelectValue
                placeholder={loadingTools[serverId] ? 'Loading...' : !serverId ? 'Select server first' : 'Select tool'}
              />
            </SelectTrigger>
            <SelectContent>
              {(selectedServerTools[serverId] || []).map((t) => (
                <SelectItem key={t.name} value={t.name}>
                  <span className="font-mono text-xs">{t.name}</span>
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </div>

      <div>
        {(() => {
          const tool = serverId ? (selectedServerTools[serverId] || []).find((t) => t.name === toolName) : undefined
          const schema = tool
            ? (() => {
                try {
                  return JSON.parse(tool.input_schema)
                } catch {
                  // JSON.parse only throws SyntaxError
                  return null
                }
              })()
            : null
          const parsedArgs = (() => {
            try {
              return JSON.parse(step.arguments || '{}')
            } catch {
              // JSON.parse only throws SyntaxError
              return {}
            }
          })()

          if (tool && schema) {
            return (
              <>
                <McpToolParamFields
                  schema={schema}
                  value={parsedArgs}
                  onChange={(v) => onUpdate({ ...step, arguments: JSON.stringify(v) })}
                />
                <RefsBar refs={refs} stepIndex={stepIndex} onInsert={insertAtCursor} />
              </>
            )
          }

          return (
            <div>
              <label className="block text-[11px] font-medium text-surface-400 mb-1">Arguments</label>
              {serverId ? (
                <div className="flex items-center justify-center h-20 rounded-lg border border-dashed border-surface-300 bg-surface-50 text-xs text-surface-400">
                  Select a tool to configure arguments
                </div>
              ) : (
                <div className="flex items-center justify-center h-20 rounded-lg border border-dashed border-surface-300 bg-surface-50 text-xs text-surface-400">
                  Select a server and tool to configure arguments
                </div>
              )}
            </div>
          )
        })()}
      </div>
    </div>
  )
}
