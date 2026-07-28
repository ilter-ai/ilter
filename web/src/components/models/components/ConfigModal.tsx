import { Button } from '../../ui/button'
import type { Model } from '../useModels'
import { tiers } from '../useModels'

interface ConfigModalProps {
  model: Model
  onTierChange: (tier: string) => void
  onToggleActive: () => void
  onSave: () => void
  onClose: () => void
  formatCost: (cost: number) => string
}

export function ConfigModal({ model, onTierChange, onToggleActive, onSave, onClose, formatCost }: ConfigModalProps) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="w-full max-w-md rounded-xl bg-white p-6 shadow-xl" onClick={(e) => e.stopPropagation()}>
        <h3 className="text-lg font-semibold text-surface-900 mb-4">Configure: {model.name}</h3>
        <div className="space-y-3">
          <div className="flex items-center justify-between rounded-lg bg-surface-50 p-3">
            <span className="text-sm text-surface-600">Status</span>
            <button
              type="button"
              role="switch"
              aria-checked={model.is_active}
              onClick={onToggleActive}
              className={`relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none focus:ring-2 focus:ring-brand-500 focus:ring-offset-2 ${
                model.is_active ? 'bg-brand-600' : 'bg-surface-300'
              }`}
            >
              <span
                className={`pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ${model.is_active ? 'translate-x-4' : 'translate-x-0'}`}
              />
            </button>
          </div>
          <div className="rounded-lg bg-surface-50 p-3">
            <label className="block text-sm text-surface-600 mb-1">Tier</label>
            <select
              value={model.tier}
              onChange={(e) => onTierChange(e.target.value)}
              className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
            >
              {tiers.map((t) => (
                <option key={t} value={t}>
                  {t.charAt(0).toUpperCase() + t.slice(1)}
                </option>
              ))}
            </select>
          </div>
          <div className="flex items-center justify-between rounded-lg bg-surface-50 p-3">
            <span className="text-sm text-surface-600">Provider</span>
            <span className="text-sm font-medium text-surface-800">{model.provider}</span>
          </div>
          <div className="flex items-center justify-between rounded-lg bg-surface-50 p-3">
            <span className="text-sm text-surface-600">Model ID</span>
            <span className="text-sm font-mono text-surface-800">{model.model}</span>
          </div>
          <div className="flex items-center justify-between rounded-lg bg-surface-50 p-3">
            <span className="text-sm text-surface-600">Input Cost</span>
            <span className="text-sm font-mono text-surface-800">${formatCost(model.cost_per_1k_in)}/1K</span>
          </div>
          <div className="flex items-center justify-between rounded-lg bg-surface-50 p-3">
            <span className="text-sm text-surface-600">Output Cost</span>
            <span className="text-sm font-mono text-surface-800">${formatCost(model.cost_per_1k_out)}/1K</span>
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button onClick={onSave}>Save Changes</Button>
          </div>
        </div>
      </div>
    </div>
  )
}
