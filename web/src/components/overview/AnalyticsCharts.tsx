import type { CostSummary, DashboardStats } from '../../lib/api'
import { CHART_COLORS } from '../../lib/recharts-theme'
import LazyChart from '../charts/LazyChart'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'

interface AnalyticsChartsProps {
  costSummary: CostSummary | null
  stats: DashboardStats | null
}

const emptyCostSummary: CostSummary = {
  total_cost: 0,
  total_requests: 0,
  avg_cost_per_request: 0,
  daily_costs: [],
  model_breakdown: [],
  provider_breakdown: [],
}

export function AnalyticsCharts({ costSummary, stats }: AnalyticsChartsProps) {
  const cs = costSummary ?? emptyCostSummary
  const hasCostData = cs.daily_costs.length > 0
  const hasProviderData = cs.provider_breakdown.length > 0
  const hasModelData = cs.model_breakdown.length > 0
  const errorRate = stats?.error_rate_pct ?? 0

  const errorColor = errorRate > 5 ? CHART_COLORS.error : errorRate > 2 ? CHART_COLORS.warning : CHART_COLORS.success

  return (
    <section>
      <h2 className="text-lg font-semibold text-surface-900 mb-4">Analytics</h2>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Daily Cost Trend</CardTitle>
          </CardHeader>
          <CardContent>
            {hasCostData ? (
              <LazyChart
                type="area"
                data={cs.daily_costs}
                xKey="date"
                height={256}
                series={[{ dataKey: 'cost', color: CHART_COLORS.brand, name: 'Cost' }]}
                gradient={{ color: CHART_COLORS.brand, startOpacity: 0.25, endOpacity: 0.02 }}
                yAxisFormatter={(v: number) => `$${v}`}
              />
            ) : (
              <div className="h-64 flex items-center justify-center text-sm text-surface-400">No cost data yet</div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Cost by Provider</CardTitle>
          </CardHeader>
          <CardContent>
            {hasProviderData ? (
              <LazyChart
                type="pie"
                data={cs.provider_breakdown}
                height={256}
                series={{ dataKey: 'cost', nameKey: 'provider' }}
                label={({ provider, percent }: { provider?: string; percent?: number }) =>
                  `${provider} ${((percent ?? 0) * 100).toFixed(0)}%`
                }
              />
            ) : (
              <div className="h-64 flex items-center justify-center text-sm text-surface-400">No cost data yet</div>
            )}
          </CardContent>
        </Card>
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6 mt-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Model Distribution</CardTitle>
          </CardHeader>
          <CardContent>
            {hasModelData ? (
              <LazyChart
                type="pie"
                data={cs.model_breakdown}
                height={256}
                series={{ dataKey: 'calls', nameKey: 'model' }}
                label={({ model, percent }: { model?: string; percent?: number }) =>
                  `${model} ${((percent ?? 0) * 100).toFixed(0)}%`
                }
              />
            ) : (
              <div className="h-64 flex items-center justify-center text-sm text-surface-400">No model data yet</div>
            )}
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Error Rate</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="h-48 flex items-center justify-center">
              <div className="text-center">
                <div className="text-5xl font-bold font-mono" style={{ color: errorColor }}>
                  {errorRate.toFixed(1)}%
                </div>
                <div className="text-sm text-surface-500 mt-2">Error rate (24h)</div>
                <div className="mt-4 w-72 mx-auto">
                  <div className="flex justify-between text-xs text-surface-400 mb-1">
                    <span>0%</span>
                    <span>Target 1%</span>
                    <span>10%</span>
                  </div>
                  <div className="w-full bg-surface-200 rounded-full h-3 overflow-hidden">
                    <div
                      className="h-3 rounded-full transition-all duration-500"
                      style={{
                        width: `${Math.min((errorRate / 10) * 100, 100)}%`,
                        backgroundColor: errorColor,
                      }}
                    />
                  </div>
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      </div>
    </section>
  )
}
