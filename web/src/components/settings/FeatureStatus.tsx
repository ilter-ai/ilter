import { Info } from '../ui/icons'
import { Switch } from '../ui/switch'

type ToggleStatus = {
  type: 'toggle'
  enabled: boolean
  onToggle: () => void
  disabled?: boolean
  label?: string
}

type CountStatus = {
  type: 'count'
  enabled: number
  total: number
}

type StaticStatus = {
  type: 'static'
  label?: string
  tooltip: string
}

type FeatureStatusProps = ToggleStatus | CountStatus | StaticStatus

export function FeatureStatus(props: FeatureStatusProps) {
  if (props.type === 'toggle') {
    return (
      <div className="flex items-center gap-3">
        <span className={`text-sm font-medium ${props.enabled ? 'text-success' : 'text-surface-500'}`}>
          {props.label ?? (props.enabled ? 'Enabled' : 'Disabled')}
        </span>
        <Switch checked={props.enabled} onCheckedChange={props.onToggle} disabled={props.disabled} />
      </div>
    )
  }

  if (props.type === 'count') {
    if (props.total === 0) {
      return (
        <span className="inline-flex items-center rounded-full border border-surface-200 bg-surface-50 px-3 py-1 text-xs font-medium text-surface-500">
          Not configured
        </span>
      )
    }
    return (
      <span className="inline-flex items-center rounded-full border border-surface-200 bg-surface-50 px-3 py-1 text-xs font-medium text-surface-700">
        {props.enabled} of {props.total} enabled
      </span>
    )
  }

  return (
    <div className="group relative flex items-center gap-1.5">
      <span className="inline-flex items-center rounded-full border border-success/20 bg-success/10 px-3 py-1 text-xs font-medium text-success">
        {props.label ?? 'Always on'}
      </span>
      <div className="relative">
        <button
          type="button"
          className="inline-flex items-center text-surface-400 hover:text-surface-600 focus:outline-none focus:ring-2 focus:ring-brand-500 rounded"
          aria-label="More information"
          tabIndex={0}
          onMouseEnter={(e) => {
            const tooltip = e.currentTarget.parentElement?.querySelector('[role="tooltip"]')
            if (tooltip) (tooltip as HTMLElement).style.display = 'block'
          }}
          onMouseLeave={(e) => {
            const tooltip = e.currentTarget.parentElement?.querySelector('[role="tooltip"]')
            if (tooltip) (tooltip as HTMLElement).style.display = 'none'
          }}
          onFocus={(e) => {
            const tooltip = e.currentTarget.parentElement?.querySelector('[role="tooltip"]')
            if (tooltip) (tooltip as HTMLElement).style.display = 'block'
          }}
          onBlur={(e) => {
            const tooltip = e.currentTarget.parentElement?.querySelector('[role="tooltip"]')
            if (tooltip) (tooltip as HTMLElement).style.display = 'none'
          }}
        >
          <Info size={14} />
        </button>
        <div
          role="tooltip"
          className="hidden absolute right-0 top-full mt-1 z-10 w-64 rounded-lg bg-surface-800 px-3 py-2 text-xs text-white shadow-lg"
        >
          {props.tooltip}
        </div>
      </div>
    </div>
  )
}
