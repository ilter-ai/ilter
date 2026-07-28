import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import {
  api,
  type ComplexityThresholds,
  type RoutingRule,
  type RoutingStrategy,
  type ScorerConfig,
} from '../../lib/api'
import { qk } from '../../lib/query'
import { cn } from '../../lib/utils'
import { ModelSelector } from '../chat/ModelSelector'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { type Column, DataTable } from '../ui/DataTable'
import { EmptyState } from '../ui/empty-state'
import { ArrowLeft, ChevronDown, ChevronUp, Edit3, Plus, Trash2 } from '../ui/icons'
import { Input } from '../ui/input'
import { QueryProvider } from '../ui/query-provider'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../ui/select'
import { Switch } from '../ui/switch'
import { Textarea } from '../ui/textarea'

/* ─── local rule type with stable id ─── */
interface LocalRule extends RoutingRule {
  _id: string
}

/* ─── helper ─── */
let _nextId = 1
function uid() {
  return `r${_nextId++}_${Date.now()}`
}

/* ─── status badge ─── */
const statusBadge = (enabled: boolean) => (
  <span
    className={cn(
      'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium',
      enabled
        ? 'bg-success/10 text-success border border-success/20'
        : 'bg-surface-100 text-surface-500 border border-surface-200',
    )}
  >
    {enabled ? 'Active' : 'Disabled'}
  </span>
)

