import { QueryProvider } from '../ui/query-provider'
import { ActiveProviders } from './ActiveProviders'
import { AnalyticsCharts } from './AnalyticsCharts'
import { FeatureControlCenter } from './FeatureControlCenter'
import { KpiCards } from './KpiCards'
import { useOverview } from './useOverview'

function OverviewDashboardContent() {
  const { stats, providers, providersLoading, costSummary, features, toggleFeature, loading } = useOverview()

  return (
    <div className="space-y-6">
      <KpiCards stats={stats} loading={loading} features={features} />
      <AnalyticsCharts costSummary={costSummary} stats={stats} />
      {features.length > 0 && <FeatureControlCenter features={features} onToggle={toggleFeature} />}
      <ActiveProviders providers={providers} loading={providersLoading} />
    </div>
  )
}

export function OverviewDashboard() {
  return (
    <QueryProvider>
      <OverviewDashboardContent />
    </QueryProvider>
  )
}
