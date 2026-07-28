import { useState } from 'react'
import { Button } from '../../ui/button'
import { OSTabbedCodeSnippet } from '../../ui/CodeSnippet'
import { Info, Key, Layers, Terminal, X } from '../../ui/icons'
import type { ProviderInfo } from '../useProviders'

function getEnvVarName(name: string): string {
  return `ILTER_PROVIDER_${name.replace(/-/g, '_').toUpperCase()}_API_KEY`
}

function isEnvSource(source: string): boolean {
  return source.startsWith('ILTER_PROVIDER_')
}

interface ConfigModalProps {
  provider: ProviderInfo
  baseUrl: string
  apiKey: string
  apiKeyTouched: boolean
  onBaseUrlChange: (url: string) => void
  onApiKeyChange: (key: string) => void
  onMultiKeysChange?: (keys: string[]) => void
  onSave: () => void
  onClose: () => void
}

export function ConfigModal({
  provider,
  baseUrl,
  apiKey,
  apiKeyTouched,
  onBaseUrlChange,
  onApiKeyChange,
  onMultiKeysChange,
  onSave,
  onClose,
}: ConfigModalProps) {
  const envVarName = getEnvVarName(provider.name)
  const envSet = isEnvSource(provider.api_key_source)
  const dbHasKey = provider.api_key_set && !envSet

  // Mode: 'single' or 'pool'
  const initialMode = provider.api_keys_count && provider.api_keys_count > 1 ? 'pool' : 'single'
  const [mode, setMode] = useState<'single' | 'pool'>(initialMode)
  const [multiKeysInput, setMultiKeysInput] = useState('')

  const getStatusPill = () => {
    if (envSet) {
      return {
        bg: 'bg-purple-50 text-purple-700 border-purple-200',
        dot: 'bg-purple-500',
        label: 'Set via Environment',
        desc: `Active key is injected via ${envVarName}. Environment variables take precedence over UI database settings.`,
      }
    }
    if (provider.api_keys_count && provider.api_keys_count > 1) {
      return {
        bg: 'bg-emerald-50 text-emerald-700 border-emerald-200',
        dot: 'bg-emerald-500',
        label: `${provider.api_keys_count} Keys Configured (Failover Active)`,
        desc: 'Multiple API keys configured in pool. If rate limits or quota issues occur, ILTER automatically cycles to the next healthy key.',
      }
    }
    if (dbHasKey) {
      return {
        bg: 'bg-blue-50 text-blue-700 border-blue-200',
        dot: 'bg-blue-500',
        label: 'Database Key Active',
        desc: 'Stored in gateway database. Used whenever no environment variable is present.',
      }
    }
    return {
      bg: 'bg-surface-100 text-surface-600 border-surface-200',
      dot: 'bg-surface-400',
      label: 'Not Configured',
      desc: 'No API key set. Provide a key below or set via environment variable.',
    }
  }

  const status = getStatusPill()

  const handleMultiTextChange = (text: string) => {
    setMultiKeysInput(text)
    const rawKeys = text
      .split(/[\n,]/)
      .map((k) => k.trim())
      .filter((k) => k !== '')
    if (onMultiKeysChange) {
      onMultiKeysChange(rawKeys)
    }
  }

  // Dynamic environment variable code snippets
  const envValueExample = mode === 'pool' ? 'sk-key1,sk-key2,sk-key3' : 'sk-your-api-key'
  const envSnippetBash = `export ${envVarName}="${envValueExample}"`
  const envSnippetPowershell = `$env:${envVarName} = "${envValueExample}"`

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm p-4"
      onClick={onClose}
    >
      <div
        className="w-full max-w-2xl overflow-hidden rounded-2xl bg-white shadow-2xl border border-surface-200 flex flex-col max-h-[90vh]"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Modal Header */}
        <div className="px-6 py-4 border-b border-surface-100 bg-surface-50/50 flex items-center justify-between">
          <div className="flex items-center gap-3">
            <span
              className={`inline-block h-2.5 w-2.5 rounded-full ${
                provider.status === 'online'
                  ? 'bg-emerald-500 shadow-sm'
                  : provider.status === 'degraded'
                    ? 'bg-amber-500'
                    : 'bg-rose-500'
              }`}
            />
            <div>
              <h3 className="text-base font-semibold text-surface-900">{provider.name}</h3>
              <p className="text-xs text-surface-500 font-mono">{provider.type}</p>
            </div>
          </div>

          <div className="flex items-center gap-3">
            <span
              className={`inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium border ${status.bg}`}
            >
              <span className={`w-1.5 h-1.5 rounded-full ${status.dot}`} />
              {status.label}
            </span>
            <button
              type="button"
              onClick={onClose}
              className="rounded-lg p-1.5 text-surface-400 hover:bg-surface-200 hover:text-surface-600 transition-colors"
            >
              <X size={18} />
            </button>
          </div>
        </div>

        {/* Modal Content: Two-Column Layout */}
        <div className="p-6 overflow-y-auto min-w-0">
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6 min-w-0">
            {/* LEFT COLUMN: Configuration Controls */}
            <div className="space-y-4 min-w-0">
              <div className="flex items-center justify-between">
                <span className="text-xs font-semibold text-surface-900 uppercase tracking-wider">
                  Provider Settings
                </span>
              </div>

              {/* Segmented Mode Switcher */}
              <div>
                <label className="block text-xs font-medium text-surface-500 mb-1.5">Key Management Mode</label>
                <div className="grid grid-cols-2 gap-1 p-1 bg-surface-100 rounded-lg border border-surface-200">
                  <button
                    type="button"
                    onClick={() => setMode('single')}
                    className={`flex items-center justify-center gap-1.5 py-1.5 text-xs font-medium rounded-md transition-all ${
                      mode === 'single'
                        ? 'bg-white text-surface-900 shadow-sm border border-surface-200 font-semibold'
                        : 'text-surface-600 hover:text-surface-900'
                    }`}
                  >
                    <Key size={13} /> Single Key
                  </button>
                  <button
                    type="button"
                    onClick={() => setMode('pool')}
                    className={`flex items-center justify-center gap-1.5 py-1.5 text-xs font-medium rounded-md transition-all ${
                      mode === 'pool'
                        ? 'bg-white text-brand-700 shadow-sm border border-brand-200 font-semibold'
                        : 'text-surface-600 hover:text-surface-900'
                    }`}
                  >
                    <Layers size={13} /> Failover Pool
                  </button>
                </div>
              </div>

              {/* Base URL Input */}
              <div>
                <label htmlFor="provider-base-url" className="block text-xs font-medium text-surface-700 mb-1">
                  Base URL
                </label>
                <input
                  id="provider-base-url"
                  type="text"
                  value={baseUrl}
                  onChange={(e) => onBaseUrlChange(e.target.value)}
                  placeholder="https://api.example.com/v1"
                  className="w-full rounded-lg border border-surface-300 px-3 py-2 text-sm text-surface-900 placeholder:text-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                />
              </div>

              {/* API Key Input(s) */}
              <div>
                {mode === 'single' ? (
                  <div>
                    <label htmlFor="single-api-key" className="block text-xs font-medium text-surface-700 mb-1">
                      API Key
                    </label>
                    <input
                      id="single-api-key"
                      type="password"
                      value={apiKey}
                      onChange={(e) => onApiKeyChange(e.target.value)}
                      placeholder={
                        envSet
                          ? 'Set in env var — override here'
                          : dbHasKey && !apiKeyTouched
                            ? 'Key is configured — type to change'
                            : 'Enter API key'
                      }
                      className="w-full rounded-lg border border-surface-300 px-3 py-2 text-sm text-surface-900 placeholder:text-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 font-mono"
                    />
                    {dbHasKey && !apiKeyTouched && (
                      <p className="mt-1 text-[11px] text-surface-500">
                        Key set in database. Enter a new value to update.
                      </p>
                    )}
                  </div>
                ) : (
                  <div>
                    <label htmlFor="multi-api-keys" className="block text-xs font-medium text-surface-700 mb-1">
                      API Keys Pool (Line or Comma Separated)
                    </label>
                    <textarea
                      id="multi-api-keys"
                      rows={4}
                      value={multiKeysInput}
                      onChange={(e) => handleMultiTextChange(e.target.value)}
                      placeholder={'sk-key1...\nsk-key2...\nsk-key3...'}
                      className="w-full rounded-lg border border-surface-300 px-3 py-2 text-xs font-mono text-surface-900 placeholder:text-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                    />
                    <p className="mt-1 text-[11px] text-surface-500">
                      Enter multiple API tokens. If rate limits (429) or quota errors occur, ILTER automatically uses
                      the next key in line.
                    </p>
                  </div>
                )}
              </div>
            </div>

            {/* RIGHT COLUMN: Environment Variable Guide & Docs */}
            <div className="space-y-4 min-w-0 bg-surface-50 p-4 rounded-xl border border-surface-200 flex flex-col justify-between">
              <div className="space-y-3 min-w-0">
                <div className="flex items-center justify-between">
                  <span className="text-xs font-semibold text-surface-900 uppercase tracking-wider flex items-center gap-1.5">
                    <Terminal size={14} className="text-brand-600" /> Setup via Environment
                  </span>
                </div>

                <p className="text-xs text-surface-600 leading-relaxed">{status.desc}</p>

                <OSTabbedCodeSnippet
                  label={mode === 'pool' ? 'Multiple Keys Export' : 'Single Key Export'}
                  bash={envSnippetBash}
                  powershell={envSnippetPowershell}
                />

                <div className="rounded-lg bg-white p-3 border border-surface-200 space-y-1 text-[11px] text-surface-600">
                  <div className="font-semibold text-surface-800 flex items-center gap-1">
                    <Info size={13} className="text-brand-600 shrink-0" /> Unified Variable Format
                  </div>
                  <p>
                    <code className="font-mono font-semibold text-surface-800">{envVarName}</code> supports single API
                    keys or multiple comma-separated keys (`sk-1,sk-2`) for automatic key failover pools.
                  </p>
                </div>
              </div>
            </div>
          </div>
        </div>

        {/* Modal Footer */}
        <div className="flex items-center justify-end gap-2 px-6 py-4 border-t border-surface-100 bg-surface-50/50">
          <Button variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button onClick={onSave}>Save Changes</Button>
        </div>
      </div>
    </div>
  )
}
