import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import {
  type APIKey,
  api,
  type BudgetKeyItem,
  type Group,
  type GroupBudgetItem,
  type User,
  type UserBudgetItem,
} from '../../lib/api'
import { qk } from '../../lib/query'
import { CHART_COLORS, PALETTE } from '../../lib/recharts-theme'
import { useApiMutation } from '../../lib/useApiMutation'
import { useAutoSelectFirst } from '../../lib/useAutoSelectFirst'
import LazyChart from '../charts/LazyChart'
import { FeatureStatus } from '../settings/FeatureStatus'
import { FeatureTabLayout } from '../settings/FeatureTabLayout'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { Activity, AlertTriangle, DollarSign, PiggyBank, TrendingUp, Wallet } from '../ui/icons'
import { LevelScopeSelector } from '../ui/LevelScopeSelector'
import { QueryProvider } from '../ui/query-provider'
import { SearchableList } from '../ui/SearchableList'
import { StatCard } from '../ui/StatCard'

const WalletIcon = () => <Wallet size={20} />
const PiggyBankIcon = () => <PiggyBank size={20} />
const AlertTriangleIcon = () => <AlertTriangle size={20} />
const TrendingUpIcon = () => <TrendingUp size={20} />

const costPeriods = [
  { label: '24h', value: '24h' },
  { label: '7d', value: '7d' },
  { label: '30d', value: '30d' },
  { label: '90d', value: '90d' },
]

