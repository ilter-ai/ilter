import { cn } from '../../lib/utils'
import type { MCPServerEntry } from '../../types/mcp'
import { Button } from '../ui/button'
import { Container, ExternalLink, FileText, Star } from '../ui/icons'

interface MarketplaceCardProps {
  server: MCPServerEntry
  viewMode: 'grid' | 'list'
  onInstall: (server: MCPServerEntry) => void
  installedId?: string
  onUninstall?: (id: string) => void
}

const CATEGORY_COLORS: Record<string, string> = {
  'Developer Tools': 'bg-indigo-50 text-indigo-700 border-indigo-200',
  'Browser Automation': 'bg-amber-50 text-amber-700 border-amber-200',
  Design: 'bg-pink-50 text-pink-700 border-pink-200',
  'AI & ML': 'bg-violet-50 text-violet-700 border-violet-200',
  Search: 'bg-cyan-50 text-cyan-700 border-cyan-200',
  Database: 'bg-emerald-50 text-emerald-700 border-emerald-200',
  Communication: 'bg-orange-50 text-orange-700 border-orange-200',
  Productivity: 'bg-teal-50 text-teal-700 border-teal-200',
  'Cloud & Infra': 'bg-blue-50 text-blue-700 border-blue-200',
  Finance: 'bg-green-50 text-green-700 border-green-200',
  'Data & Analytics': 'bg-rose-50 text-rose-700 border-rose-200',
  Security: 'bg-red-50 text-red-700 border-red-200',
}

function getCategoryColor(category: string): string {
  return CATEGORY_COLORS[category] || 'bg-surface-50 text-surface-600 border-surface-200'
}

function RuntimeBadge({ command }: { command: string }) {
  if (command === 'docker') {
    return (
      <span className="inline-flex items-center gap-1 rounded-md bg-blue-50 px-1.5 py-0.5 text-[10px] font-medium text-blue-700 border border-blue-200">
        <Container size={10} fill="currentColor" />
        Docker
      </span>
    )
  }
  if (command === 'npx' || !command) {
    return null
  }
  return (
    <span className="inline-flex items-center gap-1 rounded-md bg-surface-100 px-1.5 py-0.5 text-[10px] font-medium text-surface-600 border border-surface-200">
      {command}
    </span>
  )
}

function StarIcon() {
  return <Star size={14} className="text-amber-400" fill="currentColor" />
}

function ExternalLinkIcon() {
  return <ExternalLink size={14} />
}

function formatStars(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(n)
}

export function MarketplaceCard({ server, viewMode, onInstall, installedId, onUninstall }: MarketplaceCardProps) {
  if (viewMode === 'list') {
    return (
      <div className="flex items-center gap-4 rounded-xl border border-surface-200 bg-white px-4 py-3 shadow-sm hover:shadow-md transition-shadow">
        <div className="flex items-center gap-2 shrink-0">
          {server.stars > 0 && (
            <span className="inline-flex items-center gap-1 text-xs text-surface-400">
              <StarIcon />
              {formatStars(server.stars)}
            </span>
          )}
          <span
            className={cn(
              'rounded-full border px-2.5 py-0.5 text-[11px] font-medium',
              getCategoryColor(server.category),
            )}
          >
            {server.category}
          </span>
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-1.5">
            <span className="text-sm font-semibold text-surface-900 truncate">{server.name}</span>
            {server.url && (
              <a
                href={server.url}
                target="_blank"
                rel="noopener noreferrer"
                className="shrink-0 text-surface-400 hover:text-surface-600 transition-colors"
                title="View on GitHub"
              >
                <ExternalLinkIcon />
              </a>
            )}
          </div>
          <p className="text-xs text-surface-500 truncate mt-0.5">{server.description}</p>
        </div>

        <div className="hidden sm:flex items-center gap-2 text-xs shrink-0 max-w-[200px]">
          {server.tools.length > 0 && (
            <span className="inline-flex items-center gap-1 truncate" title={server.tools.join(', ')}>
              <code className="rounded bg-surface-100 px-1 py-0.5 text-[11px] text-surface-500 border border-surface-200 truncate max-w-[100px]">
                {server.tools[0]}
              </code>
              {server.tools.length > 1 && <span className="text-surface-400 shrink-0">+{server.tools.length - 1}</span>}
            </span>
          )}
          {server.variables.length > 0 && (
            <span className="inline-flex items-center gap-1 text-surface-400 shrink-0">
              <FileText size={14} />
              {server.variables.length}
            </span>
          )}
        </div>

        <div className="flex items-center gap-1 shrink-0">
          <RuntimeBadge command={server.command} />
          {installedId ? (
            <Button
              size="sm"
              variant="outline"
              className="text-rose-600 border-rose-300 hover:bg-rose-50"
              onClick={() => onUninstall?.(installedId)}
            >
              Uninstall
            </Button>
          ) : (
            <Button size="sm" onClick={() => onInstall(server)}>
              Install
            </Button>
          )}
        </div>
      </div>
    )
  }

  return (
    <div className="group rounded-xl border border-surface-200 bg-white shadow-sm hover:shadow-md transition-shadow flex flex-col">
      <div className="flex flex-col flex-1 p-5">
        <div className="flex items-center gap-2 mb-3">
          {server.stars > 0 && (
            <span className="inline-flex items-center gap-1 text-xs text-surface-400 shrink-0">
              <StarIcon />
              {formatStars(server.stars)}
            </span>
          )}
          <span
            className={cn(
              'rounded-full border px-2.5 py-0.5 text-[11px] font-medium',
              getCategoryColor(server.category),
            )}
          >
            {server.category}
          </span>
        </div>

        <h3 className="text-base font-semibold text-surface-900 leading-tight mb-1.5 flex items-center gap-1.5">
          {server.name}
          {server.url && (
            <a
              href={server.url}
              target="_blank"
              rel="noopener noreferrer"
              className="shrink-0 text-surface-400 hover:text-surface-600 transition-colors"
              title="View on GitHub"
            >
              <ExternalLinkIcon />
            </a>
          )}
        </h3>

        <p className="text-xs text-surface-500 leading-relaxed line-clamp-2 mb-3 flex-1">{server.description}</p>

        {server.tools.length > 0 && (
          <div className="flex items-center gap-1.5 flex-wrap text-xs pt-1 mb-1">
            {server.tools.slice(0, 3).map((t) => (
              <code
                key={t}
                className="rounded bg-surface-100 px-1.5 py-0.5 text-[11px] text-surface-500 border border-surface-200 leading-normal"
              >
                {t}
              </code>
            ))}
            {server.tools.length > 3 && (
              <span className="text-[11px] text-surface-400">+{server.tools.length - 3} more</span>
            )}
          </div>
        )}
        {server.variables.length > 0 && (
          <div className="flex items-center gap-1 text-xs text-surface-400">
            <FileText size={14} />
            {server.variables.length} var{server.variables.length > 1 ? 's' : ''}
          </div>
        )}
      </div>

      {/* Card footer — no divider */}
      <div className="flex items-center justify-between px-5 pb-4">
        <RuntimeBadge command={server.command} />
        {installedId ? (
          <Button
            size="sm"
            variant="outline"
            className="text-rose-600 border-rose-300 hover:bg-rose-50"
            onClick={() => onUninstall?.(installedId)}
          >
            Uninstall
          </Button>
        ) : (
          <Button size="sm" onClick={() => onInstall(server)}>
            Install
          </Button>
        )}
      </div>
    </div>
  )
}
