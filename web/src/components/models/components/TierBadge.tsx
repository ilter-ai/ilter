import type { ModelTier } from '../useModels'

interface TierBadgeProps {
  tier: ModelTier
}

const tierStyles: Record<ModelTier, string> = {
  premium: 'bg-brand-100 text-brand-700',
  standard: 'bg-surface-100 text-surface-700',
  economy: 'bg-warning/10 text-warning',
  free: 'bg-success/10 text-success',
}

export function TierBadge({ tier }: TierBadgeProps) {
  return <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${tierStyles[tier]}`}>{tier}</span>
}
