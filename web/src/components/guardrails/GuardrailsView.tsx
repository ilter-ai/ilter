import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState } from 'react'
import { toast } from 'sonner'
import {
  api,
  type Group,
  type GuardrailEventItem,
  type GuardrailRule,
  type GuardrailsTestResult,
  type User,
} from '../../lib/api'
import { qk } from '../../lib/query'
import { CHART_COLORS, PALETTE } from '../../lib/recharts-theme'
import { useApiMutation } from '../../lib/useApiMutation'
import LazyChart from '../charts/LazyChart'
import { FeatureStatus } from '../settings/FeatureStatus'
import { ActionBadge, ScopeBadge, TypeBadge } from '../ui/badge'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { type Column, DataTable } from '../ui/DataTable'
import { EmptyState } from '../ui/empty-state'
import { AlertTriangle, Check, Download, Search, Shield, XCircle } from '../ui/icons'
import { QueryProvider } from '../ui/query-provider'
import { StatCard } from '../ui/StatCard'
import { Skeleton } from '../ui/skeleton'
import { Switch } from '../ui/switch'

function GuardrailsViewContent() {
  const queryClient = useQueryClient()
  const [search, setSearch] = useState('')
  const [typeFilter, setTypeFilter] = useState('')
  const [actionFilter, setActionFilter] = useState('')
  const [testContent, setTestContent] = useState('')
  const [testResult, setTestResult] = useState<GuardrailsTestResult | null>(null)
  const [testLoading, setTestLoading] = useState(false)

  const [scopeFilter, setScopeFilter] = useState<string>('all')
  const [showCreateForm, setShowCreateForm] = useState(false)
  const [nameInput, setNameInput] = useState('')
  const [typeInput, setTypeInput] = useState('')
  const [patternsInput, setPatternsInput] = useState('')
  const [targetType, setTargetType] = useState<string>('global')
  const [targetId, setTargetId] = useState<number | null>(null)
  const [userSearch, setUserSearch] = useState('')
  const [groupSearch, setGroupSearch] = useState('')
  const [showUserDropdown, setShowUserDropdown] = useState(false)
  const [showGroupDropdown, setShowGroupDropdown] = useState(false)
  const [creating, setCreating] = useState(false)
  const [deleting, setDeleting] = useState<string | null>(null)
  const [toggling, setToggling] = useState<string | null>(null)

  const {
    data: rules,
    isLoading: loadingRules,
    error: rulesError,
  } = useQuery({
    queryKey: qk.guardrails,
    queryFn: () => api.guardrails.getGuardrailRules(),
  })
  const safeRules = rules ?? []

  const { data: features } = useQuery({
    queryKey: qk.features,
    queryFn: () => api.features.getFeatures(),
  })
  const safeFeatures = features ?? []
  const guardrailsFeature = safeFeatures.find((f) => f.feature_key === 'guardrails')
  const guardrailsEnabled = guardrailsFeature ? guardrailsFeature.enabled : false

  const toggleGuardrailsFeature = useMutation({
    mutationFn: (enabled: boolean) => api.features.toggleFeature('guardrails', enabled),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: qk.features })
    },
  })

  const { data: guardrailsStats } = useQuery({
    queryKey: qk.guardrailsStats,
    queryFn: () => api.guardrails.getGuardrailsStats().catch(() => null),
  })

  const { data: summary } = useQuery({
    queryKey: ['guardrails', 'summary'],
    queryFn: () => api.guardrails.getGuardrailSummary('7d').catch(() => null),
  })

  const { data: blockedTotal } = useQuery({
    queryKey: ['guardrails', 'blocked-total'],
    queryFn: () =>
      api.guardrails
        .getGuardrailViolations({ action: 'blocked', limit: 1 })
        .then((r) => r.total)
        .catch(() => 0),
  })

  const { data: violationsData } = useQuery({
    queryKey: ['guardrails', 'violations', typeFilter, actionFilter],
    queryFn: () =>
      api.guardrails.getGuardrailViolations({
        type: typeFilter || undefined,
        action: actionFilter || undefined,
        limit: 50,
      }),
  })

  const violations: GuardrailEventItem[] = violationsData?.items ?? []

  const { data: users = [] } = useQuery({
    queryKey: qk.users,
    queryFn: () => api.users.getUsers().catch(() => [] as User[]),
  })

  const { data: groups = [] } = useQuery({
    queryKey: qk.groups,
    queryFn: () => api.groups.getGroups().catch(() => [] as Group[]),
  })

  const toggleRule = useApiMutation(
    (args: { id: string; enabled: boolean }) => api.guardrails.updateGuardrailRule(args.id, { enabled: args.enabled }),
    { invalidate: [qk.guardrails] },
  )

  const deleteRule = useApiMutation((id: string) => api.guardrails.deleteGuardrailRule(id), {
    invalidate: [qk.guardrails],
  })

  const createRule = useApiMutation(
    (args: { name: string; patterns: string[]; target_type?: string; target_id?: number }) =>
      api.guardrails.createGuardrailRule(args),
    { invalidate: [qk.guardrails] },
  )

  const filteredByScope = useMemo(() => {
    if (scopeFilter === 'all') return safeRules
    return safeRules.filter((r) => (r.target_type || 'global') === scopeFilter)
  }, [safeRules, scopeFilter])

  const handleToggleRule = async (rule: GuardrailRule) => {
    setToggling(rule.id)
    try {
      await toggleRule.mutateAsync({ id: rule.id, enabled: !rule.enabled })
    } catch {
      toast.error('Failed to toggle rule')
    } finally {
      setToggling(null)
    }
  }

  const handleDeleteRule = async (id: string) => {
    setDeleting(id)
    try {
      await deleteRule.mutateAsync(id)
    } catch {
      toast.error('Failed to delete rule')
    } finally {
      setDeleting(null)
    }
  }

  const handleCreateRule = async () => {
    if (!nameInput.trim() || !typeInput.trim()) return
    setCreating(true)
    try {
      await createRule.mutateAsync({
        name: nameInput.trim(),
        patterns: patternsInput
          .split('\n')
          .filter(Boolean)
          .map((p) => p.trim()),
        target_type: targetType === 'global' ? undefined : targetType,
        target_id: targetType === 'global' ? undefined : (targetId ?? undefined),
      })
      setNameInput('')
      setTypeInput('')
      setPatternsInput('')
      setTargetType('global')
      setTargetId(null)
      setShowCreateForm(false)
      toast.success('Rule created')
    } catch {
      toast.error('Failed to create rule')
    } finally {
      setCreating(false)
    }
  }

  const runGuardrailTest = async () => {
    if (!testContent.trim()) return
    setTestLoading(true)
    setTestResult(null)
    try {
      const result = await api.guardrails.testGuardrails(testContent)
      setTestResult(result)
    } catch {
      toast.error('Guardrails test failed')
    } finally {
      setTestLoading(false)
    }
  }

  const filtered = useMemo(() => {
    if (!search) return violations
    const q = search.toLowerCase()
    return violations.filter(
      (v) => v.guardrail_type.toLowerCase().includes(q) || v.action_taken.toLowerCase().includes(q),
    )
  }, [search, violations])

  const ruleColumns: Column<GuardrailRule>[] = [
    {
      key: 'name',
      header: 'Name',
      render: (rule) => <span className="text-sm font-medium text-surface-900">{rule.name}</span>,
    },
    { key: 'type', header: 'Type', render: (rule) => <TypeBadge type={rule.type} /> },
    { key: 'scope', header: 'Scope', render: (rule) => <ScopeBadge targetType={rule.target_type} /> },
    {
      key: 'enabled',
      header: 'Status',
      render: (rule) => (
        <Switch
          size="sm"
          checked={rule.enabled}
          onCheckedChange={() => handleToggleRule(rule)}
          disabled={toggling === rule.id}
        />
      ),
    },
    {
      key: 'actions',
      header: 'Actions',
      className: 'text-right',
      headerClassName: 'text-right',
      render: (rule) => (
        <Button
          variant="ghost"
          size="sm"
          disabled={deleting === rule.id}
          onClick={() => handleDeleteRule(rule.id)}
          className="text-error hover:text-error"
        >
          {deleting === rule.id ? '...' : 'Delete'}
        </Button>
      ),
    },
  ]

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-surface-900">Guardrails</h2>
        <FeatureStatus
          type="toggle"
          enabled={guardrailsEnabled}
          onToggle={() => toggleGuardrailsFeature.mutate(!guardrailsEnabled)}
          disabled={toggleGuardrailsFeature.isPending}
          label={toggleGuardrailsFeature.isPending ? 'Updating...' : guardrailsEnabled ? 'Enabled' : 'Disabled'}
        />
      </div>
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        <StatCard
          title="Total Violations"
          value={summary ? String(summary.total_events) : '...'}
          description="Past 7 days"
          icon={<AlertTriangle />}
        />
        <StatCard
          title="Active Rules"
          value={loadingRules ? '...' : String(safeRules.filter((r) => r.enabled).length)}
          description={
            loadingRules
              ? 'Loading...'
              : `${safeRules.length} total, ${safeRules.filter((r) => r.enabled).length} enabled`
          }
          icon={<Shield />}
        />
        <StatCard
          title="Blocked"
          value={String(blockedTotal ?? '...')}
          description="Action: Blocked"
          icon={<XCircle />}
        />
        <StatCard
          title="Rule Sets"
          value={guardrailsStats ? String(guardrailsStats.rule_count) : '...'}
          description={guardrailsStats ? `${guardrailsStats.rule_sets?.join(', ') || 'N/A'}` : 'Loading...'}
          icon={<Check />}
        />
      </div>

      <Card>
        <CardHeader>
          <div className="flex items-center justify-between gap-4">
            <CardTitle className="text-base">Guardrail Rules</CardTitle>
            <div className="flex items-center gap-3">
              <select
                value={scopeFilter}
                onChange={(e) => setScopeFilter(e.target.value)}
                className="rounded-lg border border-surface-300 bg-white px-3 py-1.5 text-sm text-surface-700 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              >
                <option value="all">All Scopes</option>
                <option value="global">Global</option>
                <option value="user">User</option>
                <option value="group">Group</option>
              </select>
              <Button
                size="sm"
                onClick={() => {
                  setShowCreateForm((v) => !v)
                  if (!showCreateForm) {
                    setTargetType('global')
                    setTargetId(null)
                    setNameInput('')
                    setTypeInput('')
                    setPatternsInput('')
                  }
                }}
              >
                {showCreateForm ? 'Cancel' : '+ Add Rule'}
              </Button>
            </div>
          </div>
        </CardHeader>
        <CardContent>
          {showCreateForm && (
            <div className="mb-6 rounded-lg border border-surface-200 bg-surface-50 p-4 space-y-4">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div>
                  <label className="mb-1 block text-xs font-medium text-surface-600">Name</label>
                  <input
                    value={nameInput}
                    onChange={(e) => setNameInput(e.target.value)}
                    placeholder="e.g. PII Detection"
                    className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                  />
                </div>
                <div>
                  <label className="mb-1 block text-xs font-medium text-surface-600">Type / Rule Set</label>
                  <input
                    value={typeInput}
                    onChange={(e) => setTypeInput(e.target.value)}
                    placeholder="e.g. content_policy"
                    className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                  />
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs font-medium text-surface-600">Patterns (one per line)</label>
                <textarea
                  value={patternsInput}
                  onChange={(e) => setPatternsInput(e.target.value)}
                  placeholder="Enter regex patterns..."
                  rows={3}
                  className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 resize-y"
                />
              </div>
              <div>
                <label className="mb-2 block text-xs font-medium text-surface-600">Scope / Target</label>
                <div className="flex items-center gap-4">
                  {['global', 'user', 'group'].map((t) => (
                    <label key={t} className="flex items-center gap-1.5 cursor-pointer">
                      <input
                        type="radio"
                        name="target_type"
                        value={t}
                        checked={targetType === t}
                        onChange={() => {
                          setTargetType(t)
                          setTargetId(null)
                        }}
                        className="text-brand-600 focus:ring-brand-500"
                      />
                      <span className="text-sm text-surface-700 capitalize">{t}</span>
                    </label>
                  ))}
                </div>
              </div>
              {targetType === 'user' && (
                <div className="relative">
                  <label className="mb-1 block text-xs font-medium text-surface-600">Select User</label>
                  <input
                    value={userSearch}
                    onChange={(e) => {
                      setUserSearch(e.target.value)
                      setShowUserDropdown(true)
                    }}
                    onFocus={() => setShowUserDropdown(true)}
                    onBlur={() => setTimeout(() => setShowUserDropdown(false), 200)}
                    placeholder="Search users..."
                    className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                  />
                  {showUserDropdown && (
                    <div className="absolute z-10 mt-1 max-h-48 w-full overflow-y-auto rounded-lg border border-surface-200 bg-white shadow-lg">
                      {users
                        .filter(
                          (u) =>
                            !userSearch ||
                            u.name.toLowerCase().includes(userSearch.toLowerCase()) ||
                            u.email.toLowerCase().includes(userSearch.toLowerCase()),
                        )
                        .map((u) => (
                          <button
                            key={u.id}
                            type="button"
                            onMouseDown={() => {
                              setTargetId(u.id)
                              setUserSearch(u.name)
                              setShowUserDropdown(false)
                            }}
                            className={`w-full px-3 py-2 text-left text-sm hover:bg-surface-100 ${targetId === u.id ? 'bg-brand-50 text-brand-700' : 'text-surface-700'}`}
                          >
                            {u.name} <span className="text-surface-400">({u.email})</span>
                          </button>
                        ))}
                      {users.length === 0 && (
                        <div className="px-3 py-2 text-sm text-surface-400">No users available</div>
                      )}
                    </div>
                  )}
                </div>
              )}
              {targetType === 'group' && (
                <div className="relative">
                  <label className="mb-1 block text-xs font-medium text-surface-600">Select Group</label>
                  <input
                    value={groupSearch}
                    onChange={(e) => {
                      setGroupSearch(e.target.value)
                      setShowGroupDropdown(true)
                    }}
                    onFocus={() => setShowGroupDropdown(true)}
                    onBlur={() => setTimeout(() => setShowGroupDropdown(false), 200)}
                    placeholder="Search groups..."
                    className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                  />
                  {showGroupDropdown && (
                    <div className="absolute z-10 mt-1 max-h-48 w-full overflow-y-auto rounded-lg border border-surface-200 bg-white shadow-lg">
                      {groups
                        .filter((g) => !groupSearch || g.name.toLowerCase().includes(groupSearch.toLowerCase()))
                        .map((g) => (
                          <button
                            key={g.id}
                            type="button"
                            onMouseDown={() => {
                              setTargetId(g.id)
                              setGroupSearch(g.name)
                              setShowGroupDropdown(false)
                            }}
                            className={`w-full px-3 py-2 text-left text-sm hover:bg-surface-100 ${targetId === g.id ? 'bg-brand-50 text-brand-700' : 'text-surface-700'}`}
                          >
                            {g.name} <span className="text-surface-400">({g.description || 'No description'})</span>
                          </button>
                        ))}
                      {groups.length === 0 && (
                        <div className="px-3 py-2 text-sm text-surface-400">No groups available</div>
                      )}
                    </div>
                  )}
                </div>
              )}
              {targetType !== 'global' && targetId === null && (
                <p className="text-xs text-warning">Please select a {targetType}</p>
              )}
              <div className="flex justify-end gap-2 pt-1">
                <Button variant="outline" size="sm" onClick={() => setShowCreateForm(false)}>
                  Cancel
                </Button>
                <Button
                  size="sm"
                  onClick={handleCreateRule}
                  disabled={
                    creating || !nameInput.trim() || !typeInput.trim() || (targetType !== 'global' && targetId === null)
                  }
                >
                  {creating ? 'Creating...' : 'Create Rule'}
                </Button>
              </div>
            </div>
          )}

          {loadingRules ? (
            <div className="space-y-2">
              {[1, 2, 3].map((i) => (
                <Skeleton key={i} className="h-10 w-full" />
              ))}
            </div>
          ) : rulesError ? (
            <EmptyState
              title={rulesError instanceof Error ? rulesError.message : 'Failed to load guardrails'}
              icon={<AlertTriangle />}
            />
          ) : filteredByScope.length === 0 ? (
            <EmptyState
              title="No guardrail rules"
              description={
                scopeFilter !== 'all'
                  ? `No rules for scope "${scopeFilter}"`
                  : 'Create your first guardrail rule to get started'
              }
              action={
                scopeFilter === 'all' ? { label: '+ Add Rule', onClick: () => setShowCreateForm(true) } : undefined
              }
            />
          ) : (
            <DataTable columns={ruleColumns} data={filteredByScope} keyExtractor={(rule) => rule.id} />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Test Guardrails</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            <textarea
              value={testContent}
              onChange={(e) => setTestContent(e.target.value)}
              placeholder="Enter content to test against guardrail rules..."
              rows={3}
              className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 resize-y"
            />
            <div className="flex items-center gap-3">
              <Button onClick={runGuardrailTest} disabled={testLoading || !testContent.trim()}>
                {testLoading ? 'Testing...' : 'Test Content'}
              </Button>
              {testResult && (
                <div className="flex items-center gap-3 text-sm">
                  <span
                    className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium ${testResult.blocked ? 'bg-error/10 text-error' : testResult.warned ? 'bg-warning/10 text-warning' : 'bg-success/10 text-success'}`}
                  >
                    <span
                      className={`inline-block h-2 w-2 rounded-full ${testResult.blocked ? 'bg-error' : testResult.warned ? 'bg-warning' : 'bg-success'}`}
                    />
                    {testResult.blocked ? 'Blocked' : testResult.warned ? 'Warned' : 'Passed'}
                  </span>
                  {testResult.rule_id && (
                    <span className="text-surface-500">
                      Rule: <span className="font-mono text-surface-700">{testResult.rule_id}</span>
                    </span>
                  )}
                  {testResult.rule_set && (
                    <span className="text-surface-500">
                      Set: <span className="font-mono text-surface-700">{testResult.rule_set}</span>
                    </span>
                  )}
                  {testResult.severity && (
                    <span className="text-surface-500">
                      Severity: <span className="font-mono text-surface-700">{testResult.severity}</span>
                    </span>
                  )}
                </div>
              )}
            </div>
          </div>
        </CardContent>
      </Card>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <Card>
          <CardHeader>
            <CardTitle className="text-base">Violation Types</CardTitle>
          </CardHeader>
          <CardContent>
            <LazyChart
              type="pie"
              data={(summary?.by_type ?? []) as unknown as Record<string, unknown>[]}
              height={256}
              series={{ dataKey: 'count', nameKey: 'guardrail_type' }}
              label={({ percent }: { percent?: number }) => `${(percent ?? 0 * 100).toFixed(0)}%`}
            />
            <div className="flex flex-wrap justify-center gap-3 mt-2">
              {(summary?.by_type ?? []).map((v, i) => (
                <div key={v.guardrail_type} className="flex items-center gap-1.5 text-xs text-surface-600">
                  <span
                    className="inline-block h-2.5 w-2.5 rounded-full"
                    style={{ backgroundColor: PALETTE[i % PALETTE.length] }}
                  />
                  {v.guardrail_type}
                </div>
              ))}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle className="text-base">Violations Over Time</CardTitle>
          </CardHeader>
          <CardContent>
            <LazyChart
              type="line"
              data={(summary?.recent_trend ?? []) as unknown as Record<string, unknown>[]}
              xKey="date"
              height={256}
              series={[{ dataKey: 'count', color: CHART_COLORS.error, strokeWidth: 2, name: 'Violations' }]}
            />
          </CardContent>
        </Card>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <div className="relative flex-1 min-w-[200px] max-w-sm">
          <Search size={16} className="absolute left-3 top-1/2 -translate-y-1/2 text-surface-400" />
          <input
            type="text"
            placeholder="Search by type or action..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            className="w-full rounded-lg border border-surface-300 bg-white py-2 pl-10 pr-3 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
          />
        </div>
        <select
          value={typeFilter}
          onChange={(e) => setTypeFilter(e.target.value)}
          className="rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-700 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
        >
          <option value="">All Types</option>
          {(summary?.by_type ?? []).map((v) => (
            <option key={v.guardrail_type} value={v.guardrail_type}>
              {v.guardrail_type}
            </option>
          ))}
        </select>
        <select
          value={actionFilter}
          onChange={(e) => setActionFilter(e.target.value)}
          className="rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-700 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
        >
          <option value="">All Actions</option>
          <option value="blocked">Blocked</option>
          <option value="warned">Warned</option>
          <option value="flagged">Flagged</option>
          <option value="masked">Masked</option>
          <option value="allowed">Allowed</option>
          <option value="throttled">Throttled</option>
          <option value="alerted">Alerted</option>
        </select>
        <a href="/api/guardrails/export?format=csv" rel="noreferrer">
          <Button variant="outline" size="sm">
            <Download size={14} />
            Export CSV
          </Button>
        </a>
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Violation Log</CardTitle>
        </CardHeader>
        <CardContent className="p-0">
          <table className="w-full">
            <thead>
              <tr className="border-b border-surface-200">
                {['Timestamp', 'Type', 'Action', 'Model', 'Provider'].map((h) => (
                  <th
                    key={h}
                    className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wider text-surface-500"
                  >
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-surface-100">
              {filtered.slice(0, 50).map((v) => (
                <tr key={v.id} className="hover:bg-surface-50">
                  <td className="px-4 py-3 text-sm font-mono text-xs text-surface-500">
                    {new Date(v.timestamp).toLocaleString()}
                  </td>
                  <td className="px-4 py-3">
                    <TypeBadge type={v.guardrail_type} />
                  </td>
                  <td className="px-4 py-3">
                    <ActionBadge action={v.action_taken} />
                  </td>
                  <td className="px-4 py-3 text-sm text-surface-600">{v.model || '-'}</td>
                  <td className="px-4 py-3 text-sm text-surface-600">{v.provider || '-'}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </CardContent>
      </Card>
    </div>
  )
}

export function GuardrailsView() {
  return (
    <QueryProvider>
      <GuardrailsViewContent />
    </QueryProvider>
  )
}
