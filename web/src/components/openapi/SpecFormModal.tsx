import { useEffect, useState } from 'react'
import type { OpenAPISpec } from '../../lib/api'
import { Button } from '../ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '../ui/card'

export function SpecFormModal({
  open,
  onClose,
  onSave,
  initial,
}: {
  open: boolean
  onClose: () => void
  onSave: (data: Record<string, unknown>) => void
  initial?: OpenAPISpec | null
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [description, setDescription] = useState(initial?.description ?? '')
  const [specUrl, setSpecUrl] = useState(initial?.spec_url ?? '')
  const [operations, setOperations] = useState(initial?.operations?.join(', ') ?? '')
  const [authType, setAuthType] = useState(initial?.auth_type ?? 'none')
  const [authValue, setAuthValue] = useState(initial?.auth_value ?? '')
  const [authKey, setAuthKey] = useState(initial?.auth_key ?? '')
  const [timeoutMs, setTimeoutMs] = useState(String(initial?.timeout_ms ?? 30000))
  const [enabled, setEnabled] = useState(initial?.enabled ?? true)

  useEffect(() => {
    if (initial) {
      setName(initial.name)
      setDescription(initial.description ?? '')
      setSpecUrl(initial.spec_url)
      setOperations(initial.operations?.join(', ') ?? '')
      setAuthType(initial.auth_type ?? 'none')
      setAuthValue(initial.auth_value ?? '')
      setAuthKey(initial.auth_key ?? '')
      setTimeoutMs(String(initial.timeout_ms ?? 30000))
      setEnabled(initial.enabled ?? true)
    } else {
      setName('')
      setDescription('')
      setSpecUrl('')
      setOperations('')
      setAuthType('none')
      setAuthValue('')
      setAuthKey('')
      setTimeoutMs('30000')
      setEnabled(true)
    }
  }, [initial])

  if (!open) return null

  const canSave = name.trim() && specUrl.trim()

  const handleSave = () => {
    if (!canSave) return
    onSave({
      name: name.trim(),
      description: description.trim(),
      spec_url: specUrl.trim(),
      operations: operations
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean),
      auth_type: authType,
      auth_value: authValue,
      auth_key: authKey,
      timeout_ms: parseInt(timeoutMs, 10) || 30000,
      enabled,
    })
    onClose()
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={onClose}>
      <Card className="w-full max-w-lg mx-4 max-h-[90vh] overflow-y-auto" onClick={(e) => e.stopPropagation()}>
        <CardHeader>
          <CardTitle>{initial ? 'Edit Spec' : 'Add Spec'}</CardTitle>
          <CardDescription>Configure an OpenAPI specification source.</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">
                Name <span className="text-error">*</span>
              </label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g., Petstore API"
                className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">Description</label>
              <input
                type="text"
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder="Short summary of what this API provides for the AI assistant"
                className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">
                Spec URL <span className="text-error">*</span>
              </label>
              <input
                type="text"
                value={specUrl}
                onChange={(e) => setSpecUrl(e.target.value)}
                placeholder="https://example.com/openapi.json"
                className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm font-mono text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">Operations</label>
              <textarea
                value={operations}
                onChange={(e) => setOperations(e.target.value)}
                placeholder="GET /pets, POST /pets, GET /pets/:id"
                rows={3}
                className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm font-mono text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
              <p className="mt-1 text-xs text-surface-400">Comma-separated list of operations to expose.</p>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">Auth Type</label>
                <select
                  value={authType}
                  onChange={(e) => setAuthType(e.target.value)}
                  className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                >
                  <option value="none">None</option>
                  <option value="bearer">Bearer Token</option>
                  <option value="api_key">API Key</option>
                  <option value="basic">Basic Auth</option>
                </select>
              </div>
              <div>
                <label className="block text-sm font-medium text-surface-700 mb-1">Timeout (ms)</label>
                <input
                  type="number"
                  value={timeoutMs}
                  onChange={(e) => setTimeoutMs(e.target.value)}
                  min={1000}
                  step={1000}
                  className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm font-mono text-surface-900 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                />
              </div>
            </div>
            {authType !== 'none' && (
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Auth Value</label>
                  <input
                    type="text"
                    value={authValue}
                    onChange={(e) => setAuthValue(e.target.value)}
                    placeholder={
                      authType === 'bearer'
                        ? 'Bearer token...'
                        : authType === 'api_key'
                          ? 'API key...'
                          : 'username:password'
                    }
                    className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm font-mono text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                  />
                </div>
                <div>
                  <label className="block text-sm font-medium text-surface-700 mb-1">Auth Key</label>
                  <input
                    type="text"
                    value={authKey}
                    onChange={(e) => setAuthKey(e.target.value)}
                    placeholder={authType === 'api_key' ? 'x-api-key' : 'Authorization'}
                    className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm font-mono text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                  />
                </div>
              </div>
            )}
            <div className="flex items-center gap-2">
              <input
                type="checkbox"
                id="spec-enabled"
                checked={enabled}
                onChange={(e) => setEnabled(e.target.checked)}
                className="h-4 w-4 rounded border-surface-300 text-brand-600 focus:ring-brand-500"
              />
              <label htmlFor="spec-enabled" className="text-sm text-surface-700">
                Enabled
              </label>
            </div>
            <div className="flex justify-end gap-3 pt-2">
              <Button variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button onClick={handleSave} disabled={!canSave}>
                {initial ? 'Save Changes' : 'Add Spec'}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
