import { Skeleton } from '../ui/skeleton'

interface ProviderStatus {
  name: string
  status: 'online' | 'offline' | 'degraded'
  activeModels: number
  totalModels: number
}

interface ActiveProvidersProps {
  providers: ProviderStatus[]
  loading: boolean
}

const statusStyles: Record<string, string> = {
  online: 'bg-success',
  degraded: 'bg-warning',
  offline: 'bg-error',
}

const statusLabels: Record<string, string> = {
  online: 'Online',
  degraded: 'Degraded',
  offline: 'Offline',
}

const statusColors: Record<string, string> = {
  online: 'text-success',
  degraded: 'text-warning',
  offline: 'text-error',
}

export function ActiveProviders({ providers, loading }: ActiveProvidersProps) {
  const activeProviders = providers.filter((p) => p.status !== 'offline')

  return (
    <section>
      <h2 className="text-lg font-semibold text-surface-900 mb-4">Active Providers</h2>
      {loading ? (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <div key={i} className="rounded-xl border border-surface-200 bg-white p-6 shadow-card">
              <Skeleton className="h-4 w-1/3 mb-4" />
              <Skeleton className="h-8 w-1/2 mb-3" />
              <Skeleton className="h-3 w-2/3 mb-2" />
              <Skeleton className="h-3 w-1/2 mb-2" />
              <Skeleton className="h-3 w-3/4" />
            </div>
          ))}
        </div>
      ) : activeProviders.length === 0 ? (
        <div className="rounded-xl border border-surface-200 bg-white p-6 text-center text-surface-400 text-sm">
          No active providers
        </div>
      ) : (
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-3">
          {activeProviders.map((provider) => (
            <div
              key={provider.name}
              className="rounded-xl border border-surface-200 bg-white p-4 shadow-card flex items-center justify-between"
            >
              <div className="flex items-center gap-3">
                <span className={`inline-block h-2.5 w-2.5 rounded-full ${statusStyles[provider.status]}`} />
                <div>
                  <p className="text-sm font-medium text-surface-900">{provider.name}</p>
                  <p className="text-xs text-surface-500">
                    {provider.activeModels}/{provider.totalModels} models
                  </p>
                </div>
              </div>
              <span className={`text-xs font-medium ${statusColors[provider.status]}`}>
                {statusLabels[provider.status]}
              </span>
            </div>
          ))}
        </div>
      )}
    </section>
  )
}
