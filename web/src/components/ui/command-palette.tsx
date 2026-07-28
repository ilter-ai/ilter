import { useEffect, useState } from 'react'
import { CommandDialog, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from './command'

interface PageEntry {
  id: string
  label: string
  href: string
  category: string
}

const PAGES: PageEntry[] = [
  { id: 'overview', label: 'Overview', href: '/overview', category: 'Overview' },
  { id: 'chat', label: 'Chat', href: '/chat', category: 'Overview' },
  { id: 'budget', label: 'Budget & Costs', href: '/features/budget', category: 'Cost & Budget' },
  { id: 'providers', label: 'Providers', href: '/llm/providers', category: 'LLMs' },
  { id: 'models', label: 'Models', href: '/llm/models', category: 'LLMs' },
  { id: 'smart-router', label: 'Smart Router', href: '/features/smart-router', category: 'LLMs' },
  { id: 'pii-protection', label: 'PII Protection', href: '/features/pii-protection', category: 'Governance' },
  { id: 'guardrails', label: 'Guardrails', href: '/features/guardrails', category: 'Governance' },
  { id: 'loop-detection', label: 'Loop Detection', href: '/features/loop-detection', category: 'Governance' },
  { id: 'access-keys', label: 'API Keys', href: '/access/keys', category: 'Access' },
  { id: 'access-users', label: 'Users', href: '/access/users', category: 'Access' },
  { id: 'access-groups', label: 'Groups', href: '/access/groups', category: 'Access' },
  { id: 'access-mcp', label: 'Permissions', href: '/mcp/permissions', category: 'MCP' },
  { id: 'mcp', label: 'MCP', href: '/mcp/servers', category: 'Overview' },
  { id: 'prompts', label: 'Prompts', href: '/llm/prompts', category: 'LLMs' },
  { id: 'features', label: 'Features', href: '/features/budget', category: 'Settings' },
]

interface CommandPaletteProps {
  onNavigate?: (href: string) => void
}

export function CommandPalette({ onNavigate }: CommandPaletteProps) {
  const [open, setOpen] = useState(false)

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
        e.preventDefault()
        setOpen((prev) => !prev)
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [])

  useEffect(() => {
    const btn = document.getElementById('cmdkBtn')
    if (!btn) return
    const handler = () => setOpen((prev) => !prev)
    btn.addEventListener('click', handler)
    return () => btn.removeEventListener('click', handler)
  }, [])

  const navigate = (href: string) => {
    setOpen(false)
    if (onNavigate) {
      onNavigate(href)
    } else {
      window.location.href = href
    }
  }

  const grouped = PAGES.reduce<Record<string, PageEntry[]>>((acc, page) => {
    ;(acc[page.category] ??= []).push(page)
    return acc
  }, {})

  return (
    <CommandDialog open={open} onOpenChange={setOpen}>
      <CommandInput placeholder="Search pages..." />
      <CommandList>
        <CommandEmpty>No results found.</CommandEmpty>
        {Object.entries(grouped).map(([category, pages]) => (
          <CommandGroup key={category} heading={category}>
            {pages.map((page) => (
              <CommandItem
                key={page.id}
                value={`${page.category} ${page.label} ${page.id}`}
                onSelect={() => navigate(page.href)}
              >
                {page.label}
              </CommandItem>
            ))}
          </CommandGroup>
        ))}
      </CommandList>
    </CommandDialog>
  )
}
