import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { api } from '../../lib/api'
import type { MCPServerEntry } from '../../types/mcp'
import { Button } from '../ui/button'
import { Loader2, X } from '../ui/icons'

// --- Inlined error patterns (previously src/data/error-patterns.ts) ---

interface RemediationStep {
  label: string
  action: 'update-args' | 'update-command' | 'update-env' | 'install-package' | 'check-network' | 'check-auth'
  field?: 'args' | 'command' | 'env'
  hint: string
}

interface ErrorPattern {
  /** Unique identifier for this pattern. */
  id: string
  /** Regex tested against `stderr` (lowercased). */
  stderrMatch: RegExp
  /** Regex tested against the `error` message (lowercased). */
  errorMatch?: RegExp
  /** Human-readable title for the remediation card. */
  title: string
  /** Short description of what went wrong. */
  description: string
  /** One or more suggested fix steps. */
  steps: RemediationStep[]
}

const ERROR_PATTERNS: ErrorPattern[] = [
  // ── Missing required argument (server-filesystem etc.) ─────────────────────
  {
    id: 'missing-arg',
    stderrMatch: /please\s+(specify|provide|add)|missing\s+(required\s+)?arg|expected\s+argument/i,
    title: 'Missing configuration',
    description:
      'This MCP server needs additional command-line arguments to run. For example, server-filesystem requires a directory path.',
    steps: [
      {
        label: 'Add required arguments',
        action: 'update-args',
        field: 'args',
        hint: 'Add the missing arguments, e.g. a directory path for filesystem access.',
      },
    ],
  },

  // ── Command not found (npx, docker, uvx not installed) ─────────────────────
  {
    id: 'command-not-found',
    stderrMatch: /(command|executable)\s+not\s+found|no\s+such\s+file|not\s+found/i,
    errorMatch: /not\s+found\s+on\s+PATH/i,
    title: 'Command not found',
    description: 'The launch command was not found on the system PATH. The required runtime may not be installed.',
    steps: [
      {
        label: 'Install the required runtime',
        action: 'install-package',
        hint: 'Install the required package (e.g. npm install -g <package>, brew install, pip install).',
      },
      {
        label: 'Check command spelling',
        action: 'update-command',
        field: 'command',
        hint: 'Verify the command name is correct. Try running it in your terminal to confirm.',
      },
    ],
  },

  // ── npx / npm errors ───────────────────────────────────────────────────────
  {
    id: 'npx-error',
    stderrMatch: /(npm\s+err|could\s+not\s+determine\s+executable|package\s+not\s+found|enoent)/i,
    title: 'Package resolution failed',
    description:
      'npx could not resolve or download the package. This may be a network issue or a typo in the package name.',
    steps: [
      {
        label: 'Check package name',
        action: 'update-command',
        field: 'command',
        hint: 'Make sure the package name is correct (e.g. @modelcontextprotocol/server-filesystem).',
      },
      {
        label: 'Check network connectivity',
        action: 'check-network',
        hint: 'Ensure the server has internet access to download npm packages.',
      },
    ],
  },

  // ── Docker errors ──────────────────────────────────────────────────────────
  {
    id: 'docker-error',
    stderrMatch: /(docker|container|daemon).*(error|not\s+found|connect|cannot)/i,
    title: 'Docker error',
    description:
      'The MCP server failed to start via Docker. Docker may not be installed or the daemon may not be running.',
    steps: [
      {
        label: 'Check Docker is running',
        action: 'check-network',
        hint: 'Run `docker ps` in your terminal to verify Docker is installed and the daemon is running.',
      },
      {
        label: 'Check image name',
        action: 'update-command',
        field: 'command',
        hint: 'Verify the Docker image name is correct.',
      },
    ],
  },

  // ── Python / pip errors ─────────────────────────────────────────────────────
  {
    id: 'python-error',
    stderrMatch: /(python|pip|uvx|module\s+not\s+found|modulenotfounderror|importerror)/i,
    title: 'Python dependency error',
    description: 'A Python module is missing or there is a Python environment issue.',
    steps: [
      {
        label: 'Install Python dependencies',
        action: 'install-package',
        hint: 'Install required Python packages (e.g. pip install <package> or uvx <package>).',
      },
    ],
  },

  // ── Connection refused / timeout ───────────────────────────────────────────
  {
    id: 'connection-error',
    stderrMatch: /(connection\s+refused|connect\s+refused|econnrefused|timeout|i\/o\s+timeout)/i,
    title: 'Connection error',
    description: 'The server could not connect to a remote endpoint. Check the URL and network connectivity.',
    steps: [
      {
        label: 'Check server URL',
        action: 'update-args',
        field: 'args',
        hint: 'Verify the URL is correct and the remote server is accessible.',
      },
      {
        label: 'Check network',
        action: 'check-network',
        hint: 'Ensure the server has network access to reach the endpoint.',
      },
    ],
  },
]

