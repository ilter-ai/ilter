import { ConfigAuditView } from '../audit/ConfigAuditView'
import { McpAuditView } from '../mcp-audit/McpAuditView'
import { OpenApiAuditView } from '../mcp-audit/OpenApiAuditView'
import { RequestsView } from '../requests/RequestsView'

type Tab = 'requests' | 'mcp' | 'openapi' | 'admin'

const tabLabels: Record<Tab, string> = {
  requests: 'Requests',
  mcp: 'MCP',
  openapi: 'OpenAPI',
  admin: 'Admin Activity',
}

const TABS: Tab[] = ['requests', 'mcp', 'openapi', 'admin']

export function LogsView({ initialTab }: { initialTab?: Tab }) {
  const tab = initialTab ?? 'requests'

  return (
    <div>
      <div role="tablist" className="flex gap-1 border-b border-surface-200 mb-6">
        {TABS.map((t) => (
          <a
            key={t}
            role="tab"
            aria-selected={tab === t}
            href={`/logs/${t}`}
            className={`px-4 py-2.5 text-sm font-medium border-b-2 transition-colors ${
              tab === t
                ? 'border-brand-600 text-brand-700'
                : 'border-transparent text-surface-500 hover:text-surface-700 hover:border-surface-300'
            }`}
          >
            {tabLabels[t]}
          </a>
        ))}
      </div>
      {tab === 'requests' ? (
        <RequestsView />
      ) : tab === 'mcp' ? (
        <McpAuditView />
      ) : tab === 'openapi' ? (
        <OpenApiAuditView />
      ) : (
        <ConfigAuditView />
      )}
    </div>
  )
}
