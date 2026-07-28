import { Button } from '../ui/button'
import { Card, CardContent } from '../ui/card'
import { EmptyState } from '../ui/empty-state'
import { AlertCircle, Download, Plus, RefreshCw, Search } from '../ui/icons'
import { QueryProvider } from '../ui/query-provider'
import { Skeleton } from '../ui/skeleton'
import { AddModelModal } from './components/AddModelModal'
import { ConfigModal } from './components/ConfigModal'
import { TierBadge } from './components/TierBadge'
import { useModels } from './useModels'

function ModelsViewContent() {
  const {
    models,
    filtered,
    isLoading,
    error,
    refetch,
    search,
    setSearch,
    tierFilter,
    setTierFilter,
    showAddModal,
    setShowAddModal,
    configModel,
    setConfigModel,
    configForm,
    setConfigForm,
    toggleModel,
    handleSaveConfig,
    handleAddProvider,
    exportModels,
    formatCost,
    tiers,
  } = useModels()

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div className="relative flex-1 max-w-md">
          <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-surface-400" />
          <input
            type="text"
            placeholder="Search models..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full rounded-lg border border-surface-300 bg-white py-2 pl-10 pr-3 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
          />
        </div>
        <div className="flex items-center gap-2">
          {filtered.length > 0 && (
            <Button variant="outline" size="sm" onClick={exportModels}>
              <Download size={14} />
              Export
            </Button>
          )}
          <Button onClick={() => setShowAddModal(true)}>
            <Plus size={16} />
            Add Model
          </Button>
        </div>
      </div>

      <div className="flex flex-wrap gap-2">
        {[null, ...tiers].map((tier) => (
          <button
            key={tier ?? 'all'}
            type="button"
            onClick={() => setTierFilter(tier)}
            className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${
              tierFilter === tier
                ? tier === null
                  ? 'bg-surface-800 text-white'
                  : tier === 'premium'
                    ? 'bg-brand-600 text-white'
                    : tier === 'standard'
                      ? 'bg-surface-600 text-white'
                      : tier === 'economy'
                        ? 'bg-warning text-white'
                        : 'bg-success text-white'
                : 'bg-surface-100 text-surface-600 hover:bg-surface-200'
            }`}
          >
            {tier === null ? 'All' : tier.charAt(0).toUpperCase() + tier.slice(1)}
            {tier !== null && <span className="ml-1 opacity-70">({models.filter((m) => m.tier === tier).length})</span>}
          </button>
        ))}
      </div>

      {isLoading ? (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
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
      ) : error ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center py-10 text-center">
            <div className="rounded-full bg-error/10 p-3 mb-3">
              <AlertCircle size={24} className="text-error" />
            </div>
            <h3 className="text-lg font-semibold text-surface-900 mb-1">Failed to load models</h3>
            <p className="text-sm text-surface-500 mb-4 max-w-sm">
              {error instanceof Error ? error.message : String(error)}
            </p>
            <Button variant="outline" size="sm" onClick={() => refetch()}>
              <RefreshCw size={14} className="mr-1.5" />
              Retry
            </Button>
          </CardContent>
        </Card>
      ) : filtered.length === 0 ? (
        <EmptyState
          title="No models found"
          description={search ? 'Try a different search term.' : 'Add a provider to get started.'}
        />
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
          {filtered.map((model) => (
            <Card key={model.id} className={`transition-opacity ${!model.is_active ? 'opacity-60' : ''}`}>
              <CardContent className="p-4">
                <div className="flex items-start justify-between mb-3">
                  <div>
                    <p className="text-sm font-semibold text-surface-900">{model.name}</p>
                    <p className="text-xs text-surface-500 font-mono mt-0.5">{model.model}</p>
                  </div>
                  <button
                    type="button"
                    role="switch"
                    aria-checked={model.is_active}
                    onClick={() => toggleModel(model.id)}
                    className={`relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none focus:ring-2 focus:ring-brand-500 focus:ring-offset-2 ${
                      model.is_active ? 'bg-brand-600' : 'bg-surface-300'
                    }`}
                  >
                    <span
                      className={`pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ${model.is_active ? 'translate-x-4' : 'translate-x-0'}`}
                    />
                  </button>
                </div>

                <div className="flex items-center gap-2 mb-3">
                  <span className="text-xs text-surface-500">{model.provider}</span>
                  <TierBadge tier={model.tier} />
                </div>

                <div className="text-xs text-surface-500 space-y-1">
                  <p className="flex justify-between">
                    <span>Input</span>
                    <span className="font-mono text-surface-700">${formatCost(model.cost_per_1k_in)}/1K</span>
                  </p>
                  <p className="flex justify-between">
                    <span>Output</span>
                    <span className="font-mono text-surface-700">${formatCost(model.cost_per_1k_out)}/1K</span>
                  </p>
                </div>

                <div className="mt-3 pt-3 border-t border-surface-100">
                  <Button variant="outline" size="sm" className="w-full" onClick={() => setConfigModel(model)}>
                    Configure
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      {showAddModal && (
        <AddModelModal
          name={configForm.name}
          provider={configForm.provider}
          modelId={configForm.model}
          tier={configForm.tier}
          costIn={configForm.cost_in}
          costOut={configForm.cost_out}
          onNameChange={(val) => setConfigForm({ ...configForm, name: val })}
          onProviderChange={(val) => setConfigForm({ ...configForm, provider: val })}
          onModelIdChange={(val) => setConfigForm({ ...configForm, model: val })}
          onTierChange={(val) => setConfigForm({ ...configForm, tier: val })}
          onCostInChange={(val) => setConfigForm({ ...configForm, cost_in: val })}
          onCostOutChange={(val) => setConfigForm({ ...configForm, cost_out: val })}
          onSave={handleAddProvider}
          onClose={() => setShowAddModal(false)}
        />
      )}

      {configModel && (
        <ConfigModal
          model={configModel}
          onTierChange={(tier) => setConfigModel({ ...configModel, tier: tier as typeof configModel.tier })}
          onToggleActive={() => setConfigModel({ ...configModel, is_active: !configModel.is_active })}
          onSave={handleSaveConfig}
          onClose={() => setConfigModel(null)}
          formatCost={formatCost}
        />
      )}
    </div>
  )
}

export function ModelsView() {
  return (
    <QueryProvider>
      <ModelsViewContent />
    </QueryProvider>
  )
}
