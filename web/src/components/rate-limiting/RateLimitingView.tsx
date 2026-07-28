import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import {
  api,
  type Group,
  type GroupRateLimit,
  type RateLimitKeyItem,
  type User,
  type UserRateLimit,
} from '../../lib/api'
import { qk } from '../../lib/query'
import { CHART_COLORS } from '../../lib/recharts-theme'
import { useAutoSelectFirst } from '../../lib/useAutoSelectFirst'
import LazyChart from '../charts/LazyChart'
import { FeatureStatus } from '../settings/FeatureStatus'
import { FeatureTabLayout } from '../settings/FeatureTabLayout'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { Activity, AlertTriangle, Gauge, Key, ShieldOff } from '../ui/icons'
import { LevelScopeSelector } from '../ui/LevelScopeSelector'
import { QueryProvider } from '../ui/query-provider'
import { SearchableList } from '../ui/SearchableList'
import { StatCard } from '../ui/StatCard'

const ActivityIcon = () => <Activity size={20} />

const AlertTriangleIcon = () => <AlertTriangle size={20} />

const KeyIcon = () => <Key size={20} />

const GaugeIcon = () => <Gauge size={20} />

const ShieldOffIcon = () => <ShieldOff size={20} />

function fmtRPM(n: number): string {
  if (n >= 100) return Math.round(n).toLocaleString()
  if (n >= 10) return n.toFixed(1)
  if (n >= 1) return n.toFixed(2)
  return n.toFixed(4)
}

