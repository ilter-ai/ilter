import { Button } from '../../ui/button'
import type { Model } from '../useModels'

interface AddModelModalProps {
  name: string
  provider: string
  modelId: string
  tier: Model['tier']
  costIn: number
  costOut: number
  onNameChange: (val: string) => void
  onProviderChange: (val: string) => void
  onModelIdChange: (val: string) => void
  onTierChange: (val: Model['tier']) => void
  onCostInChange: (val: number) => void
  onCostOutChange: (val: number) => void
  onSave: () => void
  onClose: () => void
}

export function AddModelModal({
  name,
  provider,
  modelId,
  tier,
  costIn,
  costOut,
  onNameChange,
  onProviderChange,
  onModelIdChange,
  onTierChange,
  onCostInChange,
  onCostOutChange,
  onSave,
  onClose,
}: AddModelModalProps) {
  const isValid = name && provider && modelId

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4" onClick={onClose}>
      <div className="w-full max-w-lg rounded-xl bg-white p-6 shadow-xl" onClick={(e) => e.stopPropagation()}>
        <h3 className="text-lg font-semibold text-surface-900 mb-4">Add Model</h3>
        <div className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-surface-500 mb-1">Display Name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => onNameChange(e.target.value)}
                placeholder="e.g. GPT-4o"
                className="w-full rounded-lg border border-surface-300 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-surface-500 mb-1">Provider</label>
              <input
                type="text"
                value={provider}
                onChange={(e) => onProviderChange(e.target.value)}
                placeholder="e.g. OpenAI"
                className="w-full rounded-lg border border-surface-300 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
            </div>
          </div>
          <div>
            <label className="block text-xs font-medium text-surface-500 mb-1">Model ID</label>
            <input
              type="text"
              value={modelId}
              onChange={(e) => onModelIdChange(e.target.value)}
              placeholder="e.g. gpt-4o"
              className="w-full rounded-lg border border-surface-300 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
            />
          </div>
          <div>
            <label className="block text-xs font-medium text-surface-500 mb-1">Tier</label>
            <select
              value={tier}
              onChange={(e) => onTierChange(e.target.value as Model['tier'])}
              className="w-full rounded-lg border border-surface-300 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
            >
              <option value="free">Free</option>
              <option value="economy">Economy</option>
              <option value="standard">Standard</option>
              <option value="premium">Premium</option>
            </select>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div>
              <label className="block text-xs font-medium text-surface-500 mb-1">Cost per 1K input ($)</label>
              <input
                type="number"
                step="0.0001"
                value={costIn}
                onChange={(e) => onCostInChange(Number(e.target.value))}
                className="w-full rounded-lg border border-surface-300 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
            </div>
            <div>
              <label className="block text-xs font-medium text-surface-500 mb-1">Cost per 1K output ($)</label>
              <input
                type="number"
                step="0.0001"
                value={costOut}
                onChange={(e) => onCostOutChange(Number(e.target.value))}
                className="w-full rounded-lg border border-surface-300 px-3 py-2 text-sm focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
            </div>
          </div>
          <div className="flex justify-end gap-2 pt-2">
            <Button variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button onClick={onSave} disabled={!isValid}>
              Add Model
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
