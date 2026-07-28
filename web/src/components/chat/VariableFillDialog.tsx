import { Variable } from 'lucide-react'
import { useState } from 'react'
import { Button } from '../ui/button'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '../ui/dialog'
import { Input } from '../ui/input'

export function VariableFillDialog({
  templateName,
  variables,
  onResolve,
  onClose,
}: {
  templateName: string
  variables: string[]
  onResolve: (values: Record<string, string>) => void
  onClose: () => void
}) {
  const [values, setValues] = useState<Record<string, string>>(() => Object.fromEntries(variables.map((v) => [v, ''])))

  const allFilled = variables.every((v) => values[v]?.trim())

  const handleSubmit = () => {
    if (!allFilled) return
    onResolve(values)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey && allFilled) {
      e.preventDefault()
      handleSubmit()
    }
  }

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose()
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Variable size={18} className="text-brand-600" />
            <span>Fill Variables</span>
          </DialogTitle>
          <p className="text-sm text-surface-500">
            Template: <span className="font-medium text-surface-700">{templateName}</span>
          </p>
        </DialogHeader>

        <div className="space-y-4">
          {variables.map((v) => (
            <div key={v}>
              <label className="block text-sm font-medium text-surface-700 mb-1.5 capitalize">
                {v.replace(/_/g, ' ')}
              </label>
              <Input
                value={values[v]}
                onChange={(e) => setValues((prev) => ({ ...prev, [v]: e.target.value }))}
                onKeyDown={handleKeyDown}
                placeholder={`Enter ${v.replace(/_/g, ' ')}...`}
                autoFocus={variables.indexOf(v) === 0}
              />
            </div>
          ))}
        </div>

        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose}>
            Cancel
          </Button>
          <Button variant="default" size="sm" disabled={!allFilled} onClick={handleSubmit}>
            {variables.length === 0 ? 'Use Template' : 'Fill & Send'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
