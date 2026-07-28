import { formatRelativeTime } from '../../lib/format'
import { ExternalLink, Hash } from '../ui/icons'

export function TimeAgo({ date }: { date: string }) {
  return <>{formatRelativeTime(date)}</>
}

export function StatusBadge({ code }: { code: number }) {
  const colors: Record<string, string> = {
    '2': 'bg-success/10 text-success border-success/20',
    '4': 'bg-warning/10 text-warning border-warning/20',
    '5': 'bg-error/10 text-error border-error/20',
  }
  return (
    <span
      className={`inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium ${colors[String(code)[0]] || 'bg-surface-100 text-surface-600 border-surface-200'}`}
    >
      {code}
    </span>
  )
}

export function TraceBadge({ traceId }: { traceId: string }) {
  return (
    <a
      href={`/traces?trace=${traceId}`}
      className="inline-flex items-center gap-1 rounded-full bg-info/10 px-2 py-0.5 text-xs font-medium text-info border border-info/20 hover:bg-info/20 transition-colors"
      title={`Trace: ${traceId}`}
    >
      <Hash size={10} />
      Trace
    </a>
  )
}

export function DetailField({ label, value, mono }: { label: string; value: React.ReactNode; mono?: boolean }) {
  return (
    <div>
      <p className="text-xs font-medium text-surface-500 uppercase tracking-wider mb-0.5">{label}</p>
      <div className={mono ? 'font-mono text-xs text-surface-800 break-all' : 'text-sm text-surface-800'}>
        {value ?? <span className="text-surface-400">&mdash;</span>}
      </div>
    </div>
  )
}

export function TraceLink({ traceId }: { traceId: string }) {
  return (
    <a href={`/traces?trace=${traceId}`} className="inline-flex items-center gap-1 text-brand-600 hover:text-brand-700">
      {traceId}
      <ExternalLink size={12} />
    </a>
  )
}
