import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import type { RoutingStrategy } from '../../lib/api'
import { api } from '../../lib/api'
import { qk } from '../../lib/query'
import { useApiMutation } from '../../lib/useApiMutation'
import { FeatureStatus } from '../settings/FeatureStatus'
import { FeatureTabLayout } from '../settings/FeatureTabLayout'
import { QueryProvider } from '../ui/query-provider'
import { StrategyEditor } from './StrategyEditor'
import { StrategyListView } from './StrategyListView'

function StrategiesViewContent() {
  const [editingStrategy, setEditingStrategy] = useState<RoutingStrategy | null>(null)
  const [toggling, setToggling] = useState(false)

  const { data: flags = [] } = useQuery({
    queryKey: qk.features,
    queryFn: () => api.features.getFeatures(),
  })

  const toggleMutation = useApiMutation(
    (args: { feature_key: string; enabled: boolean }) => api.features.toggleFeature(args.feature_key, args.enabled),
    { invalidate: [qk.features] },
  )

  const handleToggle = async () => {
    const routerFlag = flags.find((f) => f.feature_key === 'smart_router')
    if (!routerFlag) return
    setToggling(true)
    try {
      await toggleMutation.mutateAsync({ feature_key: 'smart_router', enabled: !routerFlag.enabled })
      toast.success(`Smart Router ${!routerFlag.enabled ? 'enabled' : 'disabled'}`)
    } catch {
      toast.error('Failed to toggle Smart Router')
    } finally {
      setToggling(false)
    }
  }

  const routerEnabled = flags.find((f) => f.feature_key === 'smart_router')?.enabled ?? true

  if (editingStrategy) {
    return <StrategyEditor strategy={editingStrategy} onBack={() => setEditingStrategy(null)} />
  }

  return (
    <FeatureTabLayout
      title="Smart Router"
      description="Configure routing strategies to optimize model selection based on query complexity and requirements."
      status={
        <FeatureStatus
          type="toggle"
          enabled={routerEnabled}
          onToggle={handleToggle}
          disabled={toggling}
          label={routerEnabled ? 'Enabled' : 'Disabled'}
        />
      }
      config={<StrategyListView onEdit={setEditingStrategy} />}
      enabled={routerEnabled}
    />
  )
}

export function StrategiesView() {
  return (
    <QueryProvider>
      <StrategiesViewContent />
    </QueryProvider>
  )
}