function StatusBadge({ status }: { status: BudgetKeyItem['status'] }) {
  const config: Record<string, { color: string; bg: string; label: string }> = {
    ok: { color: 'text-success border-success/20', bg: 'bg-success/10', label: 'OK' },
    warning: { color: 'text-warning border-warning/20', bg: 'bg-warning/10', label: 'Warning' },
    critical: { color: 'text-error border-error/20', bg: 'bg-error/10', label: 'Critical' },
    depleted: { color: 'text-surface-600 border-surface-300', bg: 'bg-surface-100', label: 'Depleted' },
  }
  const s = config[status] || config.ok
  return (
    <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${s.color} ${s.bg}`}>
      {s.label}
    </span>
  )
}

function UserScopeView() {
  const [search, setSearch] = useState('')
  const [selectedUser, setSelectedUser] = useState<User | null>(null)
  const [userBudget, setUserBudget] = useState<UserBudgetItem | null>(null)
  const [monthlyInput, setMonthlyInput] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const { data: users = [], isLoading } = useQuery({
    queryKey: qk.users,
    queryFn: () => api.users.getUsers().catch(() => [] as User[]),
  })

  async function selectUser(user: User) {
    setSelectedUser(user)
    setError(null)
    try {
      const ub = await api.budget.getUserBudget(user.id)
      setUserBudget(ub)
      setMonthlyInput(String(ub.monthly_budget))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load user budget')
    }
  }

  async function saveBudget() {
    if (!selectedUser) return
    setSaving(true)
    setError(null)
    try {
      await api.budget.setUserBudget(selectedUser.id, Number(monthlyInput))
      const ub = await api.budget.getUserBudget(selectedUser.id)
      setUserBudget(ub)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save user budget')
    } finally {
      setSaving(false)
    }
  }

  const filtered = users.filter(
    (u) => u.name.toLowerCase().includes(search.toLowerCase()) || u.email.toLowerCase().includes(search.toLowerCase()),
  )

  useAutoSelectFirst(filtered, selectedUser, isLoading, selectUser)

  return (
    <div className="space-y-4">
      <SearchableList
        items={filtered}
        isLoading={isLoading}
        loadingLabel="Loading users..."
        search={search}
        onSearchChange={setSearch}
        searchPlaceholder="Search users by name or email..."
        error={error}
        isSelected={(u) => selectedUser?.id === u.id}
        onSelect={selectUser}
        getKey={(u) => u.id}
        emptyMessage="No users found"
        renderItem={(u) => (
          <>
            <span className="font-medium text-surface-900">{u.name}</span>
            <span className="ml-2 text-surface-500">{u.email}</span>
          </>
        )}
      />

      {selectedUser && userBudget && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">User Budget — {selectedUser.name}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-4">
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Monthly Limit</p>
                <p className="text-xl font-bold font-mono text-surface-900">${userBudget.monthly_budget.toFixed(2)}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Monthly Spent</p>
                <p className="text-xl font-bold font-mono text-surface-900">${userBudget.monthly_spent.toFixed(2)}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Daily Spent</p>
                <p className="text-xl font-bold font-mono text-surface-900">${userBudget.daily_spent.toFixed(2)}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Status</p>
                <StatusBadge status={userBudget.status} />
              </div>
            </div>
            <div className="flex items-end gap-3">
              <div className="flex-1">
                <label className="block text-xs font-medium text-surface-500 mb-1">Set Monthly Budget</label>
                <input
                  type="number"
                  min={0}
                  step={0.01}
                  value={monthlyInput}
                  onChange={(e) => setMonthlyInput(e.target.value)}
                  className="w-full rounded-lg border border-surface-200 bg-white px-3 py-2 text-sm font-mono text-surface-900 focus:outline-none focus:ring-2 focus:ring-brand-500"
                />
              </div>
              <button
                type="button"
                onClick={saveBudget}
                disabled={saving}
                className="rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white hover:bg-brand-700 disabled:opacity-50 transition-colors"
              >
                {saving ? 'Saving...' : 'Update'}
              </button>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function GroupScopeView() {
  const [search, setSearch] = useState('')
  const [selectedGroup, setSelectedGroup] = useState<Group | null>(null)
  const [groupBudget, setGroupBudget] = useState<GroupBudgetItem | null>(null)
  const [monthlyInput, setMonthlyInput] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const { data: groups = [], isLoading } = useQuery({
    queryKey: qk.groups,
    queryFn: () => api.groups.getGroups().catch(() => [] as Group[]),
  })

  async function selectGroup(group: Group) {
    setSelectedGroup(group)
    setError(null)
    try {
      const gb = await api.budget.getGroupBudget(group.id)
      setGroupBudget(gb)
      setMonthlyInput(String(gb.monthly_budget))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load group budget')
    }
  }

  async function saveBudget() {
    if (!selectedGroup) return
    setSaving(true)
    setError(null)
    try {
      await api.budget.setGroupBudget(selectedGroup.id, Number(monthlyInput))
      const gb = await api.budget.getGroupBudget(selectedGroup.id)
      setGroupBudget(gb)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save group budget')
    } finally {
      setSaving(false)
    }
  }

  const filtered = groups.filter((g) => g.name.toLowerCase().includes(search.toLowerCase()))

  useAutoSelectFirst(filtered, selectedGroup, isLoading, selectGroup)

  return (
    <div className="space-y-4">
      <SearchableList
        items={filtered}
        isLoading={isLoading}
        loadingLabel="Loading groups..."
        search={search}
        onSearchChange={setSearch}
        searchPlaceholder="Search groups by name..."
        error={error}
        isSelected={(g) => selectedGroup?.id === g.id}
        onSelect={selectGroup}
        getKey={(g) => g.id}
        emptyMessage="No groups found"
        renderItem={(g) => (
          <>
            <span className="font-medium text-surface-900">{g.name}</span>
            {g.description && <span className="ml-2 text-surface-500">{g.description}</span>}
          </>
        )}
      />

      {selectedGroup && groupBudget && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Group Budget — {selectedGroup.name}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-4">
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Monthly Limit</p>
                <p className="text-xl font-bold font-mono text-surface-900">${groupBudget.monthly_budget.toFixed(2)}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Monthly Spent</p>
                <p className="text-xl font-bold font-mono text-surface-900">${groupBudget.monthly_spent.toFixed(2)}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Daily Spent</p>
                <p className="text-xl font-bold font-mono text-surface-900">${groupBudget.daily_spent.toFixed(2)}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Status</p>
                <StatusBadge status={groupBudget.status} />
              </div>
            </div>
            <div className="flex items-end gap-3">
              <div className="flex-1">
                <label className="block text-xs font-medium text-surface-500 mb-1">Set Monthly Budget</label>
                <input
                  type="number"
                  min={0}
                  step={0.01}
                  value={monthlyInput}
                  onChange={(e) => setMonthlyInput(e.target.value)}
                  className="w-full rounded-lg border border-surface-200 bg-white px-3 py-2 text-sm font-mono text-surface-900 focus:outline-none focus:ring-2 focus:ring-brand-500"
                />
              </div>
              <button
                type="button"
                onClick={saveBudget}
                disabled={saving}
                className="rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white hover:bg-brand-700 disabled:opacity-50 transition-colors"
              >
                {saving ? 'Saving...' : 'Update'}
              </button>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function KeyScopeView() {
  const [search, setSearch] = useState('')
  const [selectedKey, setSelectedKey] = useState<APIKey | null>(null)
  const [keyBudget, setKeyBudget] = useState<BudgetKeyItem | null>(null)
  const [monthlyBudgetUsd, setMonthlyBudgetUsd] = useState('')
  const [monthlyBudgetTokens, setMonthlyBudgetTokens] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const { data: keys = [], isLoading } = useQuery({
    queryKey: qk.apiKeys,
    queryFn: () => api.apiKeys.getAPIKeys().catch(() => [] as APIKey[]),
  })

  async function selectKey(key: APIKey) {
    setSelectedKey(key)
    setError(null)
    try {
      const kb = await api.budget.getKeyBudget(key.id)
      setKeyBudget(kb)
      setMonthlyBudgetUsd(String(kb.monthly_budget_usd))
      setMonthlyBudgetTokens(String(kb.monthly_budget_tokens))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load key budget')
    }
  }

  async function saveBudget() {
    if (!selectedKey) return
    setSaving(true)
    setError(null)
    try {
      const kb = await api.budget.setKeyBudget(selectedKey.id, Number(monthlyBudgetUsd), Number(monthlyBudgetTokens))
      setKeyBudget(kb)
      setMonthlyBudgetUsd(String(kb.monthly_budget_usd))
      setMonthlyBudgetTokens(String(kb.monthly_budget_tokens))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save key budget')
    } finally {
      setSaving(false)
    }
  }

  const filtered = keys.filter(
    (k) => k.name.toLowerCase().includes(search.toLowerCase()) || k.id.toLowerCase().includes(search.toLowerCase()),
  )

  useAutoSelectFirst(filtered, selectedKey, isLoading, selectKey)

  return (
    <div className="space-y-4">
      <SearchableList
        items={filtered}
        isLoading={isLoading}
        loadingLabel="Loading API keys..."
        search={search}
        onSearchChange={setSearch}
        searchPlaceholder="Search keys by name or ID..."
        error={error}
        isSelected={(k) => selectedKey?.id === k.id}
        onSelect={selectKey}
        getKey={(k) => k.id}
        emptyMessage="No API keys found"
        renderItem={(k) => (
          <>
            <span className="font-medium text-surface-900">{k.name}</span>
            <span className="ml-2 font-mono text-xs text-surface-500">{k.id.slice(0, 12)}...</span>
          </>
        )}
      />

      {selectedKey && keyBudget && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">API Key Budget — {selectedKey.name}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-4">
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Monthly Limit (USD)</p>
                <p className="text-xl font-bold font-mono text-surface-900">
                  ${keyBudget.monthly_budget_usd.toFixed(2)}
                </p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Monthly Spent</p>
                <p className="text-xl font-bold font-mono text-surface-900">${keyBudget.monthly_spent.toFixed(2)}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Daily Spent</p>
                <p className="text-xl font-bold font-mono text-surface-900">${keyBudget.daily_spent.toFixed(2)}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Status</p>
                <StatusBadge status={keyBudget.status} />
              </div>
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-4">
              <div>
                <label className="block text-xs font-medium text-surface-500 mb-1">Monthly Budget ($)</label>
                <input
                  type="number"
                  min={0}
                  step={0.01}
                  value={monthlyBudgetUsd}
                  onChange={(e) => setMonthlyBudgetUsd(e.target.value)}
                  className="w-full rounded-lg border border-surface-200 bg-white px-3 py-2 text-sm font-mono text-surface-900 focus:outline-none focus:ring-2 focus:ring-brand-500"
                />
              </div>
              <div>
                <label className="block text-xs font-medium text-surface-500 mb-1">Monthly Budget Tokens</label>
                <input
                  type="number"
                  min={0}
                  step={1}
                  value={monthlyBudgetTokens}
                  onChange={(e) => setMonthlyBudgetTokens(e.target.value)}
                  className="w-full rounded-lg border border-surface-200 bg-white px-3 py-2 text-sm font-mono text-surface-900 focus:outline-none focus:ring-2 focus:ring-brand-500"
                />
              </div>
            </div>
            <button
              type="button"
              onClick={saveBudget}
              disabled={saving}
              className="rounded-lg bg-brand-600 px-4 py-2 text-sm font-medium text-white hover:bg-brand-700 disabled:opacity-50 transition-colors"
            >
              {saving ? 'Saving...' : 'Update'}
            </button>
          </CardContent>
        </Card>
      )}
    </div>
  )
}

function BudgetManagementViewContent() {
  const [scope, setScope] = useState<'key' | 'user' | 'group'>('key')
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [costPeriod, setCostPeriod] = useState('7d')
  const [toggling, setToggling] = useState(false)

  const { data: flags } = useQuery({
    queryKey: qk.features,
    queryFn: () => api.features.getFeatures(),
  })
  const safeFlags = flags ?? []

  const toggleMutation = useApiMutation(
    (args: { feature_key: string; enabled: boolean }) => api.features.toggleFeature(args.feature_key, args.enabled),
    { invalidate: [qk.features, qk.budget] },
  )

  const handleToggle = async () => {
    const budgetFlag = safeFlags.find((f) => f.feature_key === 'budget')
    if (!budgetFlag) return
    setToggling(true)
    try {
      await toggleMutation.mutateAsync({ feature_key: 'budget', enabled: !budgetFlag.enabled })
      toast.success(`Budget ${!budgetFlag.enabled ? 'enabled' : 'disabled'}`)
    } catch {
      toast.error('Failed to toggle budget')
    } finally {
      setToggling(false)
    }
  }

  const budgetEnabled = safeFlags.find((f) => f.feature_key === 'budget')?.enabled ?? true

  const { data, isLoading, error, refetch } = useQuery({
    queryKey: qk.budget,
    queryFn: () => api.budget.getBudgetSummary(),
    refetchInterval: autoRefresh ? 30000 : false,
  })

  const { data: costData } = useQuery({
    queryKey: qk.costSummary(costPeriod),
    queryFn: () => api.costs.getCostSummary(costPeriod),
  })

  const { data: costByKeyData } = useQuery({
    queryKey: qk.costByKey(costPeriod),
    queryFn: () => api.costs.getCostByKey(costPeriod),
  })

  if (isLoading && !data) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <div className="flex flex-col items-center gap-3">
          <div className="h-8 w-8 animate-spin rounded-full border-4 border-surface-200 border-t-brand-600" />
          <p className="text-sm text-surface-500">Loading budget data...</p>
        </div>
      </div>
    )
  }

  if (error && !data) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <div className="text-center">
          <p className="text-error font-medium mb-2">Failed to load budget data</p>
          <p className="text-sm text-surface-500 mb-4">{error instanceof Error ? error.message : 'Unknown error'}</p>
          <button onClick={() => refetch()} className="text-sm text-brand-600 hover:text-brand-700 font-medium">
            Try again
          </button>
        </div>
      </div>
    )
  }

  if (!data) return null

  const totalBudget = data.keys.reduce((sum, k) => sum + k.monthly_budget_usd, 0)
  const totalSpent = data.keys.reduce((sum, k) => sum + k.monthly_spent, 0)
  const totalRemaining = totalBudget - totalSpent
  const overBudgetKeys = data.keys.filter((k) => k.status === 'critical' || k.status === 'depleted').length
  const atRiskKeys = data.keys.filter((k) => k.status === 'warning').length
  const usagePct = totalBudget > 0 ? ((totalSpent / totalBudget) * 100).toFixed(1) : '0'

  const costByKeyMap = new Map<string, number>()
  for (const item of costByKeyData?.by_key ?? []) {
    costByKeyMap.set(item.key_id, item.cost)
    if (item.api_key_name) costByKeyMap.set(item.api_key_name, item.cost)
  }

  const budgetConfig = (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <LevelScopeSelector value={scope} onChange={setScope} />
          <span className="text-xs text-surface-400">
            ${data.default_monthly_limit.toFixed(0)} monthly · ${data.default_daily_limit.toFixed(0)} daily ·{' '}
            {Math.round(data.alert_threshold * 100)}% threshold
          </span>
        </div>
        <div className="flex items-center gap-4">
          <div className="flex items-center gap-2">
            <button
              type="button"
              role="switch"
              aria-checked={autoRefresh}
              onClick={() => setAutoRefresh(!autoRefresh)}
              className={`relative inline-flex h-5 w-9 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 focus:outline-none focus:ring-2 focus:ring-brand-500 focus:ring-offset-2 ${
                autoRefresh ? 'bg-brand-600' : 'bg-surface-300'
              }`}
            >
              <span
                className={`pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ${autoRefresh ? 'translate-x-4' : 'translate-x-0'}`}
              />
            </button>
            <span className="text-sm text-surface-600">Auto-refresh (30s)</span>
          </div>
          {autoRefresh && (
            <span className="inline-flex items-center gap-1.5 text-xs text-surface-500">
              <span className="inline-block h-2 w-2 rounded-full bg-success animate-pulse" />
              Live
            </span>
          )}
          <div className="flex items-center gap-2 bg-surface-100 rounded-lg p-0.5">
            {costPeriods.map((p) => (
              <button
                key={p.value}
                onClick={() => setCostPeriod(p.value)}
                className={`px-3 py-1.5 text-sm font-medium rounded-md transition-colors ${
                  costPeriod === p.value
                    ? 'bg-white text-surface-900 shadow-sm'
                    : 'text-surface-500 hover:text-surface-700'
                }`}
              >
                {p.label}
              </button>
            ))}
          </div>
        </div>
      </div>

      {scope === 'key' && <KeyScopeView />}

      {scope === 'user' && <UserScopeView />}

      {scope === 'group' && <GroupScopeView />}

      <Card>
        <CardContent className="p-4">
          <LazyChart
            type="bar"
            data={
              (costData?.daily_costs ?? []).map((d) => ({ date: d.date, cost: d.cost })) as Record<string, unknown>[]
            }
            height={250}
            xKey="date"
            series={[
              {
                dataKey: 'cost',
                color: CHART_COLORS.brand,
                radius: [4, 4, 0, 0] as [number, number, number, number],
                maxBarSize: 40,
                name: 'Cost',
              },
            ]}
            yAxisFormatter={(v: number) => `$${v}`}
            referenceLine={{
              y: data.default_daily_limit,
              color: CHART_COLORS.warning,
              label: `Daily Limit ($${data.default_daily_limit})`,
            }}
          />
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Cost by Model</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="h-64">
              <LazyChart
                type="pie"
                data={
                  (costData?.model_breakdown ?? []).map((m) => ({
                    model: m.model,
                    cost: m.cost,
                    calls: m.calls,
                  })) as Record<string, unknown>[]
                }
                height={256}
                series={{
                  dataKey: 'cost',
                  nameKey: 'model',
                  innerRadius: 60,
                  outerRadius: 90,
                  paddingAngle: 4,
                  colors: PALETTE,
                }}
                label={({ name, percent }: Record<string, unknown>) => `${name} ${(Number(percent) * 100).toFixed(0)}%`}
              />
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Model Cost Breakdown</CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            <table className="w-full">
              <thead>
                <tr className="border-b border-surface-200">
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-surface-500">
                    Model
                  </th>
                  <th className="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-surface-500">
                    Calls
                  </th>
                  <th className="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-surface-500">
                    Cost
                  </th>
                  <th className="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-surface-500">
                    %
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-surface-100">
                {(costData?.model_breakdown ?? []).map((row) => {
                  const tc = costData?.total_cost ?? 0
                  const pct = tc > 0 ? (row.cost / tc) * 100 : 0
                  return (
                    <tr key={row.model} className="hover:bg-surface-50">
                      <td className="px-4 py-3 text-sm font-medium text-surface-900">{row.model}</td>
                      <td className="px-4 py-3 text-sm text-right font-mono text-surface-700">
                        {row.calls.toLocaleString()}
                      </td>
                      <td className="px-4 py-3 text-sm text-right font-mono text-surface-700">
                        ${row.cost.toFixed(4)}
                      </td>
                      <td className="px-4 py-3 text-sm text-right font-mono text-surface-600">{pct.toFixed(1)}%</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </CardContent>
        </Card>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Cost by Provider</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="h-64">
              <LazyChart
                type="pie"
                data={
                  (costData?.provider_breakdown ?? []).map((p) => ({ name: p.provider, value: p.cost })) as Record<
                    string,
                    unknown
                  >[]
                }
                height={256}
                series={{
                  dataKey: 'value',
                  nameKey: 'name',
                  innerRadius: 60,
                  outerRadius: 90,
                  paddingAngle: 4,
                  colors: PALETTE,
                }}
                label={({ name, percent }: Record<string, unknown>) => `${name} ${(Number(percent) * 100).toFixed(0)}%`}
              />
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Provider Cost Breakdown</CardTitle>
          </CardHeader>
          <CardContent className="p-0">
            <table className="w-full">
              <thead>
                <tr className="border-b border-surface-200">
                  <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-surface-500">
                    Provider
                  </th>
                  <th className="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-surface-500">
                    Calls
                  </th>
                  <th className="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-surface-500">
                    Cost
                  </th>
                  <th className="px-4 py-3 text-right text-xs font-medium uppercase tracking-wider text-surface-500">
                    %
                  </th>
                </tr>
              </thead>
              <tbody className="divide-y divide-surface-100">
                {(costData?.provider_breakdown ?? []).map((row) => {
                  const tc = costData?.total_cost ?? 0
                  const pct = tc > 0 ? (row.cost / tc) * 100 : 0
                  return (
                    <tr key={row.provider} className="hover:bg-surface-50">
                      <td className="px-4 py-3 text-sm font-medium text-surface-900">{row.provider}</td>
                      <td className="px-4 py-3 text-sm text-right font-mono text-surface-700">
                        {row.calls.toLocaleString()}
                      </td>
                      <td className="px-4 py-3 text-sm text-right font-mono text-surface-700">
                        ${row.cost.toFixed(4)}
                      </td>
                      <td className="px-4 py-3 text-sm text-right font-mono text-surface-600">{pct.toFixed(1)}%</td>
                    </tr>
                  )
                })}
              </tbody>
            </table>
          </CardContent>
        </Card>
      </div>
    </div>
  )

  return (
    <FeatureTabLayout
      title="Budget Management"
      description="Manage API key budgets and monitor spending across keys, users, and groups."
      status={
        <FeatureStatus
          type="toggle"
          enabled={budgetEnabled}
          onToggle={handleToggle}
          disabled={toggling}
          label={budgetEnabled ? 'Enabled' : 'Disabled'}
        />
      }
      stats={
        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-6 gap-4 col-span-full">
          <StatCard
            title="Total Monthly Budget"
            value={`$${totalBudget.toLocaleString()}`}
            description={`$${totalSpent.toLocaleString()} spent`}
            icon={<WalletIcon />}
          />
          <StatCard
            title="Remaining"
            value={`$${totalRemaining.toFixed(2)}`}
            description={`${usagePct}% utilized`}
            icon={<PiggyBankIcon />}
          />
          <StatCard
            title="Over Budget"
            value={overBudgetKeys}
            description={`${atRiskKeys} keys at risk`}
            icon={<AlertTriangleIcon />}
          />
          <StatCard
            title="Total Savings"
            value={`$${(costData?.savings_summary?.total_savings ?? 0).toFixed(2)}`}
            description={`$${(costData?.savings_summary?.routing_savings ?? 0).toFixed(2)} routing · $${(costData?.savings_summary?.cache_savings ?? 0).toFixed(2)} cache`}
            icon={<TrendingUpIcon />}
          />
          <StatCard
            title="Total Requests"
            value={(costData?.total_requests ?? 0).toLocaleString()}
            description="requests in selected period"
            icon={<Activity size={20} />}
          />
          <StatCard
            title="Avg Cost/Request"
            value={`$${(costData?.avg_cost_per_request ?? 0).toFixed(4)}`}
            description="average per request"
            icon={<DollarSign size={20} />}
          />
        </div>
      }
      config={budgetConfig}
      enabled={budgetEnabled}
    />
  )
}

export function BudgetManagementView() {
  return (
    <QueryProvider>
      <BudgetManagementViewContent />
    </QueryProvider>
  )
}
