import type { ReactNode } from 'react'
import { BackButton } from './BackButton'

export function ManagementViewLayout({
  title,
  onBack,
  children,
}: {
  title: string
  onBack: () => void
  children: ReactNode
}) {
  return (
    <div className="space-y-6">
      <BackButton
        label={`Back to ${title
          .replace(' ', '')
          .replace(/([A-Z])/g, ' $1')
          .trim()}`}
        onClick={onBack}
      />
      <h2 className="text-xl font-semibold text-surface-900">{title}</h2>
      {children}
    </div>
  )
}