function StatusBadge({ status }: { status?: RateLimitKeyItem['status'] }) {
  const config: Record<string, { color: string; bg: string; label: string }> = {
    ok: { color: 'text-success border-success/20', bg: 'bg-success/10', label: 'OK' },
    warning: { color: 'text-warning border-warning/20', bg: 'bg-warning/10', label: 'Warning' },
    critical: { color: 'text-error border-error/20', bg: 'bg-error/10', label: 'Critical' },
  }
  const s = config[status ?? ''] || {
    color: 'text-surface-400 border-surface-200/20',
    bg: 'bg-surface-100/10',
    label: '—',
  }
  return (
    <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${s.color} ${s.bg}`}>
      {s.label}
    </span>
  )
}

function UserScopeView() {
  const [search, setSearch] = useState('')
  const [selectedUser, setSelectedUser] = useState<User | null>(null)
  const [userRateLimit, setUserRateLimit] = useState<UserRateLimit | null>(null)
  const [rpmInput, setRpmInput] = useState('')
  const [retryAfterInput, setRetryAfterInput] = useState('')
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
      const rl = await api.rateLimits.getUserRateLimit(user.id)
      setUserRateLimit(rl)
      setRpmInput(String(rl.rpm_limit))
      setRetryAfterInput(String(rl.retry_after))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load rate limit')
    }
  }

  async function saveRpm() {
    if (!selectedUser) return
    setSaving(true)
    setError(null)
    try {
      await api.rateLimits.setUserRateLimit(selectedUser.id, Number(rpmInput), Number(retryAfterInput))
      const rl = await api.rateLimits.getUserRateLimit(selectedUser.id)
      setUserRateLimit(rl)
      setRpmInput(String(rl.rpm_limit))
      setRetryAfterInput(String(rl.retry_after))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save rate limit')
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

      {selectedUser && userRateLimit && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">User Rate Limit — {selectedUser.name}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-4">
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">RPM Limit</p>
                <p className="text-xl font-bold font-mono text-surface-900">{userRateLimit.rpm_limit ?? '∞'}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Current RPM</p>
                <p className="text-xl font-bold font-mono text-surface-900">{fmtRPM(userRateLimit.current_rpm ?? 0)}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Blocked (24h)</p>
                <p className="text-xl font-bold font-mono text-surface-900">{userRateLimit.blocked_24h ?? 0}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Status</p>
                <StatusBadge status={userRateLimit.status} />
              </div>
            </div>
            <div className="flex items-end gap-3">
              <div className="flex-1">
                <label className="block text-xs font-medium text-surface-500 mb-1">Set RPM Limit (0 = inherit)</label>
                <input
                  type="number"
                  min={0}
                  value={rpmInput}
                  onChange={(e) => setRpmInput(e.target.value)}
                  className="w-full rounded-lg border border-surface-200 bg-white px-3 py-2 text-sm font-mono text-surface-900 focus:outline-none focus:ring-2 focus:ring-brand-500"
                />
              </div>
              <div className="flex-1">
                <label className="block text-xs font-medium text-surface-500 mb-1">
                  Retry-After (s) (0 = default 60s)
                </label>
                <input
                  type="number"
                  min={0}
                  value={retryAfterInput}
                  onChange={(e) => setRetryAfterInput(e.target.value)}
                  className="w-full rounded-lg border border-surface-200 bg-white px-3 py-2 text-sm font-mono text-surface-900 focus:outline-none focus:ring-2 focus:ring-brand-500"
                />
              </div>
              <button
                type="button"
                onClick={saveRpm}
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
  const [groupRateLimit, setGroupRateLimit] = useState<GroupRateLimit | null>(null)
  const [rpmInput, setRpmInput] = useState('')
  const [retryAfterInput, setRetryAfterInput] = useState('')
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
      const rl = await api.rateLimits.getGroupRateLimit(group.id)
      setGroupRateLimit(rl)
      setRpmInput(String(rl.rpm_limit))
      setRetryAfterInput(String(rl.retry_after))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load rate limit')
    }
  }

  async function saveRpm() {
    if (!selectedGroup) return
    setSaving(true)
    setError(null)
    try {
      await api.rateLimits.setGroupRateLimit(selectedGroup.id, Number(rpmInput), Number(retryAfterInput))
      const rl = await api.rateLimits.getGroupRateLimit(selectedGroup.id)
      setGroupRateLimit(rl)
      setRpmInput(String(rl.rpm_limit))
      setRetryAfterInput(String(rl.retry_after))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save rate limit')
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

      {selectedGroup && groupRateLimit && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Group Rate Limit — {selectedGroup.name}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-4">
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">RPM Limit</p>
                <p className="text-xl font-bold font-mono text-surface-900">{groupRateLimit.rpm_limit ?? '∞'}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Current RPM</p>
                <p className="text-xl font-bold font-mono text-surface-900">
                  {fmtRPM(groupRateLimit.current_rpm ?? 0)}
                </p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Blocked (24h)</p>
                <p className="text-xl font-bold font-mono text-surface-900">{groupRateLimit.blocked_24h ?? 0}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Status</p>
                <StatusBadge status={groupRateLimit.status} />
              </div>
            </div>
            <div className="flex items-end gap-3">
              <div className="flex-1">
                <label className="block text-xs font-medium text-surface-500 mb-1">Set RPM Limit (0 = inherit)</label>
                <input
                  type="number"
                  min={0}
                  value={rpmInput}
                  onChange={(e) => setRpmInput(e.target.value)}
                  className="w-full rounded-lg border border-surface-200 bg-white px-3 py-2 text-sm font-mono text-surface-900 focus:outline-none focus:ring-2 focus:ring-brand-500"
                />
              </div>
              <div className="flex-1">
                <label className="block text-xs font-medium text-surface-500 mb-1">
                  Retry-After (s) (0 = default 60s)
                </label>
                <input
                  type="number"
                  min={0}
                  value={retryAfterInput}
                  onChange={(e) => setRetryAfterInput(e.target.value)}
                  className="w-full rounded-lg border border-surface-200 bg-white px-3 py-2 text-sm font-mono text-surface-900 focus:outline-none focus:ring-2 focus:ring-brand-500"
                />
              </div>
              <button
                type="button"
                onClick={saveRpm}
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

function KeyScopeView({ keys }: { keys: RateLimitKeyItem[] }) {
  const [search, setSearch] = useState('')
  const [selectedKey, setSelectedKey] = useState<RateLimitKeyItem | null>(null)
  const [keyRateLimit, setKeyRateLimit] = useState<RateLimitKeyItem | null>(null)
  const [rpmInput, setRpmInput] = useState('')
  const [retryAfterInput, setRetryAfterInput] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function selectKey(k: RateLimitKeyItem) {
    setSelectedKey(k)
    setError(null)
    try {
      const rl = await api.rateLimits.getKeyRateLimit(k.id)
      setKeyRateLimit(rl)
      setRpmInput(String(rl.rpm_limit))
      setRetryAfterInput(String(rl.retry_after))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load rate limit')
    }
  }

  async function saveRpm() {
    if (!selectedKey) return
    setSaving(true)
    setError(null)
    try {
      await api.rateLimits.setKeyRateLimit(selectedKey.id, Number(rpmInput), Number(retryAfterInput))
      const rl = await api.rateLimits.getKeyRateLimit(selectedKey.id)
      setKeyRateLimit(rl)
      setRpmInput(String(rl.rpm_limit))
      setRetryAfterInput(String(rl.retry_after))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to save rate limit')
    } finally {
      setSaving(false)
    }
  }

  const filtered = keys.filter(
    (k) =>
      k.key_name.toLowerCase().includes(search.toLowerCase()) ||
      k.key_prefix.toLowerCase().includes(search.toLowerCase()),
  )

  useAutoSelectFirst(filtered, selectedKey, false, selectKey)

  return (
    <div className="space-y-4">
      <SearchableList
        items={filtered}
        isLoading={false}
        loadingLabel="Loading keys..."
        search={search}
        onSearchChange={setSearch}
        searchPlaceholder="Search API keys by name or prefix..."
        error={error}
        isSelected={(k) => selectedKey?.id === k.id}
        onSelect={selectKey}
        getKey={(k) => k.id}
        emptyMessage="No API keys found"
        renderItem={(k) => (
          <>
            <span className="font-medium text-surface-900">{k.key_name}</span>
            <span className="ml-2 text-xs font-mono text-surface-400">{k.key_prefix}...</span>
          </>
        )}
      />

      {selectedKey && keyRateLimit && (
        <Card>
          <CardHeader>
            <CardTitle className="text-base">API Key Rate Limit — {selectedKey.key_name}</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-2 sm:grid-cols-4 gap-4 mb-4">
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">RPM Limit</p>
                <p className="text-xl font-bold font-mono text-surface-900">{keyRateLimit.rpm_limit ?? '∞'}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Current RPM</p>
                <p className="text-xl font-bold font-mono text-surface-900">{fmtRPM(keyRateLimit.current_rpm ?? 0)}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Blocked (24h)</p>
                <p className="text-xl font-bold font-mono text-surface-900">{keyRateLimit.blocked_24h ?? 0}</p>
              </div>
              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Status</p>
                <StatusBadge status={keyRateLimit.status} />
              </div>
            </div>
            <div className="flex items-end gap-3">
              <div className="flex-1">
                <label className="block text-xs font-medium text-surface-500 mb-1">Set RPM Limit (0 = inherit)</label>
                <input
                  type="number"
                  min={0}
                  value={rpmInput}
                  onChange={(e) => setRpmInput(e.target.value)}
                  className="w-full rounded-lg border border-surface-200 bg-white px-3 py-2 text-sm font-mono text-surface-900 focus:outline-none focus:ring-2 focus:ring-brand-500"
                />
              </div>
              <div className="flex-1">
                <label className="block text-xs font-medium text-surface-500 mb-1">
                  Retry-After (s) (0 = default 60s)
                </label>
                <input
                  type="number"
                  min={0}
                  value={retryAfterInput}
                  onChange={(e) => setRetryAfterInput(e.target.value)}
                  className="w-full rounded-lg border border-surface-200 bg-white px-3 py-2 text-sm font-mono text-surface-900 focus:outline-none focus:ring-2 focus:ring-brand-500"
                />
              </div>
              <button
                type="button"
                onClick={saveRpm}
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

function RateLimitingViewContent() {
  const [scope, setScope] = useState<'key' | 'user' | 'group'>('key')
  const [autoRefresh, setAutoRefresh] = useState(true)
  const [toggling, setToggling] = useState(false)
  const queryClient = useQueryClient()

  const {
    data: summary,
    isLoading,
    error,
    refetch,
  } = useQuery({
    queryKey: qk.rateLimiting,
    queryFn: () => api.rateLimits.getRateLimitSummary(),
    refetchInterval: autoRefresh ? 30000 : false,
  })

  const handleToggle = useCallback(async () => {
    if (!summary || toggling) return
    setToggling(true)
    try {
      await api.features.toggleFeature('rate_limit', !summary.enabled)
      queryClient.invalidateQueries({ queryKey: qk.rateLimiting })
    } catch {
      // Error fetching is handled by the query
    } finally {
      setToggling(false)
    }
  }, [summary, toggling, queryClient])

  if (isLoading && !summary) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="flex flex-col items-center gap-3">
          <div className="h-8 w-8 animate-spin rounded-full border-4 border-surface-200 border-t-brand-600" />
          <p className="text-sm text-surface-500">Loading rate limit data...</p>
        </div>
      </div>
    )
  }

  if (error && !summary) {
    return (
      <div className="flex items-center justify-center h-64">
        <div className="flex flex-col items-center gap-3 text-center">
          <AlertTriangleIcon />
          <p className="text-sm text-surface-600">
            {error instanceof Error ? error.message : 'Unable to load rate limit data'}
          </p>
          <button
            type="button"
            onClick={() => refetch()}
            className="text-sm text-brand-600 hover:text-brand-700 font-medium"
          >
            Try again
          </button>
        </div>
      </div>
    )
  }

  if (!summary) return null

  const rateLimitedPct =
    summary.total_requests_24h > 0 ? ((summary.rate_limited_24h / summary.total_requests_24h) * 100).toFixed(1) : '0.0'

  return (
    <FeatureTabLayout
      title="Rate Limiting"
      status={
        <FeatureStatus
          type="toggle"
          enabled={summary.enabled}
          onToggle={handleToggle}
          label={summary.enabled ? 'Enabled' : 'Disabled'}
        />
      }
      enabled={summary.enabled}
      stats={
        <>
          <StatCard
            title="Total Requests (24h)"
            value={summary.total_requests_24h.toLocaleString()}
            icon={<ActivityIcon />}
          />
          <StatCard
            title="Rate Limited"
            value={summary.rate_limited_24h.toLocaleString()}
            description={`${rateLimitedPct}% of total requests`}
            icon={<ShieldOffIcon />}
          />
          <StatCard
            title="Active API Keys"
            value={summary.active_keys}
            description="with requests in last 24h"
            icon={<KeyIcon />}
          />
          <StatCard
            title="Average RPM (24h)"
            value={
              summary.limit_rpm > 0 ? `${fmtRPM(summary.avg_rpm)} / ${summary.limit_rpm}` : fmtRPM(summary.avg_rpm)
            }
            description={summary.limit_rpm > 0 ? 'avg across all keys / global limit' : 'average across all keys'}
            icon={<GaugeIcon />}
          />
        </>
      }
      table={
        <>
          {/* ── Disabled banner ── */}
          {!summary.enabled && (
            <div className="rounded-lg border border-warning/20 bg-warning/5 px-4 py-3">
              <div className="flex items-start gap-3">
                <AlertTriangle size={18} className="text-warning shrink-0 mt-0.5" />
                <div>
                  <p className="text-sm font-medium text-warning">Rate limiting is disabled</p>
                  <p className="text-xs text-surface-500 mt-0.5">
                    All requests proceed without rate checks. Toggle the switch above to enable.
                  </p>
                </div>
              </div>
            </div>
          )}

          {/* ── Scope Tabs ── */}
          <LevelScopeSelector value={scope} onChange={setScope} />

          {scope === 'key' && <KeyScopeView keys={summary.keys} />}

          {scope === 'user' && <UserScopeView />}

          {scope === 'group' && <GroupScopeView />}

          {/* ── Request Rate Chart (below scope picker) ── */}
          <div className="flex items-center justify-between">
            <h2 className="text-lg font-semibold text-surface-900">Request Rate (24h)</h2>
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
            </div>
          </div>

          <Card>
            <CardContent className="p-4">
              <LazyChart
                type="bar"
                data={summary.chart as unknown as Record<string, unknown>[]}
                xKey="time"
                series={[{ dataKey: 'requests', color: CHART_COLORS.brand, name: 'Requests' }]}
                {...(summary.limit_rpm > 0
                  ? {
                      referenceLine: {
                        y: summary.limit_rpm,
                        color: CHART_COLORS.error,
                        label: `Limit (${summary.limit_rpm} RPM)`,
                        strokeDasharray: '6 3' as const,
                      },
                    }
                  : {})}
                height={250}
              />
            </CardContent>
          </Card>
        </>
      }
    />
  )
}

export function RateLimitingView() {
  return (
    <QueryProvider>
      <RateLimitingViewContent />
    </QueryProvider>
  )
}
