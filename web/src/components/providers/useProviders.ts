import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import { api } from '../../lib/api'
import { logger } from '../../lib/logger'
import { qk } from '../../lib/query'
import { useApiMutation } from '../../lib/useApiMutation'

export interface ProviderModelItem {
  name: string
  active: boolean
  tier?: string
}

export interface ProviderInfo {
  name: string
  type: string
  base_url: string
  models: ProviderModelItem[]
  active_models: number
  total_models: number
  status: 'online' | 'offline' | 'degraded'
  circuit_breaker_state: string
  total_requests: number
  total_errors: number
  success_rate: number
  last_error_time: string | null
  last_success_time: string | null
  api_key_set: boolean
  api_key_source: string
  api_keys_count?: number
}

export function useProviders() {
  const { data: providers = [], isLoading } = useQuery({
    queryKey: qk.providers,
    queryFn: () =>
      api.providers
        .getProviders()
        .then((r) => r as ProviderInfo[])
        .catch((err) => {
          logger.error('Failed to fetch providers:', err)
          return [] as ProviderInfo[]
        }),
  })

  const saveProvider = useApiMutation(
    (args: { name: string; baseUrl: string; apiKey: string | null; apiKeys?: string[] }) =>
      api.providers.updateProvider(args.name, args.baseUrl, args.apiKey, args.apiKeys),
    { invalidate: [qk.providers] },
  )

  const [configProvider, setConfigProvider] = useState<ProviderInfo | null>(null)
  const [configForm, setConfigForm] = useState<{ base_url: string; api_key: string; api_keys: string[] }>({
    base_url: '',
    api_key: '',
    api_keys: [],
  })
  const [apiKeyTouched, setApiKeyTouched] = useState(false)
  const [multiKeysTouched, setMultiKeysTouched] = useState(false)
  const [expandedProviders, setExpandedProviders] = useState<Set<string>>(new Set())

  const toggleModels = (name: string) => {
    setExpandedProviders((prev) => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  const openConfig = (p: ProviderInfo) => {
    setConfigForm({ base_url: p.base_url, api_key: '', api_keys: [] })
    setApiKeyTouched(false)
    setMultiKeysTouched(false)
    setConfigProvider(p)
  }

  const handleSaveConfig = async () => {
    if (!configProvider) return
    try {
      await saveProvider.mutateAsync({
        name: configProvider.name,
        baseUrl: configForm.base_url,
        apiKey: apiKeyTouched ? configForm.api_key : null,
        apiKeys: multiKeysTouched ? configForm.api_keys : undefined,
      })
      toast.success('Provider updated', { description: `${configProvider.name} configuration saved.` })
      setConfigProvider(null)
    } catch {
      toast.error('Update failed', { description: `Could not update ${configProvider.name}.` })
    }
  }

  const closeConfig = () => setConfigProvider(null)

  return {
    providers,
    isLoading,
    saveProvider,
    configProvider,
    configForm,
    setConfigForm,
    apiKeyTouched,
    setApiKeyTouched,
    multiKeysTouched,
    setMultiKeysTouched,
    expandedProviders,
    toggleModels,
    openConfig,
    handleSaveConfig,
    closeConfig,
  }
}
