import { Code2, Edit3 } from 'lucide-react'
import { useMemo, useRef, useState } from 'react'
import { useLocalStorage } from '../../lib/useLocalStorage'
import { Button } from '../ui/button'

const FORMABLE = new Set(['string', 'number', 'integer', 'boolean'])

export function isFormable(schema: Record<string, unknown> | null): boolean {
  if (schema?.type !== 'object' || !schema.properties) return false
  return Object.values(schema.properties as Record<string, unknown>).every((p: unknown) => {
    const prop = p as Record<string, unknown>
    if (prop.oneOf || prop.anyOf) return false
    if (typeof prop.type === 'string' && FORMABLE.has(prop.type)) return true
    if (
      prop.type === 'array' &&
      prop.items &&
      ['string', 'number', 'integer'].includes((prop.items as Record<string, unknown>)?.type as string)
    )
      return true
    return false
  })
}

function coerce(raw: string, type: string): unknown {
  if (raw === '') return type === 'string' ? '' : undefined
  if (raw.includes('{{')) return raw
  if (type === 'number' || type === 'integer') {
    const n = Number(raw)
    return Number.isNaN(n) ? raw : n
  }
  return raw
}

interface EnumFieldProps {
  enumValues: string[]
  value: string | undefined
  enumValue: string | undefined
  onChange: (v: unknown) => void
  defaultVal: string
  typeName: string
}

function EnumField({ enumValues, value, enumValue, onChange, defaultVal, typeName }: EnumFieldProps) {
  const [custom, setCustom] = useState(() => value !== undefined && !enumValues.includes(value as string))
  if (custom || (value !== undefined && !enumValues.includes(value as string))) {
    return (
      <div className="flex gap-1">
        <input
          type="text"
          value={value ?? ''}
          placeholder={defaultVal ?? typeName}
          onChange={(e) => onChange(coerce(e.target.value, typeName))}
          className="flex-1 h-7 rounded-md border border-surface-300 bg-white px-2 text-xs text-surface-700 font-mono placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
        />
        <button
          type="button"
          onClick={() => {
            setCustom(false)
            onChange(enumValues[0] ?? '')
          }}
          className="text-[10px] text-brand-600 hover:text-brand-800 underline shrink-0 self-center"
        >
          Pick…
        </button>
      </div>
    )
  }
  return (
    <div className="flex gap-1">
      <select
        value={enumValue ?? ''}
        onChange={(e) => onChange(e.target.value)}
        className="flex-1 h-7 rounded-md border border-surface-300 bg-white px-2 text-xs text-surface-700 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
      >
        <option value="">—</option>
        {enumValues.map((o) => (
          <option key={o} value={o}>
            {o}
          </option>
        ))}
      </select>
      <button
        type="button"
        onClick={() => setCustom(true)}
        className="text-[10px] text-brand-600 hover:text-brand-800 underline shrink-0 self-center"
      >
        Expression…
      </button>
    </div>
  )
}

interface Props {
  schema: Record<string, unknown> | null
  value: Record<string, unknown>
  onChange: (v: Record<string, unknown>) => void
  hint?: string
}

function isComplexType(prop: Record<string, unknown>): boolean {
  const t = prop.type as string
  if (prop.oneOf || prop.anyOf) return true
  if (t === 'object') return true
  if (t === 'array' && prop.items) {
    const itemType = (prop.items as Record<string, unknown>)?.type as string
    if (itemType && !['string', 'number', 'integer'].includes(itemType)) return true
  }
  return false
}

function ComplexField({
  value: fieldVal,
  onChange: onFieldChange,
  placeholder,
}: {
  value: unknown
  onChange: (v: unknown) => void
  placeholder: string
}) {
  const init = typeof fieldVal === 'string' ? fieldVal : (JSON.stringify(fieldVal, null, 2) ?? '')
  const [raw, setRaw] = useState(init)
  const prev = useRef(fieldVal)
  if (prev.current !== fieldVal) {
    prev.current = fieldVal
    setRaw(typeof fieldVal === 'string' ? fieldVal : (JSON.stringify(fieldVal, null, 2) ?? ''))
  }
  return (
    <textarea
      value={raw}
      placeholder={placeholder}
      onChange={(e) => setRaw(e.target.value)}
      onBlur={() => {
        const trimmed = raw.trim()
        if (!trimmed) {
          onFieldChange(undefined)
          return
        }
        if (trimmed.includes('{{')) {
          onFieldChange(trimmed)
          return
        }
        try {
          onFieldChange(JSON.parse(trimmed))
        } catch {
          /* keep raw, user sees their text */
        }
      }}
      rows={3}
      className="w-full rounded-md border border-surface-300 bg-[#1e1e1e] px-2 py-1.5 text-xs text-green-300 font-mono placeholder-surface-500 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 resize-none"
      spellCheck={false}
    />
  )
}

const STORAGE_KEY = 'mcp-arg-mode'

