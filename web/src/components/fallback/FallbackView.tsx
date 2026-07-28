import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { api } from '../../lib/api'
import { qk } from '../../lib/query'
import { useApiMutation } from '../../lib/useApiMutation'
import { ModelBadge, ModelSelector, providerMeta, TierBadge } from '../chat/ModelSelector'
import { FeatureStatus } from '../settings/FeatureStatus'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { DataTable } from '../ui/DataTable'
import { Clock, Plus, Save, ShieldCheck, Trash2, X } from '../ui/icons'
import { QueryProvider } from '../ui/query-provider'
import { StatCard } from '../ui/StatCard'

function FallbackViewContent() {
  const { data, isLoading } = useQuery({
    queryKey: qk.fallback,
    queryFn: api.fallback.getFallbackSummary,
  })

  const { data: rawModelsData } = useQuery({
    queryKey: qk.models,
    queryFn: () => api.models.getModelProviders().catch(() => []),
  })

  const [cooldownDuration, setCooldownDuration] = useState('')
  const [maxAttempts, setMaxAttempts] = useState('')
  const [modelDowngrade, setModelDowngrade] = useState('')
  const [selectedModels, setSelectedModels] = useState<string[]>([])
  const [candidateToAdd, setCandidateToAdd] = useState('')
  const [formInitialized, setFormInitialized] = useState(false)
  const [saveSuccess, setSaveSuccess] = useState(false)

  if (data && !formInitialized) {
    setCooldownDuration(data.cooldown_duration || '5m')
    setMaxAttempts(String(data.max_attempts ?? 0))
    setModelDowngrade(data.model_downgrade || 'cheapest')
    setSelectedModels(data.allowed_models || [])
    setFormInitialized(true)
  }

  const availableModels = rawModelsData ?? []
  const findModel = (name: string) => availableModels.find((m) => m.model === name || m.id === name)

  const toggleMutation = useApiMutation((enabled: boolean) => api.fallback.toggleFallback(enabled), {
    invalidate: [qk.fallback],
  })

  const updateConfigMutation = useApiMutation(
    (configData: Parameters<typeof api.fallback.updateFallbackConfig>[0]) =>
      api.fallback.updateFallbackConfig(configData),
    {
      invalidate: [qk.fallback],
      onDone: () => {
        setSaveSuccess(true)
        setTimeout(() => setSaveSuccess(false), 3000)
      },
    },
  )

  const clearCooldownMutation = useApiMutation(
    ({ provider, model, keyId }: { provider: string; model: string; keyId: string }) =>
      api.fallback.clearCooldown(provider, model, keyId),
    {
      invalidate: [qk.fallback],
    },
  )

  if (isLoading) {
    return (
      <div className="flex items-center justify-center py-20">
        <div className="text-surface-500 text-sm">Loading fallback configuration...</div>
      </div>
    )
  }

  const config = data ?? {
    enabled: true,
    cooldown_duration: '5m',
    model_downgrade: 'cheapest',
    allowed_models: [],
    max_attempts: 0,
    active_cooldowns: [],
  }

  const handleAddSelectedModel = () => {
    if (!candidateToAdd) return
    // Normalize: ModelSelector returns m.id ("provider/name"), store bare model name
    const bareName = candidateToAdd.includes('/') ? candidateToAdd.split('/')[1] : candidateToAdd
    if (selectedModels.includes(bareName)) return
    setSelectedModels([...selectedModels, bareName])
    setCandidateToAdd('')
  }

  const handleRemoveModel = (modelId: string) => {
    setSelectedModels(selectedModels.filter((m) => m !== modelId))
  }

  const handleSaveConfig = (e: React.SyntheticEvent) => {
    e.preventDefault()
    const attempts = parseInt(maxAttempts, 10) || 0

    updateConfigMutation.mutate({
      cooldown_duration: cooldownDuration,
      max_attempts: attempts,
      model_downgrade: modelDowngrade,
      allowed_models: selectedModels,
    })
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold text-surface-900">Automatic Fallback</h2>
          <p className="text-xs text-surface-500 mt-0.5">
            Automatic key/provider rate-limit & error fallback routing across multiple candidate models
          </p>
        </div>
        <div className="flex items-center gap-2">
          <FeatureStatus
            type="toggle"
            enabled={config.enabled}
            onToggle={() => toggleMutation.mutate(!config.enabled)}
          />
        </div>
      </div>

      <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
        <StatCard
          title="Active Cooldowns"
          value={config.active_cooldowns.length}
          description="benched failing candidates"
          icon={<Clock size={20} />}
        />
        <StatCard
          title="Configured Candidates"
          value={selectedModels.length}
          description="allowed fallback models"
          icon={<ShieldCheck size={20} />}
        />
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Fallback Policy & Cooldown Settings</CardTitle>
        </CardHeader>
        <CardContent>
          <form onSubmit={handleSaveConfig} className="space-y-6">
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <div>
                <label htmlFor="cooldown-duration-input" className="block text-xs font-medium text-surface-700 mb-1">
                  Cooldown Duration
                </label>
                <input
                  id="cooldown-duration-input"
                  type="text"
                  value={cooldownDuration}
                  onChange={(e) => setCooldownDuration(e.target.value)}
                  placeholder="5m, 1m, 30s"
                  className="w-full px-3 py-1.5 text-sm border border-surface-300 rounded-md shadow-sm focus:ring-brand-500 focus:border-brand-500"
                />
                <p className="text-[11px] text-surface-500 mt-1">Penalty period for failing candidates</p>
              </div>

              <div>
                <label htmlFor="max-attempts-input" className="block text-xs font-medium text-surface-700 mb-1">
                  Max Attempts
                </label>
                <input
                  id="max-attempts-input"
                  type="number"
                  min="0"
                  value={maxAttempts}
                  onChange={(e) => setMaxAttempts(e.target.value)}
                  placeholder="0 (unlimited)"
                  className="w-full px-3 py-1.5 text-sm border border-surface-300 rounded-md shadow-sm focus:ring-brand-500 focus:border-brand-500"
                />
                <p className="text-[11px] text-surface-500 mt-1">0 = try all candidates</p>
              </div>

              <div>
                <label htmlFor="model-downgrade-select" className="block text-xs font-medium text-surface-700 mb-1">
                  Model Downgrade Policy
                </label>
                <select
                  id="model-downgrade-select"
                  value={modelDowngrade}
                  onChange={(e) => setModelDowngrade(e.target.value)}
                  className="w-full px-3 py-1.5 text-sm border border-surface-300 rounded-md shadow-sm focus:ring-brand-500 focus:border-brand-500"
                >
                  <option value="cheapest">Cheapest Healthy Candidate</option>
                  <option value="same_family">Same Model Family</option>
                  <option value="none">None (Exact Model Match Only)</option>
                  <option value="custom">Custom Allowed List</option>
                </select>
                <p className="text-[11px] text-surface-500 mt-1">Downgrade rule when primary model fails</p>
              </div>
            </div>

            {/* ModelSelector Integration */}
            <div className="space-y-3 pt-4 border-t border-surface-200">
              <div className="flex items-center justify-between">
                <div>
                  <label className="block text-xs font-medium text-surface-900">
                    Allowed Fallback Models ({selectedModels.length} Selected)
                  </label>
                  <p className="text-[11px] text-surface-500 mt-0.5">
                    Select registered models from catalog to add to the fallback candidate pool
                  </p>
                </div>
              </div>

              <div className="flex items-center gap-3">
                <div className="flex-1 max-w-sm">
                  <ModelSelector
                    models={availableModels.filter((m) => !selectedModels.includes(m.model))}
                    value={candidateToAdd}
                    onChange={(val) => setCandidateToAdd(val)}
                  />
                </div>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={handleAddSelectedModel}
                  disabled={!candidateToAdd}
                >
                  <Plus size={14} className="mr-1" /> Add Candidate Model
                </Button>
              </div>

              {/* Selected Models — grouped by provider, SelectItem-style */}
              <div className="bg-surface-50 border border-surface-200 rounded-lg min-h-[70px]">
                {selectedModels.length === 0 ? (
                  <div className="p-4 text-xs text-surface-500 text-center">
                    No allowed fallback models selected. Use the model selector above to add candidate models.
                  </div>
                ) : (
                  <div className="divide-y divide-surface-200">
                    <div className="px-4 pt-3 pb-2 text-[11px] font-semibold uppercase tracking-wider text-surface-500">
                      Active Fallback Candidates ({selectedModels.length}):
                    </div>
                    {(() => {
                      const groups = new Map<string, { modelId: string; mInfo?: ReturnType<typeof findModel> }[]>()
                      for (const modelId of selectedModels) {
                        const mInfo = findModel(modelId)
                        const provKey = (mInfo?.provider || modelId.split('/')[0] || 'other').toLowerCase()
                        if (!groups.has(provKey)) groups.set(provKey, [])
                        groups.get(provKey)!.push({ modelId, mInfo })
                      }
                      return Array.from(groups.entries()).map(([provKey, items]) => {
                        const meta = providerMeta[provKey]
                        return (
                          <div key={provKey} className="px-4 py-2 space-y-0.5">
                            <div className="flex items-center gap-1.5 pb-1 text-[11px] font-semibold uppercase tracking-wider text-surface-500">
                              {meta?.dot && <span className={`w-1.5 h-1.5 rounded-full ${meta.dot}`} />}
                              {meta?.label || provKey}
                            </div>
                            {items.map(({ modelId, mInfo }) => (
                              <div
                                key={modelId}
                                className="flex items-center gap-2 rounded-md px-3 py-2 text-sm text-surface-900 hover:bg-white/60 transition-colors group"
                              >
                                <button
                                  type="button"
                                  onClick={() => handleRemoveModel(modelId)}
                                  className="text-surface-400 hover:text-error transition-colors opacity-0 group-hover:opacity-100 text-xs"
                                  title={`Remove ${modelId}`}
                                >
                                  <X size={14} />
                                </button>
                                <span className="flex items-center gap-2">
                                  <span className="font-medium">{mInfo?.name || modelId}</span>
                                  <TierBadge tier={mInfo?.tier} />
                                </span>
                              </div>
                            ))}
                          </div>
                        )
                      })
                    })()}
                  </div>
                )}
              </div>
            </div>

            <div className="flex items-center justify-between pt-2">
              <div className="text-xs">
                {saveSuccess && <span className="text-success font-medium">Configuration saved successfully!</span>}
                {updateConfigMutation.isError && (
                  <span className="text-error font-medium">Failed to save configuration.</span>
                )}
              </div>
              <Button type="submit" disabled={updateConfigMutation.isPending} size="sm">
                <Save size={16} className="mr-1" />
                {updateConfigMutation.isPending ? 'Saving...' : 'Save Settings'}
              </Button>
            </div>
          </form>
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="flex flex-row items-center justify-between">
          <CardTitle className="text-base">Active Candidate Cooldowns</CardTitle>
          {config.active_cooldowns.length > 0 && (
            <span className="text-xs text-surface-500">
              {config.active_cooldowns.length} candidate(s) currently in cooldown
            </span>
          )}
        </CardHeader>
        <CardContent className="p-0">
          {config.active_cooldowns.length === 0 ? (
            <div className="p-8 text-center text-surface-500 text-sm">
              No candidates are currently in cooldown. All provider routes are healthy.
            </div>
          ) : (
            <DataTable
              columns={[
                {
                  key: 'model',
                  header: 'Model',
                  render: (c) => {
                    const mInfo = findModel(c.model)
                    return (
                      <ModelBadge
                        modelId={c.model}
                        provider={mInfo?.provider || c.provider}
                        tier={mInfo?.tier}
                        name={mInfo?.name}
                      />
                    )
                  },
                },
                {
                  key: 'provider',
                  header: 'Provider',
                  render: (c) => {
                    const mInfo = findModel(c.model)
                    return <span className="font-medium text-surface-900">{mInfo?.provider || c.provider}</span>
                  },
                },
                {
                  key: 'key_id',
                  header: 'API Key ID',
                  render: (c) => <span className="font-mono text-xs text-surface-600">{c.key_id || 'Global'}</span>,
                },
                {
                  key: 'expires_at',
                  header: 'Expires At',
                  render: (c) => <span className="font-mono text-xs text-surface-600">{c.expires_at}</span>,
                },
                {
                  key: 'actions',
                  header: 'Actions',
                  render: (c) => (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() =>
                        clearCooldownMutation.mutate({
                          provider: c.provider,
                          model: c.model,
                          keyId: c.key_id,
                        })
                      }
                      disabled={clearCooldownMutation.isPending}
                    >
                      <Trash2 size={14} className="mr-1 text-error" />
                      Clear Cooldown
                    </Button>
                  ),
                },
              ]}
              data={config.active_cooldowns}
              keyExtractor={(c) => `${c.provider}-${c.model}-${c.key_id}`}
            />
          )}
        </CardContent>
      </Card>
    </div>
  )
}

export function FallbackView() {
  return (
    <QueryProvider>
      <FallbackViewContent />
    </QueryProvider>
  )
}
