import type { ReactNode } from 'react'
import { cn } from '../../lib/utils'
import { Button } from './button'
import { Monitor } from './icons'

interface EmptyStateProps {
  icon?: ReactNode
  title: string
  description?: string
  action?: {
    label: string
    onClick: () => void
  }
  className?: string
}

export function EmptyState({ icon, title, description, action, className }: EmptyStateProps) {
  return (
    <div className={cn('flex flex-col items-center justify-center py-16', className)}>
      <div className="text-surface-300">
        {icon ?? <Monitor size={48} strokeWidth={1.5} className="text-surface-300" />}
      </div>
      <p className="mt-4 text-sm font-medium text-surface-500">{title}</p>
      {description && <p className="mt-1 text-xs text-surface-400 max-w-sm text-center">{description}</p>}
      {action && (
        <Button variant="outline" size="sm" className="mt-4" onClick={action.onClick}>
          {action.label}
        </Button>
      )}
    </div>
  )
}