/**
 * Match an error against the registry.
 *
 * Returns the first matching pattern and the matched step suggestions.
 * Priority: stderr match first, error message match second.
 */
function matchError(errorMsg: string, stderr: string): ErrorPattern | null {
  const stderrLower = stderr.toLowerCase().trim()
  const errorLower = errorMsg.toLowerCase().trim()

  for (const pattern of ERROR_PATTERNS) {
    if (pattern.stderrMatch && stderrLower && pattern.stderrMatch.test(stderrLower)) {
      return pattern
    }
    if (pattern.errorMatch && errorLower && pattern.errorMatch.test(errorLower)) {
      return pattern
    }
  }
  return null
}

interface DiscoverInstallModalProps {
  open: boolean
  server: MCPServerEntry | null
  onClose: () => void
  onSuccess: () => void
}

type ViewMode = 'form' | 'result'

interface TestResult {
  status: string
  tools_count: number
  error?: string
  stderr?: string
}

interface ValidationErrors {
  name?: string
  package?: string
  command?: string
  variables?: string
}

function XIcon() {
  return <X size={18} />
}

function SpinnerIcon() {
  return <Loader2 className="animate-spin -ml-1 mr-2 h-4 w-4" />
}

const TRANSPORT_OPTIONS = [
  { value: 'stdio', label: 'stdio' },
  { value: 'sse', label: 'SSE' },
  { value: 'streamable-http', label: 'Streamable HTTP' },
]

const inputClass =
  'w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500'

const inputErrorClass =
  'w-full rounded-lg border border-error bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-error focus:outline-none focus:ring-1 focus:ring-error'

const labelClass = 'block text-xs font-medium text-surface-500 mb-1'

const selectClass =
  'w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 appearance-none cursor-pointer'

