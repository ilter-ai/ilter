import { ArrowLeft } from 'lucide-react'
import { Button } from './button'

export function BackButton({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <Button variant="ghost" size="sm" onClick={onClick}>
      <ArrowLeft size={16} className="mr-1.5" /> {label}
    </Button>
  )
}
