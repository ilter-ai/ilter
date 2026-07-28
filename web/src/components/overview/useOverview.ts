import { useQuery } from '@tanstack/react-query'
import { useMemo } from 'react'
import { toast } from 'sonner'
import { api } from '../../lib/api'
import { ApiError } from '../../lib/api/request'
import { qk } from '../../lib/query'

export interface ProviderStatus {
  name: string
  status: 'online' | 'offline' | 'degraded'
  activeModels: number
  totalModels: number
}

const REFETCH_INTERVAL = 30_000

export function useOverview() {
  const { data: stats, isLoading: statsLoading } = useQuery({
    queryKey: qk.dashboardStats,
    queryFn: api.dashboard.getDashboardStats,
    refetchInterval: REFETCH_INTERVAL,
  })

  const { data: providerInfos, isLoading: providersLoading } = useQuery({
    queryKey: qk.providers,
    queryFn: api.providers.getProviders,
    refetchInterval: REFETCH_INTERVAL,
  })

  const { data: costSummary } = useQuery({
    queryKey: qk.costSummary('7d'),
    queryFn: () => api.costs.getCostSummary('7d'),
  })

  const { data: features, refetch: refetchFeatures } = useQuery({
    queryKey: qk.features,
    queryFn: api.features.getFeatures,
  })

  // Real per-provider health, derived from live circuit-breaker state on the
  // backend (see providers.HandleProviders) — not merely "is a model toggled on".
  const providers: ProviderStatus[] = useMemo(() => {
    if (!providerInfos) return []
    return providerInfos.map((p) => ({
      name: p.name,
      status: (p.status as ProviderStatus['status']) || 'offline',
      activeModels: p.active_models,
      totalModels: p.total_models,
    }))
  }, [providerInfos])

  const toggleFeature = async (featureKey: string) => {
    const feature = features?.find((f) => f.feature_key === featureKey)
    if (!feature) return
    const newEnabled = !feature.enabled
    try {
      await api.features.toggleFeature(featureKey, newEnabled)
      await refetchFeatures()
      toast.success(newEnabled ? `${feature.feature_key} enabled` : `${feature.feature_key} disabled`, {
        description: `${feature.feature_key} is now ${newEnabled ? 'active' : 'disabled'}.`,
      })
    } catch (err) {
      const msg = err instanceof ApiError ? err.message : `Could not update ${feature.feature_key}.`
      toast.error('Toggle failed', { description: msg })
    }
  }

  return {
    stats: stats ?? null,
    statsLoading,
    providers,
    providersLoading,
    costSummary: costSummary ?? null,
    features: features ?? [],
    toggleFeature,
    loading: statsLoading,
  }
}