export default function DiscoverInstallModal({ open, server, onClose, onSuccess }: DiscoverInstallModalProps) {
  const [name, setName] = useState('')
  const [pkg, setPkg] = useState('')
  const [transport, setTransport] = useState('stdio')
  const [command, setCommand] = useState('npx')
  const [varValues, setVarValues] = useState<Record<string, string>>({})
  const [timeout, setTimeout_] = useState(30000)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [validationErrors, setValidationErrors] = useState<ValidationErrors>({})

  const [view, setView] = useState<ViewMode>('form')
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<TestResult | null>(null)
  const [matchedPattern, setMatchedPattern] = useState<ErrorPattern | null>(null)
  const [editingServerId, setEditingServerId] = useState<string | null>(null)

  useEffect(() => {
    if (server && open) {
      setName(server.name)
      setPkg(server.package)
      setTransport('stdio')
      setCommand(server.command || 'npx')
      const initial: Record<string, string> = {}
      if (server.variables) {
        server.variables.forEach((v) => {
          initial[v.key] = v.default || ''
        })
      }
      setVarValues(initial)
      setTimeout_(30000)
      setError(null)
      setValidationErrors({})
      setSaving(false)
      setView('form')
      setTesting(false)
      setTestResult(null)
      setMatchedPattern(null)
      setEditingServerId(null)
    }
  }, [server, open])

  const validate = (): boolean => {
    const errors: ValidationErrors = {}

    if (!name.trim()) {
      errors.name = 'Server name is required'
    }

    if (!pkg.trim()) {
      errors.package = 'Package is required'
    }

    const cmd = command.trim()
    if (!cmd) {
      errors.command = 'Command is required'
    } else if (
      cmd !== 'npx' &&
      cmd !== 'docker' &&
      cmd !== 'uvx' &&
      !cmd.startsWith('/') &&
      !cmd.startsWith('./') &&
      !cmd.startsWith('../')
    ) {
      errors.command = 'Command must be "npx", "docker", "uvx", or a valid path (starts with /, ./, or ../)'
    }

    const missingVars = (server?.variables || []).filter((v) => v.required !== false && !varValues[v.key]?.trim())
    if (missingVars.length > 0) {
      errors.variables = `Required variables must have values: ${missingVars.map((v) => v.label || v.key).join(', ')}`
    }

    setValidationErrors(errors)
    return Object.keys(errors).length === 0
  }

  const saveServer = async (): Promise<string> => {
    const substituted = (str: string): string => {
      let result = str
      for (const [key, value] of Object.entries(varValues)) {
        result = result.replaceAll(`{{${key}}}`, value)
      }
      return result
    }

    if (editingServerId) {
      await api.mcp.updateMCPServer(editingServerId, {
        name: name.trim(),
        url: server?.url ?? 'http://localhost:3100',
        command: command.trim(),
        args: JSON.stringify((server?.args ?? []).map((a) => substituted(a))),
        transport,
        env: JSON.stringify(varValues),
        timeout_ms: timeout,
        description: server?.description,
      })
      return editingServerId
    }

    const result = await api.mcp.createMCPServer({
      name: name.trim(),
      url: server?.url ?? 'http://localhost:3100',
      command: command.trim(),
      args: JSON.stringify((server?.args ?? []).map((a) => substituted(a))),
      transport,
      env: JSON.stringify(varValues),
      timeout_ms: timeout,
      description: server?.description,
    })
    return result.id
  }

  const handleTestConnection = async () => {
    if (!editingServerId) return

    setTesting(true)
    setTestResult(null)
    setMatchedPattern(null)
    setView('form')

    try {
      const res = await api.mcp.testMCPServer(editingServerId)
      setTestResult(res)

      if (res.status === 'online') {
        toast.success('Sync successful', {
          description: `"${name.trim()}" is online with ${res.tools_count} tools.`,
        })
        return
      }

      // Test failed — match pattern and show remediation
      const pattern = matchError(res.error || '', res.stderr || '')
      setMatchedPattern(pattern)
      setView('result')
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setTestResult({ status: 'error', tools_count: 0, error: message })
      setView('result')
    } finally {
      setTesting(false)
    }
  }

  const handleSubmit = async () => {
    if (!validate()) return

    setSaving(true)
    setError(null)

    try {
      const id = await saveServer()
      setEditingServerId(id)

      toast.success('Server saved', { description: `"${name.trim()}" has been saved.` })

      onSuccess()
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err)
      setError(message)
      toast.error(editingServerId ? 'Update failed' : 'Installation failed', { description: message })
    } finally {
      setSaving(false)
    }
  }

  const handleRemove = async () => {
    if (!editingServerId) return
    try {
      await api.mcp.deleteMCPServer(editingServerId)
      toast.success('Server removed', { description: `"${name.trim()}" has been removed.` })
      onSuccess()
      onClose()
    } catch {
      toast.error('Failed to remove server', { description: 'Could not delete the server. Please try again.' })
    }
  }

  const handleEditAndRetry = () => {
    setView('form')
  }

  if (!open || !server) return null

  const renderForm = () => (
    <>
      <div className="flex items-center justify-between px-6 pt-6 pb-4 border-b border-surface-100">
        <h2 className="text-lg font-semibold text-surface-900">
          {editingServerId ? `Edit ${server.name}` : `Install ${server.name}`}
        </h2>
        <button
          type="button"
          onClick={onClose}
          className="rounded-lg p-1.5 text-surface-400 hover:bg-surface-100 hover:text-surface-600 transition-colors"
          aria-label="Close"
        >
          <XIcon />
        </button>
      </div>

      <div className="px-6 py-4 space-y-4">
        <div>
          <label className={labelClass}>
            Server Name <span className="text-error">*</span>
          </label>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="My MCP Server"
            className={validationErrors.name ? inputErrorClass : inputClass}
          />
          {validationErrors.name && <p className="mt-1 text-xs text-error">{validationErrors.name}</p>}
        </div>

        <div>
          <label className={labelClass}>
            Package <span className="text-error">*</span>
          </label>
          <input
            type="text"
            value={pkg}
            readOnly
            className="w-full rounded-lg border border-surface-200 bg-surface-50 px-3 py-2 text-sm text-surface-500 cursor-not-allowed"
          />
          <p className="mt-1 text-xs text-surface-400">Package name is determined by the server</p>
        </div>

        <div>
          <label className={labelClass}>Transport</label>
          <select value={transport} onChange={(e) => setTransport(e.target.value)} className={selectClass}>
            {TRANSPORT_OPTIONS.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </div>

        <div className="grid grid-cols-3 gap-3">
          <div className="col-span-2">
            <label className={labelClass}>
              Command <span className="text-error">*</span>
            </label>
            <input
              type="text"
              value={command}
              onChange={(e) => setCommand(e.target.value)}
              placeholder="npx"
              className={validationErrors.command ? inputErrorClass : inputClass}
            />
            {validationErrors.command && <p className="mt-1 text-xs text-error">{validationErrors.command}</p>}
          </div>
          <div>
            <label className={labelClass}>Timeout (ms)</label>
            <input
              type="number"
              value={timeout}
              onChange={(e) => setTimeout_(Number(e.target.value))}
              min={1000}
              step={1000}
              className={inputClass}
            />
          </div>
        </div>

        <div>
          <label className={labelClass}>Args</label>
          <input
            type="text"
            value={`${server.command} ${(server.args || []).join(' ')}`}
            readOnly
            className="w-full rounded-lg border border-surface-200 bg-surface-50 px-3 py-2 text-sm text-surface-500 cursor-not-allowed"
          />
        </div>

        <div>
          <label className={labelClass}>Variables</label>
          {(server.variables || []).length === 0 ? (
            <p className="text-xs text-surface-400 py-2">No variables required</p>
          ) : (
            <div className="space-y-3">
              {(server.variables || []).map((v) => (
                <div key={v.key}>
                  <label className="block text-xs font-medium text-surface-600 mb-1">
                    {v.label || v.key}
                    {v.required !== false && <span className="text-error ml-0.5">*</span>}
                    {v.secret && <span className="text-xs text-surface-400 ml-1">(secret)</span>}
                  </label>
                  <input
                    type={v.secret ? 'password' : 'text'}
                    value={varValues[v.key] || ''}
                    onChange={(e) => {
                      setVarValues((prev) => ({ ...prev, [v.key]: e.target.value }))
                      if (e.target.value.trim() && validationErrors.variables) {
                        setValidationErrors((prev) => ({ ...prev, envVars: undefined }))
                      }
                    }}
                    placeholder={v.default ? `default: ${v.default}` : `Enter ${v.label || v.key}`}
                    className={
                      validationErrors.variables && v.required !== false && !varValues[v.key]?.trim()
                        ? inputErrorClass
                        : inputClass
                    }
                  />
                  {v.description && <p className="mt-0.5 text-xs text-surface-400">{v.description}</p>}
                </div>
              ))}
            </div>
          )}
          {validationErrors.variables && <p className="mt-1 text-xs text-error">{validationErrors.variables}</p>}
        </div>

        {error && (
          <div className="rounded-lg bg-error/5 border border-error/20 p-3">
            <p className="text-sm text-error font-medium">Failed to install server</p>
            <p className="text-xs text-error/80 mt-0.5">{error}</p>
          </div>
        )}
      </div>

      <div className="flex items-center justify-end gap-2 px-6 pb-6 pt-4 border-t border-surface-100">
        <Button variant="outline" onClick={onClose} disabled={saving || testing}>
          {editingServerId ? 'Close' : 'Cancel'}
        </Button>
        {editingServerId && (
          <Button variant="outline" onClick={handleTestConnection} disabled={saving || testing}>
            {testing ? (
              <>
                <SpinnerIcon />
                Syncing...
              </>
            ) : (
              'Sync Connection'
            )}
          </Button>
        )}
        <Button onClick={handleSubmit} disabled={saving || testing}>
          {saving ? (
            <>
              <SpinnerIcon />
              {editingServerId ? 'Saving...' : 'Installing...'}
            </>
          ) : editingServerId ? (
            'Save Changes'
          ) : (
            'Add Server'
          )}
        </Button>
      </div>
    </>
  )

  const renderResult = () => (
    <>
      <div className="flex items-center justify-between px-6 pt-6 pb-4 border-b border-surface-100">
        <h2 className="text-lg font-semibold text-surface-900">Connection Issue</h2>
        <button
          type="button"
          onClick={onClose}
          className="rounded-lg p-1.5 text-surface-400 hover:bg-surface-100 hover:text-surface-600 transition-colors"
          aria-label="Close"
        >
          <XIcon />
        </button>
      </div>

      <div className="px-6 py-4 space-y-4">
        <div className="rounded-lg bg-error/5 border border-error/20 p-4">
          <p className="text-sm font-medium text-error">Sync failed for {server.name}</p>
          {testResult?.error && (
            <p className="text-xs text-error/80 mt-1.5 font-mono leading-relaxed">{testResult.error}</p>
          )}
          {testResult?.stderr && (
            <details className="mt-2">
              <summary className="text-xs text-surface-500 cursor-pointer hover:text-surface-700">
                View stderr output
              </summary>
              <pre className="mt-2 text-xs text-surface-600 bg-surface-50 rounded p-2 max-h-32 overflow-y-auto whitespace-pre-wrap leading-relaxed">
                {testResult.stderr}
              </pre>
            </details>
          )}
        </div>

        {/* Pattern-matched suggestion */}
        {matchedPattern ? (
          <div className="rounded-lg border border-surface-200 bg-surface-50 p-4">
            <p className="text-sm font-semibold text-surface-900">{matchedPattern.title}</p>
            <p className="text-xs text-surface-600 mt-1">{matchedPattern.description}</p>
            {matchedPattern.steps.length > 0 && (
              <div className="mt-3 space-y-2">
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wide">Suggested fixes:</p>
                {matchedPattern.steps.map((step, i) => (
                  <div key={i} className="flex items-start gap-2">
                    <span className="shrink-0 w-5 h-5 rounded-full bg-brand-100 text-brand-700 text-xs font-medium flex items-center justify-center mt-0.5">
                      {i + 1}
                    </span>
                    <p className="text-xs text-surface-700">{step.hint}</p>
                  </div>
                ))}
              </div>
            )}
          </div>
        ) : (
          <div className="rounded-lg border border-surface-200 bg-surface-50 p-4">
            <p className="text-sm font-semibold text-surface-900">Unknown error</p>
            <p className="text-xs text-surface-600 mt-1">
              No automatic fix suggestion available. Check the error details above and adjust the server configuration
              manually.
            </p>
          </div>
        )}
      </div>

      <div className="flex items-center justify-end gap-2 px-6 pb-6 pt-4 border-t border-surface-100">
        <Button variant="outline" onClick={onClose}>
          Close
        </Button>
        {editingServerId && (
          <Button variant="outline" onClick={handleRemove}>
            Remove Server
          </Button>
        )}
        <Button variant="outline" onClick={handleEditAndRetry}>
          Edit & Retry
        </Button>
        <Button onClick={handleTestConnection}>Retry Sync</Button>
      </div>
    </>
  )

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 backdrop-blur-sm p-4"
      onClick={onClose}
    >
      <div
        className="relative w-full max-w-lg rounded-xl bg-white shadow-xl max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        {view === 'result' ? renderResult() : renderForm()}
      </div>
    </div>
  )
}