export function McpToolParamFields({ schema, value, onChange, hint }: Props) {
  const [mode, setMode] = useLocalStorage<'form' | 'json'>(STORAGE_KEY, 'form')
  const [text, setText] = useState(() => JSON.stringify(value, null, 2))
  const [initialText] = useState(() => JSON.stringify(value, null, 2))

  const badJson = useMemo(() => {
    try {
      JSON.parse(text)
      return false
    } catch {
      return true
    }
  }, [text])

  if (!schema) {
    return (
      <div>
        <textarea
          value={text}
          onChange={(e) => {
            setText(e.target.value)
            try {
              onChange(JSON.parse(e.target.value))
            } catch {
              /* partial JSON while typing */
            }
          }}
          className="w-full h-28 rounded-lg border border-surface-300 bg-[#1e1e1e] px-3 py-2 font-mono text-xs text-green-300 placeholder-surface-500 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 resize-none"
          spellCheck={false}
          placeholder='{ "param": "value" }'
        />
      </div>
    )
  }

  if (mode === 'json') {
    return (
      <div>
        <div className="flex items-center justify-between mb-1">
          <span className="text-[11px] font-medium text-surface-400">Arguments (JSON)</span>
          <Button
            variant="ghost"
            size="xs"
            onClick={() => {
              if (badJson) return
              onChange(JSON.parse(text))
              setMode('form')
            }}
            disabled={badJson}
            className="h-6 text-xs gap-1"
          >
            <Edit3 size={12} />
            Form mode
          </Button>
        </div>
        <textarea
          value={text}
          onChange={(e) => setText(e.target.value)}
          className="w-full h-28 rounded-lg border border-surface-300 bg-[#1e1e1e] px-3 py-2 font-mono text-xs text-green-300 placeholder-surface-500 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 resize-none"
          spellCheck={false}
          placeholder='{ "param": "value" }'
        />
        {badJson && text !== initialText && (
          <p className="text-[11px] text-error mt-1">Invalid JSON — fix or switch back</p>
        )}
      </div>
    )
  }

  const properties = (schema.properties as Record<string, Record<string, unknown>>) || {}
  const required = (schema.required as string[]) || []

  return (
    <div>
      <div className="flex items-center justify-between mb-1">
        <span className="text-[11px] font-medium text-surface-400">Arguments</span>
        <Button
          variant="ghost"
          size="xs"
          onClick={() => {
            setText(JSON.stringify(value, null, 2))
            setMode('json')
          }}
          className="h-6 text-xs gap-1"
        >
          <Code2 size={12} />
          JSON mode
        </Button>
      </div>
      <div className="space-y-2">
        {Object.entries(properties).map(([key, prop]) => {
          const ptype = prop.type as string
          const val = value[key]
          const isExpr = typeof val === 'string' && val.includes('{{')
          return (
            <div key={key}>
              <label className="block text-[11px] font-medium text-surface-500 mb-0.5">
                {key}
                {required.includes(key) && <span className="text-error ml-0.5">*</span>}
                {(prop.description as string) && (
                  <span className="font-normal text-surface-400 ml-1">— {prop.description as string}</span>
                )}
              </label>
              {isComplexType(prop) ? (
                <ComplexField
                  key={key}
                  value={val}
                  onChange={(v) => onChange({ ...value, [key]: v })}
                  placeholder={ptype === 'object' ? '{ "key": "value" }' : '['}
                />
              ) : ptype === 'boolean' ? (
                <select
                  value={val == null ? '' : val ? 'true' : 'false'}
                  onChange={(e) => {
                    const v = e.target.value
                    onChange({ ...value, [key]: v === '' ? undefined : v === 'true' })
                  }}
                  className="w-full h-7 rounded-md border border-surface-300 bg-white px-2 text-xs text-surface-700 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                >
                  <option value="">—</option>
                  <option value="true">true</option>
                  <option value="false">false</option>
                </select>
              ) : prop.enum ? (
                <EnumField
                  enumValues={prop.enum as string[]}
                  value={isExpr ? (val as string) : undefined}
                  enumValue={!isExpr ? (val as string) : undefined}
                  onChange={(v) => onChange({ ...value, [key]: v })}
                  defaultVal={prop.default as string}
                  typeName={ptype}
                />
              ) : ptype === 'array' ? (
                <input
                  type="text"
                  value={Array.isArray(val) ? val.join(', ') : ((val as string) ?? '')}
                  placeholder={(prop.default as string) ?? 'comma-separated'}
                  onChange={(e) => {
                    const raw = e.target.value
                    if (raw.includes('{{')) {
                      onChange({ ...value, [key]: raw })
                    } else {
                      onChange({
                        ...value,
                        [key]: raw
                          .split(',')
                          .map((s: string) => s.trim())
                          .filter(Boolean),
                      })
                    }
                  }}
                  className="w-full h-7 rounded-md border border-surface-300 bg-white px-2 text-xs text-surface-700 font-mono placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                />
              ) : (
                <input
                  type="text"
                  inputMode={ptype === 'number' || ptype === 'integer' ? 'decimal' : 'text'}
                  value={(val as string) ?? ''}
                  placeholder={(prop.default as string) ?? ptype}
                  onChange={(e) => onChange({ ...value, [key]: coerce(e.target.value, ptype) })}
                  className="w-full h-7 rounded-md border border-surface-300 bg-white px-2 text-xs text-surface-700 font-mono placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                />
              )}
            </div>
          )
        })}
      </div>
      {hint && <p className="text-[10px] text-surface-400 mt-2 leading-relaxed">{hint}</p>}
    </div>
  )
}
