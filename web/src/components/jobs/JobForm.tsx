import { QueryClientProvider } from '@tanstack/react-query'
import { toString as cronToString } from 'cronstrue'
import { GripVertical, KeyRound, Loader2, Plus, Trash2 } from 'lucide-react'
import { useCallback, useEffect, useState } from 'react'
import {
  type APIKey,
  api,
  type Job,
  type JobStep,
  type MCPServer,
  type ModelProvider,
  type PromptTemplate,
  type Trigger,
} from '../../lib/api'
import { queryClient } from '../../lib/queryClient'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { Input } from '../ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../ui/select'
import { Textarea } from '../ui/textarea'
import { StepParamFields } from './StepParamFields'

interface JobFormProps {
  initialData?: Partial<Job>
  onSubmit: (data: Partial<Job>) => Promise<void>
  onCancel?: () => void
}

const CRON_PRESETS = [
  { label: 'Every hour', value: '0 * * * *' },
  { label: 'Daily at 9am', value: '0 9 * * *' },
  { label: 'Every 30 min', value: '*/30 * * * *' },
] as const

function isValidCron(expr: string): boolean {
  try {
    cronToString(expr.trim())
    return true
  } catch {
    return false
  }
}

function parseVariablesConfig(raw: Record<string, unknown> | null | undefined): Record<string, string> {
  if (!raw || typeof raw !== 'object') return {}
  const flat: Record<string, string> = {}
  for (const [k, v] of Object.entries(raw)) {
    flat[k] = typeof v === 'object' && v !== null ? JSON.stringify(v) : String(v ?? '')
  }
  return flat
}

function serializeVariablesConfig(vars: Record<string, string>): Record<string, unknown> | undefined {
  const entries = Object.entries(vars).filter(([, v]) => String(v ?? '').trim())
  if (entries.length === 0) return undefined
  const obj: Record<string, unknown> = {}
  for (const [key, val] of entries) {
    // keep as string, do not JSON.parse — that creates nested objects
    obj[key] = String(val ?? '').trim()
  }
  return obj
}

function parseSteps(raw: string | undefined): JobStep[] {
  if (!raw) return [{ type: 'llm', prompt_id: undefined, model: '' }]
  try {
    const parsed = JSON.parse(raw)
    if (Array.isArray(parsed)) {
      return parsed.map((s: unknown) => {
        // guard against null step entries — ...null throws
        if (!s || typeof s !== 'object') return { type: 'llm', prompt_id: undefined, model: '' } as JobStep
        const step = s as Record<string, unknown>
        return {
          ...step,
          arguments:
            typeof step.arguments === 'object' && step.arguments !== null
              ? JSON.stringify(step.arguments)
              : step.arguments,
        } as JobStep
      })
    }
    return [{ type: 'llm', prompt_id: undefined, model: '' }]
  } catch {
    return [{ type: 'llm', prompt_id: undefined, model: '' }]
  }
}

function parseJsonArg(raw: string | undefined): unknown {
  if (!raw) return raw
  try {
    return JSON.parse(raw)
  } catch {
    return raw
  }
}

function serializeSteps(steps: JobStep[]): string | undefined {
  if (steps.length === 0) return undefined
  return JSON.stringify(
    steps.map((s) => {
      if (s.type === 'mcp') return { type: 'mcp', tool: s.tool, arguments: parseJsonArg(s.arguments) }
      return { type: 'llm', prompt_id: s.prompt_id, model: s.model }
    }),
  )
}

interface TriggerInput {
  // For existing triggers this is the real backend trigger ID; for triggers
  // added client-side this session it's just a local React key. isNew tells
  // handleSubmit which case it is, since the backend generates real IDs (and
  // the webhook token/secret) itself — it never accepts a client-supplied ID.
  id: string
  isNew: boolean
  kind: 'cron' | 'webhook'
  config: string
}

function generateId(): string {
  if (typeof crypto !== 'undefined' && crypto.randomUUID) return crypto.randomUUID()
  return `tr_${Date.now()}_${Math.random().toString(36).slice(2, 9)}`
}

function parseCronConfig(config: string): { expr?: string; timezone?: string } {
  try {
    return JSON.parse(config) as { expr?: string; timezone?: string }
  } catch {
    return {}
  }
}

