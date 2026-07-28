import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import { api } from '../../lib/api'
import { logger } from '../../lib/logger'
import { qk, queryClient } from '../../lib/query'
import { useApiMutation } from '../../lib/useApiMutation'
import { useExport } from '../ui/useExport'

export interface Model {
  id: string
  name: string
  provider: string
  model: string
  is_active: boolean
  tier: 'free' | 'economy' | 'standard' | 'premium'
  cost_per_1k_in: number
  cost_per_1k_out: number
}

export const tiers = ['free', 'economy', 'standard', 'premium'] as const
export type ModelTier = (typeof tiers)[number]

/** Formats small cost values (per-1K) concisely.
 *  - $0 → "0"
 *  - $0.0000014 → "0.000001" (strips trailing zeros)
 *  - $0.001 → "0.001"
 *  - $0.14 → "0.14"
 */
export function formatCost(cost: number): string {
  if (cost === 0) return '0'
  const s = cost.toFixed(10).replace(/\.?0+$/, '')
  if (s.length > 8) {
    return Number(cost)
      .toFixed(6)
      .replace(/\.?0+$/, '')
  }
  return s
}

export function useModels() {
  const { exportCsv } = useExport()

  const {
    data: models = [],
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: qk.models,
    queryFn: () =>
      api.models.getModelProviders().then((items) =>
        items.map((item) => ({
          id: item.id,
          name: item.name,
          provider: item.provider,
          model: item.model,
          is_active: item.is_active,
          tier: (item.tier || 'standard') as Model['tier'],
          cost_per_1k_in: item.cost_per_1k_in || 0,
          cost_per_1k_out: item.cost_per_1k_out || 0,
        })),
      ),
  })

  const [search, setSearch] = useState('')
  const [tierFilter, setTierFilter] = useState<string | null>(null)
  const [showAddModal, setShowAddModal] = useState(false)
  const [configModel, setConfigModel] = useState<Model | null>(null)
  const [configForm, setConfigForm] = useState({
    name: '',
    provider: '',
    model: '',
    tier: 'standard' as Model['tier'],
    cost_in: 0,
    cost_out: 0,
  })

  const filtered = models.filter((m) => {
    if (tierFilter && m.tier !== tierFilter) return false
    const q = search.toLowerCase()
    return m.name.toLowerCase().includes(q) || m.provider.toLowerCase().includes(q) || m.model.toLowerCase().includes(q)
  })

  const toggleModelMutation = useApiMutation(
    ({ name, active }: { name: string; active: boolean }) => api.models.toggleModel(name, active),
    { invalidate: [qk.models] },
  )

  const toggleModel = async (id: string) => {
    const model = models.find((m) => m.id === id)
    if (!model) return
    const newActive = !model.is_active
    queryClient.setQueryData(qk.models, (old: Model[] | undefined) =>
      (old || []).map((m) => (m.id === id ? { ...m, is_active: newActive } : m)),
    )
    try {
      await toggleModelMutation.mutateAsync({ name: model.name, active: newActive })
      toast.success(newActive ? 'Model enabled' : 'Model disabled', {
        description: `${model.name} is now ${newActive ? 'active' : 'disabled'}.`,
      })
    } catch (e) {
      logger.error('Failed to toggle model', { name: model.name, active: newActive, error: e })
      queryClient.setQueryData(qk.models, (old: Model[] | undefined) =>
        (old || []).map((m) => (m.id === id ? { ...m, is_active: !newActive } : m)),
      )
      toast.error('Toggle failed', { description: `Could not update ${model.name}.` })
    }
  }

  const updateTier = useApiMutation(
    ({ name, tier }: { name: string; tier: string }) => api.models.updateModelTier(name, tier),
    { invalidate: [qk.models] },
  )

  const handleSaveConfig = async () => {
    if (!configModel) return
    try {
      await updateTier.mutateAsync({ name: configModel.name, tier: configModel.tier })
      toast.success('Model updated', { description: `${configModel.name} tier set to ${configModel.tier}.` })
      setConfigModel(null)
    } catch (e) {
      logger.error('Failed to update model tier', { name: configModel.name, tier: configModel.tier, error: e })
      toast.error('Update failed', { description: `Could not update ${configModel.name}.` })
    }
  }

  const addModel = useApiMutation(
    (data: { name: string; provider: string; model: string; tier: string; cost_in: number; cost_out: number }) =>
      api.models.updateModelTier(data.name, data.tier),
    { invalidate: [qk.models] },
  )

  const handleAddProvider = () => {
    const name = configForm.name
    const newModel: Model = {
      id: `m${Date.now()}`,
      name: configForm.name,
      provider: configForm.provider,
      model: configForm.model,
      is_active: true,
      tier: configForm.tier,
      cost_per_1k_in: configForm.cost_in,
      cost_per_1k_out: configForm.cost_out,
    }
    queryClient.setQueryData(qk.models, (old: Model[] | undefined) => [...(old || []), newModel])
    setShowAddModal(false)
    setConfigForm({ name: '', provider: '', model: '', tier: 'standard', cost_in: 0, cost_out: 0 })
    toast.success('Model added', { description: `"${name}" has been added to the registry.` })
    addModel.mutate({
      name: configForm.name,
      provider: configForm.provider,
      model: configForm.model,
      tier: configForm.tier,
      cost_in: configForm.cost_in,
      cost_out: configForm.cost_out,
    })
  }

  const exportModels = () => {
    exportCsv(
      filtered.map((m) => ({
        Name: m.name,
        Provider: m.provider,
        Model: m.model,
        Status: m.is_active ? 'Active' : 'Disabled',
        Tier: m.tier,
        'Cost/1K In': m.cost_per_1k_in,
        'Cost/1K Out': m.cost_per_1k_out,
      })),
      [
        { key: 'Name' as const, header: 'Name' },
        { key: 'Provider' as const, header: 'Provider' },
        { key: 'Model' as const, header: 'Model' },
        { key: 'Status' as const, header: 'Status' },
        { key: 'Tier' as const, header: 'Tier' },
        { key: 'Cost/1K In' as const, header: 'Cost/1K In' },
        { key: 'Cost/1K Out' as const, header: 'Cost/1K Out' },
      ],
      'models.csv',
    )
  }

  return {
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
  }
}
