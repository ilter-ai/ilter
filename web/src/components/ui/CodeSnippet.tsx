import { useState } from 'react'
import { cn } from '@/lib/utils'
import { Check, Copy } from './icons'

// Shared "copy-paste snippet" building block used by every "How To"-style
// panel (Providers env setup, MCP connect guide, Jobs webhook invoke guide,
// ...) so they stay visually and behaviorally consistent.

export function CodeSnippet({ code, label, className }: { code: string; label?: string; className?: string }) {
  const [copied, setCopied] = useState(false)

  const handleCopy = () => {
    navigator.clipboard.writeText(code)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }

  return (
    <div className={cn('space-y-1 min-w-0 w-full', className)}>
      {label && (
        <div className="flex items-center justify-between text-[11px] font-medium text-surface-500">
          <span>{label}</span>
          <button
            type="button"
            onClick={handleCopy}
            className="inline-flex items-center gap-1 text-surface-400 hover:text-brand-600 transition-colors"
          >
            {copied ? (
              <>
                <Check size={12} className="text-emerald-500" />
                <span className="text-emerald-600 text-[10px]">Copied!</span>
              </>
            ) : (
              <>
                <Copy size={12} />
                <span className="text-[10px]">Copy</span>
              </>
            )}
          </button>
        </div>
      )}
      <div className="relative min-w-0 w-full overflow-hidden rounded-lg bg-surface-950 border border-surface-800">
        {!label && (
          <button
            type="button"
            onClick={handleCopy}
            className="absolute right-2 top-2 z-10 inline-flex items-center gap-1 rounded-md bg-surface-900/80 px-1.5 py-1 text-surface-400 hover:text-brand-400 transition-colors"
          >
            {copied ? <Check size={12} className="text-emerald-500" /> : <Copy size={12} />}
          </button>
        )}
        <pre className="overflow-x-auto p-2.5 font-mono text-xs text-emerald-400 whitespace-pre scrollbar-thin">
          <code>{code}</code>
        </pre>
      </div>
    </div>
  )
}

// A CodeSnippet with a bash/PowerShell switcher above it — the standard shape
// for "here's how to call this from your terminal" instructions regardless
// of the caller's OS.
export function OSTabbedCodeSnippet({ bash, powershell, label }: { bash: string; powershell: string; label?: string }) {
  const [osTab, setOsTab] = useState<'bash' | 'powershell'>('bash')

  return (
    <div className="space-y-2">
      <div className="flex items-center gap-2 border-b border-surface-200 pb-2">
        <button
          type="button"
          onClick={() => setOsTab('bash')}
          className={cn(
            'text-xs font-medium px-2 py-1 rounded transition-colors',
            osTab === 'bash'
              ? 'bg-surface-200 text-surface-900 font-semibold'
              : 'text-surface-500 hover:text-surface-900',
          )}
        >
          macOS / Linux
        </button>
        <button
          type="button"
          onClick={() => setOsTab('powershell')}
          className={cn(
            'text-xs font-medium px-2 py-1 rounded transition-colors',
            osTab === 'powershell'
              ? 'bg-surface-200 text-surface-900 font-semibold'
              : 'text-surface-500 hover:text-surface-900',
          )}
        >
          Windows (PowerShell)
        </button>
      </div>
      <CodeSnippet code={osTab === 'bash' ? bash : powershell} label={label} />
    </div>
  )
}
