import { Button } from '../ui/button'
import { Plus, Trash2 } from '../ui/icons'

interface EnvVarInputProps {
  /** Required env vars: key → value. Keys shown as non-editable labels. */
  requiredVars?: Record<string, string>
  /** Custom env vars (key+value pairs with add/remove). */
  customVars?: Array<{ key: string; value: string }>
  /** Called when a required var value changes. */
  onRequiredChange?: (key: string, value: string) => void
  /** Called when custom vars change. */
  onCustomChange?: (vars: Array<{ key: string; value: string }>) => void
  /** Validation errors. */
  errors?: { envVars?: string; customKeys?: string }
}

const inputClass =
  'w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500'

const labelClass = 'block text-xs font-medium text-surface-500 mb-1'

export function EnvVarInput({
  requiredVars = {},
  customVars = [],
  onRequiredChange,
  onCustomChange,
  errors,
}: EnvVarInputProps) {
  const addCustom = () => {
    onCustomChange?.([...customVars, { key: '', value: '' }])
  }

  const updateCustom = (index: number, field: 'key' | 'value', val: string) => {
    const next = [...customVars]
    next[index] = { ...next[index], [field]: field === 'key' ? val.toUpperCase() : val }
    onCustomChange?.(next)
  }

  const removeCustom = (index: number) => {
    onCustomChange?.(customVars.filter((_, i) => i !== index))
  }

  const hasVars = Object.keys(requiredVars).length > 0 || customVars.length > 0

  return (
    <div>
      <div className="flex items-center justify-between mb-2">
        <label className={labelClass}>Environment Variables</label>
        <Button type="button" variant="outline" size="sm" onClick={addCustom}>
          <Plus size={14} />
          Add Variable
        </Button>
      </div>

      {!hasVars ? (
        <p className="text-xs text-surface-400 py-2">No environment variables required</p>
      ) : (
        <div className="space-y-2">
          {Object.entries(requiredVars).map(([key, value]) => (
            <div key={key} className="flex items-center gap-2">
              <span className="text-xs font-semibold text-surface-700 uppercase w-1/3 truncate" title={key}>
                {key}
              </span>
              <input
                type="text"
                value={value}
                onChange={(e) => onRequiredChange?.(key, e.target.value)}
                placeholder={`Enter ${key} value`}
                className={inputClass.replace('w-full', 'flex-1')}
              />
            </div>
          ))}

          {customVars.map((env, index) => (
            <div key={index} className="flex items-center gap-2">
              <input
                type="text"
                value={env.key}
                onChange={(e) => updateCustom(index, 'key', e.target.value)}
                placeholder="KEY_NAME"
                className={`font-mono text-xs uppercase ${inputClass.replace('w-full', 'w-1/3')}`}
              />
              <input
                type="text"
                value={env.value}
                onChange={(e) => updateCustom(index, 'value', e.target.value)}
                placeholder="Value"
                className={inputClass.replace('w-full', 'flex-1')}
              />
              <button
                type="button"
                onClick={() => removeCustom(index)}
                className="shrink-0 rounded-lg p-1.5 text-surface-400 hover:bg-surface-100 hover:text-error transition-colors"
                aria-label="Remove variable"
              >
                <Trash2 size={14} />
              </button>
            </div>
          ))}
        </div>
      )}

      {errors?.envVars && <p className="mt-1 text-xs text-error">{errors.envVars}</p>}
      {errors?.customKeys && <p className="mt-1 text-xs text-error">{errors.customKeys}</p>}
    </div>
  )
}
