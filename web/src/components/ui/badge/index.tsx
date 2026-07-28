import { cn } from '../../../lib/utils'

/**
 * Generic Badge component with variant support
 */
export interface BadgeProps {
  variant?: 'default' | 'success' | 'error' | 'warning' | 'info' | 'primary' | 'secondary'
  children: React.ReactNode
  className?: string
}

export function Badge({ variant = 'default', children, className }: BadgeProps) {
  const variants: Record<string, string> = {
    default:
      'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-surface-100 text-surface-600 border-surface-200',
    success:
      'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-success/10 text-success border-success/20',
    error:
      'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-error/10 text-error border-error/20',
    warning:
      'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-warning/10 text-warning border-warning/20',
    info: 'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-info/10 text-info border-info/20',
    primary:
      'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-brand-50 text-brand-700 border-brand-200',
    secondary:
      'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-purple-100 text-purple-700 border-purple-200',
  }

  return <span className={cn(variants[variant], className)}>{children}</span>
}

/**
 * Type Badge - Maps type strings to colors
 * Used in: McpAccessView, GuardrailsView
 */
const TYPE_COLORS: Record<string, string> = {
  key: 'inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-wider bg-blue-100 text-blue-700 border border-blue-200',
  user: 'inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-wider bg-green-100 text-green-700 border border-green-200',
  group:
    'inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-wider bg-purple-100 text-purple-700 border border-purple-200',
  prompt_injection:
    'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-violet-100 text-violet-700 border-violet-200',
  toxic_content:
    'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-error/10 text-error border-error/20',
  topic_block:
    'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-cyan-100 text-cyan-700 border-cyan-200',
  moderation_api:
    'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-blue-100 text-blue-700 border-blue-200',
  custom:
    'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-surface-100 text-surface-600 border-surface-200',
  pii_block:
    'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-error/10 text-error border-error/20',
  budget_block:
    'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-warning/10 text-warning border-warning/20',
  rate_limit:
    'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-warning/10 text-warning border-warning/20',
  loop_detection:
    'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-cyan-100 text-cyan-700 border-cyan-200',
  content_policy:
    'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-blue-100 text-blue-700 border-blue-200',
  model_access:
    'inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium bg-violet-100 text-violet-700 border-violet-200',
}

function typeLabel(type: string): string {
  return type.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase())
}

export function TypeBadge({ type }: { type: string | null | undefined }) {
  const t = type?.toLowerCase() ?? ''
  const className = TYPE_COLORS[t] || TYPE_COLORS.custom
  return <span className={className}>{typeLabel(type ?? '')}</span>
}

/**
 * Effect Badge - Maps allow/deny to colors
 * Used in: McpAccessView
 */
export function EffectBadge({ effect }: { effect: string }) {
  const className =
    effect === 'allow'
      ? 'inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-semibold bg-green-100 text-green-700 border border-green-200'
      : 'inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-semibold bg-red-100 text-red-700 border border-red-200'
  return <span className={className}>{effect === 'allow' ? 'Allow' : 'Deny'}</span>
}

/**
 * Action Badge - Maps action strings to colors
 * Used in: GuardrailsView
 */
const ACTION_COLORS: Record<string, string> = {
  blocked: 'bg-error/10 text-error border-error/20',
  throttled: 'bg-error/10 text-error border-error/20',
  flagged: 'bg-warning/10 text-warning border-warning/20',
  warned: 'bg-warning/10 text-warning border-warning/20',
  alerted: 'bg-warning/10 text-warning border-warning/20',
  masked: 'bg-info/10 text-info border-info/20',
  allowed: 'bg-success/10 text-success border-success/20',
}

export function ActionBadge({ action }: { action: string }) {
  const className = ACTION_COLORS[action.toLowerCase()] || 'bg-surface-100 text-surface-600 border-surface-200'
  return (
    <span
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium capitalize ${className}`}
    >
      {action}
    </span>
  )
}

/**
 * Scope Badge - Maps scope/target_type to colors
 * Used in: GuardrailsView
 */
const SCOPE_COLORS: Record<string, string> = {
  global: 'bg-surface-100 text-surface-600 border-surface-200',
  user: 'bg-blue-100 text-blue-700 border-blue-200',
  group: 'bg-violet-100 text-violet-700 border-violet-200',
}

export function ScopeBadge({ scope, targetType }: { scope?: string; targetType?: string }) {
  const s = scope || targetType || 'global'
  const className = SCOPE_COLORS[s] || SCOPE_COLORS.global
  const label = s.charAt(0).toUpperCase() + s.slice(1)
  return (
    <span className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${className}`}>
      {label}
    </span>
  )
}

/**
 * Status Badge - Maps status strings to colors (active/inactive)
 * Used in: UserList, JobListView
 */
export function StatusBadge({ status, isActive }: { status?: string; isActive?: boolean }) {
  const active = isActive ?? (status === 'active' || status === '')
  const className = active
    ? 'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium bg-success/10 text-success border border-success/20'
    : 'inline-flex items-center rounded-full px-2 py-0.5 text-xs font-medium bg-surface-100 text-surface-500 border border-surface-200'

  const label = active ? 'Active' : status ? status.charAt(0).toUpperCase() + status.slice(1) : 'Inactive'
  return <span className={className}>{label}</span>
}

/**
 * Trigger Type Badge - Maps trigger types to colors
 * Used in: JobListView
 */
export function TriggerTypeBadge({ kind, title = '' }: { kind: string; title?: string }) {
  const configs: Record<string, { emoji: string; bg: string; border: string; text: string }> = {
    cron: {
      emoji: '⏰',
      bg: 'bg-amber-50',
      border: 'border-amber-200',
      text: 'text-amber-700',
    },
    webhook: {
      emoji: '🌐',
      bg: 'bg-sky-50',
      border: 'border-sky-200',
      text: 'text-sky-700',
    },
  }

  const config = configs[kind] || {
    emoji: '?',
    bg: 'bg-surface-50',
    border: 'border-surface-200',
    text: 'text-surface-600',
  }

  return (
    <span
      className={`inline-flex items-center gap-0.5 rounded ${config.bg} ${config.text} border ${config.border} px-1.5 py-0.5 text-[11px] font-medium leading-none`}
      title={title}
    >
      {config.emoji}
    </span>
  )
}

/**
 * Job Status Badge - Maps job execution status to colors
 * Used in: JobListView
 */
export function JobStatusBadge({ status }: { status?: string | null }) {
  const colors: Record<string, string> = {
    running: 'bg-yellow-400',
    success: 'bg-green-400',
    llm_success: 'bg-green-400',
    failed: 'bg-red-400',
    llm_failed: 'bg-red-400',
    delivery_failed: 'bg-red-400',
    budget_exceeded: 'bg-red-400',
  }

  const color = colors[status ?? ''] || 'bg-surface-300'
  return <span className={`inline-block w-2 h-2 rounded-full ${color}`} />
}
