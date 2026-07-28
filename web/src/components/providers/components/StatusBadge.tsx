interface StatusBadgeProps {
  status: string
}

export function StatusBadge({ status }: StatusBadgeProps) {
  const label = statusLabel(status)

  const chipClass =
    status === 'online'
      ? 'bg-success/10 text-success'
      : status === 'degraded'
        ? 'bg-warning/10 text-warning'
        : 'bg-error/10 text-error'

  return <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${chipClass}`}>{label}</span>
}

function statusLabel(status: string): string {
  switch (status) {
    case 'online':
      return 'Online'
    case 'degraded':
      return 'Degraded'
    default:
      return 'Offline'
  }
}
