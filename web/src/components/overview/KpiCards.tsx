import type { DashboardStats, FeatureFlag } from '../../lib/api'
import { Activity, AlertTriangle, DollarSign, KeyRound, Shield, ShieldCheck } from '../ui/icons'
import { StatCard } from '../ui/StatCard'
import { Skeleton } from '../ui/skeleton'

interface KpiCardsProps {
  stats: DashboardStats | null
  loading: boolean
  features: FeatureFlag[]
}

export function KpiCards({ stats, loading, features }: KpiCardsProps) {
  if (loading) {
    return (
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-4">
        {Array.from({ length: 6 }).map((_, i) => (
          <div key={i} className="rounded-xl border border-surface-200 bg-white p-6 shadow-card">
            <Skeleton className="h-4 w-1/3 mb-4" />
            <Skeleton className="h-8 w-1/2 mb-3" />
            <Skeleton className="h-3 w-2/3 mb-2" />
            <Skeleton className="h-3 w-1/2 mb-2" />
            <Skeleton className="h-3 w-3/4" />
          </div>
        ))}
      </div>
    )
  }

  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-6 gap-4">
      <StatCard
        title="Total Requests (24h)"
        value={(stats?.total_requests_24h ?? 0).toLocaleString()}
        icon={<Activity className="h-5 w-5" />}
      />
      <StatCard
        title="Total Cost (24h)"
        value={`$${(stats?.total_cost_24h ?? 0).toFixed(2)}`}
        icon={<DollarSign className="h-5 w-5" />}
      />
      <StatCard
        title="Active API Keys"
        value={String(stats?.active_keys ?? 0)}
        icon={<KeyRound className="h-5 w-5" />}
      />
      <StatCard
        title="Active Features"
        value={`${features.filter((f) => f.enabled).length}/${features.length}`}
        icon={<ShieldCheck className="h-5 w-5" />}
      />
      <StatCard
        title="Error Rate"
        value={`${(stats?.error_rate_pct ?? 0).toFixed(1)}%`}
        icon={<AlertTriangle className="h-5 w-5" />}
      />
      <StatCard
        title="Blocked Requests"
        value={(stats?.blocked_requests_24h ?? 0).toLocaleString()}
        icon={<Shield className="h-5 w-5" />}
      />
    </div>
  )
}