/* ─── Rule Form Modal ─── */
function RuleFormModal({
  open,
  onClose,
  onSave,
  initial,
}: {
  open: boolean
  onClose: () => void
  onSave: (rule: RoutingRule) => void
  initial?: RoutingRule | null
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [condition, setCondition] = useState(initial?.condition ?? '')
  const [targetModel, setTargetModel] = useState(initial?.target_model ?? '')
  const [priority, setPriority] = useState(initial?.priority ?? 1)
  const [enabled, setEnabled] = useState(initial?.enabled ?? true)

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <Card className="w-full max-w-lg mx-4">
        <CardHeader>
          <CardTitle>{initial ? 'Edit Rule' : 'Add Rule'}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">Rule Name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g., High Priority Queries"
                className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">Condition</label>
              <textarea
                value={condition}
                onChange={(e) => setCondition(e.target.value)}
                placeholder="e.g., tokens < 500 && model_cost == 'low'"
                rows={3}
                className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm font-mono text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
              <p className="text-xs text-surface-400 mt-1">
                Use expressions like: <code className="text-brand-600">tokens &lt; 500</code>,{' '}
                <code className="text-brand-600">prompt contains "code"</code>
              </p>
            </div>
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">Target Model</label>
              <ModelSelector value={targetModel} onChange={setTargetModel} placeholder="Select target model..." />
            </div>
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">Priority</label>
              <input
                type="number"
                min={1}
                value={priority}
                onChange={(e) => setPriority(Number(e.target.value))}
                className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
            </div>
            <div className="flex items-center gap-3">
              <label className="text-sm font-medium text-surface-700">Enabled</label>
              <Switch checked={enabled} onCheckedChange={setEnabled} />
            </div>
            <div className="flex justify-end gap-3 pt-2">
              <Button variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button
                onClick={() => {
                  onSave({ name, condition, target_model: targetModel, priority, enabled })
                  onClose()
                }}
                disabled={!name || !condition || !targetModel}
              >
                {initial ? 'Save Changes' : 'Create Rule'}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

/* ─── Strategy Editor Content ─── */
interface StrategyEditorProps {
  strategy: RoutingStrategy
  onBack: () => void
}

const providerPrefOptions = ['cheapest', 'round-robin'] as const
const lbStrategyOptions = [
  'weighted-random',
  'round-robin',
  'cost-optimized',
  'latency-optimized',
  'priority-based',
] as const
const scorerTypeOptions = ['heuristic', 'llm', 'embedding', 'trainable'] as const

function StrategyEditorContent({ strategy, onBack }: StrategyEditorProps) {
  const queryClient = useQueryClient()

  /* ─── local form state ─── */
  const [form, setForm] = useState<RoutingStrategy>(strategy)
  const [rules, setRules] = useState<LocalRule[]>(() => strategy.rules.map((r) => ({ ...r, _id: uid() })))
  const [showAddModal, setShowAddModal] = useState(false)
  const [editingRule, setEditingRule] = useState<LocalRule | null>(null)
  const [deleteConfirm, setDeleteConfirm] = useState<string | null>(null)

  const hasChanges =
    JSON.stringify({ ...form, rules: rules.map(({ _id, ...r }) => r) }) !==
    JSON.stringify({ ...strategy, rules: strategy.rules })

  /* ─── field updaters ─── */
  const update = <K extends keyof RoutingStrategy>(key: K, value: RoutingStrategy[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }))

  const updateThresholds = (key: keyof ComplexityThresholds, value: number) =>
    setForm((prev) => ({
      ...prev,
      complexity_thresholds: { ...prev.complexity_thresholds, [key]: value },
    }))

  const updateScorer = (field: keyof ScorerConfig, value: unknown) =>
    setForm((prev) => {
      const base = { ...prev.scorer, [field]: value } as ScorerConfig
      // when type changes, null out stale sub-configs to prevent phantom payload
      if (field === 'type') {
        base.llm = null
        base.embedding = null
        base.trainable = null
      }
      return { ...prev, scorer: base }
    })

  const updateLLM = (field: string, value: unknown) =>
    setForm((prev) => ({
      ...prev,
      scorer: {
        ...prev.scorer,
        llm: {
          ...(prev.scorer.llm ?? { model: '', provider: '', cache_ttl: '', cache_max_entries: 0, timeout: '' }),
          [field]: value,
        },
      },
    }))

  const updateEmbedding = (field: string, value: unknown) =>
    setForm((prev) => ({
      ...prev,
      scorer: {
        ...prev.scorer,
        embedding: {
          ...(prev.scorer.embedding ?? { model: '', dimensions: 0, reference_count: 0, similarity_threshold: 0 }),
          [field]: value,
        },
      },
    }))

  const updateTrainable = (field: string, value: unknown) =>
    setForm((prev) => ({
      ...prev,
      scorer: {
        ...prev.scorer,
        trainable: {
          ...(prev.scorer.trainable ?? { model_path: '', feature_version: 0, fallback_on_error: false }),
          [field]: value,
        },
      },
    }))

  /* ─── rule CRUD ─── */
  const addRule = (rule: RoutingRule) => setRules((prev) => [...prev, { ...rule, _id: uid() }])

  const updateRule = (rule: LocalRule) => setRules((prev) => prev.map((r) => (r._id === rule._id ? rule : r)))

  const deleteRule = () => {
    if (!deleteConfirm) return
    setRules((prev) => prev.filter((r) => r._id !== deleteConfirm))
    setDeleteConfirm(null)
  }

  const moveRule = (id: string, direction: 'up' | 'down') => {
    setRules((prev) => {
      const sorted = [...prev].sort((a, b) => a.priority - b.priority)
      const idx = sorted.findIndex((r) => r._id === id)
      if (idx === -1) return prev
      const swapIdx = direction === 'up' ? idx - 1 : idx + 1
      if (swapIdx < 0 || swapIdx >= sorted.length) return prev
      const temp = sorted[idx].priority
      sorted[idx] = { ...sorted[idx], priority: sorted[swapIdx].priority }
      sorted[swapIdx] = { ...sorted[swapIdx], priority: temp }
      return sorted
    })
  }

  /* ─── save ─── */
  const saveMutation = useMutation({
    mutationFn: (s: RoutingStrategy) => api.strategies.saveStrategy(strategy.name, s),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: qk.strategies })
      queryClient.invalidateQueries({ queryKey: qk.strategyDetail(strategy.name) })
      toast.success('Strategy saved')
    },
    onError: (err) => toast.error('Failed to save strategy', { description: String(err) }),
  })

  const handleSave = () => {
    saveMutation.mutate({ ...form, rules: rules.map(({ _id, ...r }) => r) })
  }

  /* ─── table columns ─── */
  const sortedRules = [...rules].sort((a, b) => a.priority - b.priority)

  const columns: Column<LocalRule>[] = [
    {
      key: 'priority',
      header: 'Priority',
      className: 'w-24',
      render: (r) => (
        <div className="flex items-center gap-1">
          <span className="text-sm font-medium text-surface-700 mr-2">{r.priority}</span>
          <div className="flex flex-col gap-0.5">
            <button
              onClick={(e) => {
                e.stopPropagation()
                moveRule(r._id, 'up')
              }}
              className="text-surface-400 hover:text-surface-700 transition-colors"
              disabled={r.priority <= 1}
            >
              <ChevronUp size={10} />
            </button>
            <button
              onClick={(e) => {
                e.stopPropagation()
                moveRule(r._id, 'down')
              }}
              className="text-surface-400 hover:text-surface-700 transition-colors"
              disabled={r.priority >= rules.length}
            >
              <ChevronDown size={10} />
            </button>
          </div>
        </div>
      ),
    },
    {
      key: 'name',
      header: 'Name',
      render: (r) => <span className="font-medium text-surface-900">{r.name}</span>,
    },
    {
      key: 'condition',
      header: 'Condition',
      render: (r) => (
        <code className="text-xs bg-surface-100 text-surface-700 px-1.5 py-0.5 rounded font-mono">
          {r.condition.length > 60 ? `${r.condition.slice(0, 60)}…` : r.condition}
        </code>
      ),
    },
    {
      key: 'target_model',
      header: 'Target Model',
      render: (r) => (
        <span className="inline-flex items-center rounded-md bg-brand-50 px-2 py-0.5 text-xs font-medium text-brand-700">
          {r.target_model}
        </span>
      ),
    },
    {
      key: 'enabled',
      header: 'Status',
      render: (r) => statusBadge(r.enabled),
    },
    {
      key: 'actions',
      header: 'Actions',
      className: 'w-24',
      render: (r) => (
        <div className="flex items-center gap-2">
          <Switch
            size="sm"
            checked={r.enabled}
            onCheckedChange={() =>
              setRules((prev) => prev.map((x) => (x._id === r._id ? { ...x, enabled: !x.enabled } : x)))
            }
          />
          <button
            onClick={(e) => {
              e.stopPropagation()
              setEditingRule(r)
            }}
            className="text-surface-400 hover:text-brand-600 transition-colors"
            title="Edit"
          >
            <Edit3 size={15} />
          </button>
          <button
            onClick={(e) => {
              e.stopPropagation()
              setDeleteConfirm(r._id)
            }}
            className="text-surface-400 hover:text-error transition-colors"
            title="Delete"
          >
            <Trash2 size={15} />
          </button>
        </div>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      {/* ── Header ── */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <button
            onClick={onBack}
            className="inline-flex items-center gap-1 text-sm text-surface-500 hover:text-surface-700 transition-colors"
          >
            <ArrowLeft size={16} />
            Back
          </button>
          <h2 className="text-lg font-semibold text-surface-900">
            {strategy.name
              .split('-')
              .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
              .join(' ')}
          </h2>
          {statusBadge(form.enabled)}
        </div>
        <Button onClick={handleSave} disabled={!hasChanges || saveMutation.isPending}>
          {saveMutation.isPending ? 'Saving…' : 'Save'}
        </Button>
      </div>

      {/* ── Config Section ── */}
      <Card>
        <CardHeader>
          <CardTitle>Strategy Configuration</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">Name</label>
                <Input value={form.name} disabled />
              </div>
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">Description</label>
                <Textarea value={form.description} onChange={(e) => update('description', e.target.value)} rows={3} />
              </div>
              <div className="flex items-center gap-3">
                <label className="text-sm font-medium text-surface-700">Enabled</label>
                <Switch checked={form.enabled} onCheckedChange={(v) => update('enabled', v)} />
              </div>
            </div>
            <div className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">Provider Preference</label>
                <Select
                  value={form.provider_preference}
                  onValueChange={(v) => v && update('provider_preference', v as 'cheapest' | 'round-robin')}
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {providerPrefOptions.map((opt) => (
                      <SelectItem key={opt} value={opt}>
                        {opt === 'cheapest' ? 'Cheapest' : 'Round Robin'}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">Load Balancer Strategy</label>
                <Select
                  value={form.load_balancer_strategy}
                  onValueChange={(v) =>
                    v && update('load_balancer_strategy', v as RoutingStrategy['load_balancer_strategy'])
                  }
                >
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {lbStrategyOptions.map((opt) => (
                      <SelectItem key={opt} value={opt}>
                        {opt
                          .split('-')
                          .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
                          .join(' ')}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Economy Threshold</label>
                  <Input
                    type="number"
                    min={0}
                    max={100}
                    step={1}
                    value={form.complexity_thresholds.economy}
                    onChange={(e) => updateThresholds('economy', parseFloat(e.target.value) || 0)}
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Standard Threshold</label>
                  <Input
                    type="number"
                    min={0}
                    max={100}
                    step={1}
                    value={form.complexity_thresholds.standard}
                    onChange={(e) => updateThresholds('standard', parseFloat(e.target.value) || 0)}
                  />
                </div>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* ── Scorer Section ── */}
      <Card>
        <CardHeader>
          <CardTitle>Scorer Configuration</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <div className="max-w-xs">
              <label className="block text-sm font-medium text-surface-700 mb-1">Scorer Type</label>
              <Select
                value={form.scorer.type}
                onValueChange={(v) => v && updateScorer('type', v as ScorerConfig['type'])}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {scorerTypeOptions.map((opt) => (
                    <SelectItem key={opt} value={opt}>
                      {opt.charAt(0).toUpperCase() + opt.slice(1)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {form.scorer.type === 'llm' && (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pt-2 border-t border-surface-100">
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Model</label>
                  <Input
                    value={form.scorer.llm?.model ?? ''}
                    onChange={(e) => updateLLM('model', e.target.value)}
                    placeholder="e.g., gpt-4o-mini"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Provider</label>
                  <Input
                    value={form.scorer.llm?.provider ?? ''}
                    onChange={(e) => updateLLM('provider', e.target.value)}
                    placeholder="e.g., openai"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Cache TTL</label>
                  <Input
                    value={form.scorer.llm?.cache_ttl ?? ''}
                    onChange={(e) => updateLLM('cache_ttl', e.target.value)}
                    placeholder="e.g., 10m"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Cache Max Entries</label>
                  <Input
                    type="number"
                    min={0}
                    value={form.scorer.llm?.cache_max_entries ?? 0}
                    onChange={(e) => updateLLM('cache_max_entries', parseInt(e.target.value, 10) || 0)}
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Timeout</label>
                  <Input
                    value={form.scorer.llm?.timeout ?? ''}
                    onChange={(e) => updateLLM('timeout', e.target.value)}
                    placeholder="e.g., 30s"
                  />
                </div>
              </div>
            )}

            {form.scorer.type === 'embedding' && (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pt-2 border-t border-surface-100">
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Model</label>
                  <Input
                    value={form.scorer.embedding?.model ?? ''}
                    onChange={(e) => updateEmbedding('model', e.target.value)}
                    placeholder="e.g., text-embedding-3-small"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Dimensions</label>
                  <Input
                    type="number"
                    min={1}
                    value={form.scorer.embedding?.dimensions ?? 0}
                    onChange={(e) => updateEmbedding('dimensions', parseInt(e.target.value, 10) || 0)}
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Reference Count</label>
                  <Input
                    type="number"
                    min={1}
                    value={form.scorer.embedding?.reference_count ?? 0}
                    onChange={(e) => updateEmbedding('reference_count', parseInt(e.target.value, 10) || 0)}
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Similarity Threshold</label>
                  <Input
                    type="number"
                    min={0}
                    max={1}
                    step={0.05}
                    value={form.scorer.embedding?.similarity_threshold ?? 0}
                    onChange={(e) => updateEmbedding('similarity_threshold', parseFloat(e.target.value) || 0)}
                  />
                </div>
              </div>
            )}

            {form.scorer.type === 'trainable' && (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pt-2 border-t border-surface-100">
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Model Path</label>
                  <Input
                    value={form.scorer.trainable?.model_path ?? ''}
                    onChange={(e) => updateTrainable('model_path', e.target.value)}
                    placeholder="e.g., /models/scorer-v1"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Feature Version</label>
                  <Input
                    type="number"
                    min={1}
                    value={form.scorer.trainable?.feature_version ?? 0}
                    onChange={(e) => updateTrainable('feature_version', parseInt(e.target.value, 10) || 0)}
                  />
                </div>
                <div className="flex items-center gap-3">
                  <label className="text-sm font-medium text-surface-700">Fallback on Error</label>
                  <Switch
                    checked={form.scorer.trainable?.fallback_on_error ?? false}
                    onCheckedChange={(v) => updateTrainable('fallback_on_error', v)}
                  />
                </div>
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      {/* ── Rules Section ── */}
      <Card>
        <CardHeader>
          <div className="flex items-center justify-between w-full">
            <CardTitle>Routing Rules</CardTitle>
            <Button size="sm" onClick={() => setShowAddModal(true)}>
              <Plus size={16} />
              Add Rule
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          {sortedRules.length === 0 ? (
            <EmptyState
              title="No routing rules"
              description="Add your first rule to start routing requests."
              action={{ label: 'Add Rule', onClick: () => setShowAddModal(true) }}
            />
          ) : (
            <DataTable columns={columns} data={sortedRules} keyExtractor={(r) => r._id} />
          )}
        </CardContent>
      </Card>

      {/* ── Delete confirmation ── */}
      {deleteConfirm && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
          <Card className="w-full max-w-sm mx-4">
            <CardHeader>
              <CardTitle>Delete Rule</CardTitle>
            </CardHeader>
            <CardContent>
              <p className="text-sm text-surface-600 mb-4">
                Are you sure you want to delete this rule? This action cannot be undone.
              </p>
              <div className="flex justify-end gap-3">
                <Button variant="outline" onClick={() => setDeleteConfirm(null)}>
                  Cancel
                </Button>
                <Button variant="destructive" onClick={deleteRule}>
                  Delete
                </Button>
              </div>
            </CardContent>
          </Card>
        </div>
      )}

      {/* ── Add / Edit rule modal ── */}
      <RuleFormModal open={showAddModal} onClose={() => setShowAddModal(false)} onSave={addRule} />

      {editingRule && (
        <RuleFormModal
          open={!!editingRule}
          onClose={() => setEditingRule(null)}
          initial={editingRule}
          onSave={(data) => updateRule({ ...data, _id: editingRule._id })}
        />
      )}
    </div>
  )
}

/* ─── Wrapper with QueryProvider ─── */
export function StrategyEditor(props: StrategyEditorProps) {
  return (
    <QueryProvider>
      <StrategyEditorContent {...props} />
    </QueryProvider>
  )
}