function makeCronConfig(expr: string, timezone = 'UTC'): string {
  return JSON.stringify({ expr, timezone })
}

function getCronExpr(config: string): string {
  const c = parseCronConfig(config)
  return c.expr ?? ''
}

function setCronExpr(config: string, expr: string): string {
  const c = parseCronConfig(config)
  return makeCronConfig(expr, c.timezone ?? 'UTC')
}

function parseTriggersFromInitial(data: Partial<Job> | undefined): TriggerInput[] {
  const existing = (data as Record<string, unknown>)?.triggers
  if (existing && Array.isArray(existing) && existing.length > 0) {
    return existing.map((t: Record<string, unknown>) => ({
      id: typeof t.id === 'string' && t.id ? t.id : generateId(),
      isNew: false,
      kind: t.kind === 'webhook' ? 'webhook' : 'cron',
      config: typeof t.config === 'string' ? t.config : JSON.stringify(t.config),
    }))
  }
  // Fall back to legacy cron_expr field for jobs created before triggers
  const legacyExpr = (data as Record<string, unknown>)?.cron_expr
  if (typeof legacyExpr === 'string' && legacyExpr.trim()) {
    return [{ id: generateId(), isNew: true, kind: 'cron', config: makeCronConfig(legacyExpr.trim()) }]
  }
  // Default: start with a cron trigger so the form is never empty
  return [{ id: generateId(), isNew: true, kind: 'cron', config: makeCronConfig('0 * * * *') }]
}

