import { Button } from '../../ui/button'
import { Card, CardContent } from '../../ui/card'
import { ChevronDown, Settings } from '../../ui/icons'
import type { ProviderInfo } from '../useProviders'
import { StatusBadge } from './StatusBadge'

function isEnvSource(source: string): boolean {
  return source.startsWith('ILTER_PROVIDER_')
}

interface ProviderCardProps {
  provider: ProviderInfo
  isExpanded: boolean
  onToggleModels: (name: string) => void
  onOpenConfig: (provider: ProviderInfo) => void
}

export function ProviderCard({ provider, isExpanded, onToggleModels, onOpenConfig }: ProviderCardProps) {
  return (
    <Card>
      <CardContent className="p-4">
        <div className="flex items-start justify-between mb-3">
          <div className="flex items-center gap-3">
            <span className={`inline-block h-3 w-3 rounded-full ${statusDotColor(provider.status)}`} />
            <div>
              <p className="text-sm font-semibold text-surface-900">{provider.name}</p>
              <p className="text-xs text-surface-500 font-mono mt-0.5">{provider.type}</p>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <StatusBadge status={provider.status} />
            {isEnvSource(provider.api_key_source) && (
              <span className="relative group rounded-full px-2 py-0.5 text-xs font-mono font-medium bg-purple-50 text-purple-700 cursor-help">
                ENV
                <span className="absolute bottom-full left-1/2 -translate-x-1/2 mb-1.5 px-2.5 py-1 text-[11px] bg-gray-900 text-white rounded-md opacity-0 group-hover:opacity-100 whitespace-nowrap pointer-events-none transition-opacity z-10 shadow-lg">
                  {provider.api_key_source}
                </span>
              </span>
            )}
            {provider.api_keys_count && provider.api_keys_count > 1 ? (
              <span className="rounded-full px-2 py-0.5 text-xs font-medium bg-emerald-50 text-emerald-700 border border-emerald-200">
                {provider.api_keys_count} Keys
              </span>
            ) : null}
            {provider.circuit_breaker_state && provider.circuit_breaker_state !== 'closed' && (
              <span className="rounded-full px-2 py-0.5 text-xs font-medium bg-error/10 text-error">
                CB: {provider.circuit_breaker_state}
              </span>
            )}
            <Button variant="outline" size="sm" onClick={() => onOpenConfig(provider)}>
              <Settings size={14} />
              Configure
            </Button>
          </div>
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 mb-3">
          <div className="rounded-lg bg-surface-50 p-3">
            <p className="text-xs text-surface-500 mb-1">Base URL</p>
            <p className="text-sm font-mono text-surface-800 truncate">{provider.base_url}</p>
          </div>
          <div className="rounded-lg bg-surface-50 p-3">
            <p className="text-xs text-surface-500 mb-1">Active Models</p>
            <p className="text-sm font-semibold text-surface-900">
              {provider.active_models ?? 0} / {provider.total_models ?? 0}
            </p>
          </div>
          <div className="rounded-lg bg-surface-50 p-3">
            <p className="text-xs text-surface-500 mb-1">Circuit Breaker</p>
            <p className="text-sm font-semibold text-surface-900 capitalize">{provider.circuit_breaker_state || '—'}</p>
          </div>
        </div>

        {provider.circuit_breaker_state && provider.circuit_breaker_state !== 'closed' && (
          <div className="rounded-lg bg-warning/5 border border-warning/20 p-3 mb-3">
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-3">
              <div>
                <p className="text-xs text-surface-500 mb-0.5">Requests</p>
                <p className="text-sm font-semibold text-surface-900">{provider.total_requests ?? 0}</p>
              </div>
              <div>
                <p className="text-xs text-surface-500 mb-0.5">Errors</p>
                <p className="text-sm font-semibold text-error">{provider.total_errors ?? 0}</p>
              </div>
              <div>
                <p className="text-xs text-surface-500 mb-0.5">Success Rate</p>
                <p className="text-sm font-semibold text-surface-900">{provider.success_rate?.toFixed(1) ?? '0.0'}%</p>
              </div>
              <div>
                <p className="text-xs text-surface-500 mb-0.5">State</p>
                <p className="text-sm font-semibold text-warning capitalize">{provider.circuit_breaker_state}</p>
              </div>
            </div>
            {(provider.last_error_time || provider.last_success_time) && (
              <div className="grid grid-cols-2 gap-3 mt-2 pt-2 border-t border-warning/10">
                {provider.last_error_time && (
                  <div>
                    <p className="text-xs text-surface-500 mb-0.5">Last Error</p>
                    <p className="text-xs font-mono text-error">
                      {new Date(provider.last_error_time).toLocaleString()}
                    </p>
                  </div>
                )}
                {provider.last_success_time && (
                  <div>
                    <p className="text-xs text-surface-500 mb-0.5">Last Success</p>
                    <p className="text-xs font-mono text-success">
                      {new Date(provider.last_success_time).toLocaleString()}
                    </p>
                  </div>
                )}
              </div>
            )}
          </div>
        )}

        {provider.models.length > 0 && (
          <div className="border-t border-surface-100 pt-3">
            <button
              type="button"
              onClick={() => onToggleModels(provider.name)}
              className="flex items-center gap-1.5 w-full text-left mb-2"
            >
              <ChevronDown
                size={14}
                className={`text-surface-400 transition-transform duration-200 ${
                  isExpanded ? 'rotate-0' : '-rotate-90'
                }`}
              />
              <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">
                Models ({provider.models.length})
              </p>
            </button>
            {isExpanded && (
              <div className="pl-5">
                {[...new Set(provider.models.map((m) => m.tier || 'other'))].map((tier) => {
                  const group = provider.models.filter((m) => (m.tier || 'other') === tier)
                  return (
                    <div key={tier} className="mb-2 last:mb-0">
                      <p className="text-[10px] font-medium text-surface-400 uppercase tracking-wider mb-1.5">{tier}</p>
                      <div className="flex flex-wrap gap-1.5">
                        {group.map((m) => (
                          <span
                            key={m.name}
                            className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium ${
                              m.active ? 'bg-brand-50 text-brand-700' : 'bg-surface-100 text-surface-400'
                            }`}
                          >
                            <span
                              className={`inline-block h-1.5 w-1.5 rounded-full ${m.active ? 'bg-success' : 'bg-surface-300'}`}
                            />
                            {m.name}
                          </span>
                        ))}
                      </div>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  )
}

function statusDotColor(status: string): string {
  switch (status) {
    case 'online':
      return 'bg-success'
    case 'degraded':
      return 'bg-warning'
    default:
      return 'bg-error'
  }
}