export function JobForm({ initialData, onSubmit, onCancel }: JobFormProps) {
  const [name, setName] = useState(initialData?.name ?? '')
  const [description, setDescription] = useState(initialData?.description ?? '')
  const [triggers, setTriggers] = useState<TriggerInput[]>(() => parseTriggersFromInitial(initialData))
  const [steps, setSteps] = useState<JobStep[]>(() => parseSteps(initialData?.steps))
  const [variableValues, setVariableValues] = useState<Record<string, string>>(() =>
    parseVariablesConfig(initialData?.variables_config),
  )
  const [timeoutMs, setTimeoutMs] = useState(initialData?.timeout_ms ?? 120000)
  const [apiKeyId, setApiKeyId] = useState(initialData?.api_key_id ?? '')
  const [saving, setSaving] = useState(false)
  const [submitError, setSubmitError] = useState<string | null>(null)

  const [templates, setTemplates] = useState<PromptTemplate[]>([])
  const [_loadingTemplates, setLoadingTemplates] = useState(true)
  const [models, setModels] = useState<ModelProvider[]>([])
  const [_loadingModels, setLoadingModels] = useState(true)
  const [apiKeys, setApiKeys] = useState<APIKey[]>([])
  const [loadingApiKeys, setLoadingApiKeys] = useState(true)
  const [mcpServers, setMcpServers] = useState<MCPServer[]>([])
  const [selectedServerTools, setSelectedServerTools] = useState<
    Record<string, import('../../lib/api').MCPToolDefinition[]>
  >({})
  const [loadingTools, setLoadingTools] = useState<Record<string, boolean>>({})

  useEffect(() => {
    api.prompts
      .getPromptTemplates()
      .then(setTemplates)
      .catch(() => {})
      .finally(() => setLoadingTemplates(false))
  }, [])

  useEffect(() => {
    api.models
      .getModelProviders()
      .then(setModels)
      .catch(() => {})
      .finally(() => setLoadingModels(false))
  }, [])

  useEffect(() => {
    api.apiKeys
      .getAPIKeys()
      .then(setApiKeys)
      .catch(() => {})
      .finally(() => setLoadingApiKeys(false))
  }, [])

  useEffect(() => {
    api.mcp
      .getMCPServers()
      .then(setMcpServers)
      .catch(() => {})
  }, [])

  const loadServerTools = useCallback(
    (serverId: string) => {
      if (selectedServerTools[serverId]) return
      setLoadingTools((prev) => ({ ...prev, [serverId]: true }))
      api.mcp
        .getServerTools(serverId)
        .then((result) => {
          setSelectedServerTools((prev) => ({ ...prev, [serverId]: result.tools }))
        })
        .catch(() => {
          setSelectedServerTools((prev) => ({ ...prev, [serverId]: [] }))
        })
        .finally(() => {
          setLoadingTools((prev) => ({ ...prev, [serverId]: false }))
        })
    },
    [selectedServerTools],
  )

  // Collect variables from all LLM steps' selected templates
  // and merge them with any variables already set from the job config.
  useEffect(() => {
    const allVars = new Set<string>()
    for (const s of steps) {
      if (s.type === 'llm' && s.prompt_id !== undefined) {
        // Compare as strings: API returns t.id as number but typed as string.
        const t = templates.find((t) => String(t.id) === String(s.prompt_id))
        if (t && Array.isArray(t.variables)) t.variables.forEach((v) => allVars.add(v))
      }
    }
    setVariableValues((prev) => {
      // If no template variables were found, keep existing values (from job config).
      if (allVars.size === 0) return prev
      const next: Record<string, string> = { ...prev }
      for (const v of allVars) {
        if (!(v in next)) next[v] = ''
      }
      return next
    })
  }, [steps, templates])

  const updateStep = useCallback((i: number, s: JobStep) => {
    setSteps((prev) => {
      const next = [...prev]
      next[i] = s
      return next
    })
  }, [])

  const removeStep = useCallback((i: number) => {
    setSteps((prev) => prev.filter((_, idx) => idx !== i))
  }, [])

  const addStep = useCallback(() => {
    setSteps((prev) => [...prev, { type: 'mcp', tool: '', arguments: '' }])
  }, [])

  // ── Trigger callbacks ──

  const addCronTrigger = useCallback(() => {
    setTriggers((prev) => [
      ...prev,
      { id: generateId(), isNew: true, kind: 'cron', config: makeCronConfig('0 * * * *') },
    ])
  }, [])

  const addWebhookTrigger = useCallback(() => {
    setTriggers((prev) => [...prev, { id: generateId(), isNew: true, kind: 'webhook', config: JSON.stringify({}) }])
  }, [])

  const removeTrigger = useCallback((id: string) => {
    setTriggers((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const updateTriggerConfig = useCallback((id: string, config: string) => {
    setTriggers((prev) => prev.map((t) => (t.id === id ? { ...t, config } : t)))
  }, [])

  const errors: Record<string, string> = {}
  if (!name.trim()) errors.name = 'Name is required'
  if (triggers.length === 0) errors.triggers = 'At least one trigger is required'
  for (const t of triggers) {
    if (t.kind === 'cron') {
      const expr = getCronExpr(t.config)
      if (!expr.trim() || !isValidCron(expr)) {
        errors[`cron_${t.id}`] = 'Invalid cron expression (e.g. 0 * * * *)'
      }
    }
  }

  const hasErrors = Object.keys(errors).length > 0

  const handleSubmit = async () => {
    if (hasErrors || saving) return
    setSaving(true)
    setSubmitError(null)
    try {
      const serializedSteps = serializeSteps(steps)
      await onSubmit({
        name: name.trim(),
        description: description.trim() || undefined,
        triggers: triggers.map((t) => ({
          id: t.isNew ? undefined : t.id,
          kind: t.kind,
          config: t.config,
        })) as Trigger[],
        variables_config: serializeVariablesConfig(variableValues),
        steps: serializedSteps,
        timeout_ms: timeoutMs,
        api_key_id: apiKeyId || undefined,
      })
    } catch (err) {
      setSubmitError(err instanceof Error ? err.message : 'Failed to save job')
      setSaving(false)
    }
  }

  return (
    <QueryClientProvider client={queryClient}>
      <div className="space-y-5">
        <div>
          <label className="block text-xs font-medium text-surface-500 mb-1">
            Name <span className="text-error">*</span>
          </label>
          <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. morning-summary" />
          {errors.name && <p className="text-xs text-error mt-1">{errors.name}</p>}
        </div>

        <div>
          <label className="block text-xs font-medium text-surface-500 mb-1">Description</label>
          <Textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="Optional description"
            rows={2}
          />
        </div>

        <div>
          <div className="flex items-center justify-between mb-2">
            <label className="block text-xs font-medium text-surface-500">Triggers</label>
            <div className="flex gap-1.5">
              <Button
                variant="outline"
                size="xs"
                onClick={addCronTrigger}
                disabled={triggers.some((t) => t.kind === 'cron')}
              >
                <Plus size={14} className="mr-1" /> Cron
              </Button>
              <Button
                variant="outline"
                size="xs"
                onClick={addWebhookTrigger}
                disabled={triggers.some((t) => t.kind === 'webhook')}
              >
                <Plus size={14} className="mr-1" /> Webhook
              </Button>
            </div>
          </div>
          {errors.triggers && <p className="text-xs text-error mb-2">{errors.triggers}</p>}
          <div className="space-y-3">
            {triggers.map((t) => (
              <div key={t.id} className="rounded-lg border border-surface-200 bg-white p-3">
                <div className="flex items-center justify-between mb-2">
                  <div className="flex items-center gap-2">
                    <span
                      className={cn(
                        'inline-flex items-center rounded-full px-2 py-0.5 text-[11px] font-semibold uppercase tracking-wider',
                        t.kind === 'cron'
                          ? 'bg-amber-50 text-amber-700 border border-amber-200'
                          : 'bg-sky-50 text-sky-700 border border-sky-200',
                      )}
                    >
                      {t.kind === 'cron' ? 'Cron' : 'Webhook'}
                    </span>
                  </div>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => removeTrigger(t.id)}
                    className="text-surface-400 hover:text-error"
                  >
                    <Trash2 size={14} />
                  </Button>
                </div>

                {t.kind === 'cron' ? (
                  <>
                    <Input
                      value={getCronExpr(t.config)}
                      onChange={(e) => updateTriggerConfig(t.id, setCronExpr(t.config, e.target.value))}
                      placeholder="e.g. 0 * * * *"
                    />
                    <div className="flex flex-wrap gap-1.5 mt-1.5">
                      {CRON_PRESETS.map((preset) => {
                        const currentExpr = getCronExpr(t.config)
                        return (
                          <Button
                            key={preset.value}
                            variant="outline"
                            size="xs"
                            onClick={() => updateTriggerConfig(t.id, setCronExpr(t.config, preset.value))}
                            className={cn(
                              currentExpr === preset.value && 'border-brand-500 bg-brand-50 text-brand-700',
                            )}
                          >
                            {preset.label}
                          </Button>
                        )
                      })}
                    </div>
                    {errors[`cron_${t.id}`] && <p className="text-xs text-error mt-1">{errors[`cron_${t.id}`]}</p>}
                  </>
                ) : (
                  <div className="space-y-2.5">
                    {t.isNew ? (
                      <p className="text-[11px] text-surface-500 leading-relaxed">
                        The webhook token and signing secret will be generated when you save — you'll get one chance to
                        copy them from the confirmation banner, so keep it open until you have.
                      </p>
                    ) : (
                      <p className="text-[11px] text-surface-500 leading-relaxed">
                        Webhook trigger — the token and signing secret were shown once when this trigger was created and
                        can't be retrieved again. To rotate credentials, remove this trigger and add a new one.
                      </p>
                    )}
                  </div>
                )}
              </div>
            ))}
          </div>
        </div>

        <div>
          <div className="flex items-center justify-between mb-2">
            <label className="block text-xs font-medium text-surface-500">Steps</label>
            <Button variant="outline" size="xs" onClick={addStep}>
              <Plus size={14} className="mr-1" /> Add Step
            </Button>
          </div>
          <div className="space-y-3">
            {steps.map((s, i) => (
              <div key={i} className="rounded-lg border border-surface-200 bg-white p-3">
                <div className="flex items-center justify-between mb-2">
                  <div className="flex items-center gap-2">
                    <GripVertical size={14} className="text-surface-300" />
                    <span className="text-xs font-semibold text-surface-500 uppercase tracking-wide">Step {i + 1}</span>
                    <Select
                      value={s.type}
                      onValueChange={(val) => updateStep(i, { ...s, type: val as 'mcp' | 'llm' } as JobStep)}
                    >
                      <SelectTrigger className="h-7 w-24 text-xs">
                        <SelectValue>{s.type === 'llm' ? 'LLM Call' : 'MCP Tool'}</SelectValue>
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="mcp">MCP Tool</SelectItem>
                        <SelectItem value="llm">LLM Call</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <Button
                    variant="ghost"
                    size="icon-xs"
                    onClick={() => removeStep(i)}
                    className="text-surface-400 hover:text-error"
                  >
                    <Trash2 size={14} />
                  </Button>
                </div>

                <StepParamFields
                  step={s}
                  stepIndex={i}
                  onUpdate={(updated) => updateStep(i, updated)}
                  templates={templates}
                  models={models}
                  mcpServers={mcpServers}
                  selectedServerTools={selectedServerTools}
                  loadingTools={loadingTools}
                  onLoadServerTools={loadServerTools}
                  variableValues={variableValues}
                  onVariableChange={(key, value) => setVariableValues((prev) => ({ ...prev, [key]: value }))}
                />
              </div>
            ))}
          </div>
        </div>

        <div>
          <label className="block text-xs font-medium text-surface-500 mb-1">Bill To (API Key)</label>
          <Select
            value={apiKeyId || undefined}
            onValueChange={(val: string | null) => setApiKeyId(val && val !== '__none' ? val : '')}
          >
            <SelectTrigger className="w-full" disabled={loadingApiKeys}>
              <SelectValue placeholder="Select an API key (optional)">
                {apiKeyId &&
                  (() => {
                    const k = apiKeys.find((a) => a.id === apiKeyId)
                    return k ? (
                      <span className="flex items-center gap-2">
                        <KeyRound size={14} className="text-surface-400 shrink-0" />
                        <span className="font-medium truncate">{k.name}</span>
                        <span className="text-xs text-surface-400 font-mono">({k.id.slice(0, 8)}...)</span>
                      </span>
                    ) : undefined
                  })()}
              </SelectValue>
            </SelectTrigger>
            <SelectContent
              className="z-50 min-w-[280px] overflow-hidden rounded-lg border border-surface-200 bg-white shadow-lg"
              align="start"
              sideOffset={4}
            >
              <SelectItem
                value="__none"
                className="cursor-pointer rounded-md px-3 py-2 text-sm text-surface-500 data-[highlighted]:bg-brand-50 data-[highlighted]:text-brand-700"
              >
                <span className="italic">None (uses admin key)</span>
              </SelectItem>
              {loadingApiKeys ? (
                <div className="flex items-center justify-center gap-2 px-4 py-3 text-xs text-surface-400">
                  <Loader2 size={14} className="animate-spin" /> Loading...
                </div>
              ) : apiKeys.length === 0 ? (
                <div className="px-4 py-3 text-xs text-surface-400 text-center">No API keys available</div>
              ) : (
                apiKeys.map((k) => (
                  <SelectItem
                    key={k.id}
                    value={k.id}
                    className="cursor-pointer rounded-md px-3 py-2 text-sm text-surface-900 data-[highlighted]:bg-brand-50 data-[highlighted]:text-brand-700 data-[state=checked]:bg-brand-50 data-[state=checked]:text-brand-700"
                  >
                    <span className="flex items-center gap-2">
                      <KeyRound size={14} className="text-surface-400 shrink-0" />
                      <span className="font-medium">{k.name}</span>
                      <span className="text-xs text-surface-400 font-mono">({k.id.slice(0, 8)}...)</span>
                    </span>
                  </SelectItem>
                ))
              )}
            </SelectContent>
          </Select>
          <p className="text-[11px] text-surface-400 mt-1 leading-relaxed">
            Each job execution consumes tokens and costs money. Select the API key whose monthly budget should be
            charged.
          </p>
        </div>

        <div>
          <label className="block text-xs font-medium text-surface-500 mb-1">Timeout (ms)</label>
          <Input
            type="number"
            value={timeoutMs}
            onChange={(e) => setTimeoutMs(Number(e.target.value))}
            min={1000}
            step={1000}
          />
        </div>

        {submitError && (
          <p className="text-xs text-error bg-error/5 rounded-lg px-3 py-2 border border-error/20">{submitError}</p>
        )}

        <div className="flex gap-2 pt-2 border-t border-surface-200">
          <Button onClick={handleSubmit} disabled={hasErrors || saving}>
            {saving ? 'Saving...' : initialData?.id ? 'Save Changes' : 'Create Job'}
          </Button>
          {onCancel && (
            <Button variant="outline" onClick={onCancel}>
              Cancel
            </Button>
          )}
        </div>
      </div>
    </QueryClientProvider>
  )
}
