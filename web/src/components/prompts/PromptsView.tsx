import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { api, type PromptTemplate } from '../../lib/api'
import { cn } from '../../lib/utils'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { EmptyState } from '../ui/empty-state'
import { FilterBar } from '../ui/FilterBar'
import { Box, Download, Edit3, Info, Plus, Trash2, X } from '../ui/icons'
import { Skeleton } from '../ui/skeleton'
import { useExport } from '../ui/useExport'

function PromptHelpPanel({ onClose }: { onClose: () => void }) {
  return (
    <>
      <div className="fixed inset-0 z-50 bg-black/30 backdrop-blur-sm" onClick={onClose} />
      <div className="fixed right-0 top-0 z-50 flex h-full w-[480px] max-w-full flex-col border-l border-surface-200 bg-white shadow-2xl">
        <div className="flex items-center justify-between border-b border-surface-200 px-6 py-5">
          <h3 className="text-lg font-semibold text-surface-900">How Prompt Injection Works</h3>
          <Button variant="ghost" size="icon-sm" onClick={onClose} aria-label="Close">
            <X size={16} />
          </Button>
        </div>

        <div className="flex-1 overflow-y-auto px-6 py-6">
          <div className="space-y-6 text-sm text-surface-600">
            <p className="leading-relaxed">
              Prompt Injection lets you attach a system prompt to every request automatically — without the client
              needing to send it. Define templates in the dashboard, then reference them by name in your API calls.
            </p>

            <div className="rounded-lg bg-surface-50 border border-surface-200 p-4 space-y-3">
              <p className="font-medium text-surface-800">Usage</p>

              <div>
                <p className="text-xs font-medium text-surface-500 uppercase tracking-wider mb-1">
                  Header: X-Prompt-Name
                </p>
                <code className="block text-xs font-mono bg-white rounded px-3 py-1.5 text-surface-700 border border-surface-200">
                  X-Prompt-Name: customer-support
                </code>
              </div>
            </div>

            <p className="text-xs text-surface-400">
              The rendered system prompt is prepended to your messages automatically. Templates use Go's text/template
              syntax — reference variables as <code className="font-mono">{'{{.variableName}}'}</code>.
            </p>
          </div>
        </div>
      </div>
    </>
  )
}

function extractVariables(content: string): string[] {
  const matches = content.match(/\{\{\s*\.(\w+)\s*\}\}/g)
  if (!matches) return []
  return [...new Set(matches.map((m) => m.replace(/\{\{\s*\.(\w+)\s*\}\}/, '$1')))]
}

function VariableChip({ name, onRemove }: { name: string; onRemove?: () => void }) {
  return (
    <span className="inline-flex items-center gap-1 rounded-md bg-brand-50 px-2 py-1 text-xs font-medium text-brand-700 border border-brand-200">
      <Box size={12} />
      {`{{.${name}}}`}
      {onRemove && (
        <button onClick={onRemove} className="text-brand-400 hover:text-brand-700 ml-0.5">
          <X size={12} />
        </button>
      )}
    </span>
  )
}

function TemplateFormModal({
  open,
  onClose,
  onSave,
  initial,
}: {
  open: boolean
  onClose: () => void
  onSave: (data: { name: string; content: string; variables: string[] }) => void
  initial?: PromptTemplate | null
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [content, setContent] = useState(initial?.content ?? '')
  const [variables, setVariables] = useState<string[]>(initial?.variables ?? [])
  const [newVar, setNewVar] = useState('')

  const detectedVars = useMemo(() => extractVariables(content), [content])

  const addVariable = () => {
    const trimmed = newVar.trim()
    if (trimmed && !variables.includes(trimmed)) {
      setVariables([...variables, trimmed])
      setNewVar('')
    }
  }

  const removeVariable = (v: string) => {
    setVariables(variables.filter((x) => x !== v))
  }

  if (!open) return null

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <Card className="w-full max-w-2xl mx-4 max-h-[85vh] overflow-y-auto">
        <CardHeader>
          <CardTitle>{initial ? 'Edit Template' : 'Create Template'}</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="space-y-4">
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">Template Name</label>
              <input
                type="text"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="e.g., Customer Support Response"
                className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              />
            </div>
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-1">Content</label>
              <textarea
                value={content}
                onChange={(e) => setContent(e.target.value)}
                placeholder="Use {{.variable}} syntax (Go template) for dynamic values..."
                rows={12}
                className="w-full rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm font-mono text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500 resize-y"
              />
              {detectedVars.length > 0 && (
                <p className="text-xs text-surface-400 mt-1">Detected variables: {detectedVars.join(', ')}</p>
              )}
            </div>
            <div>
              <label className="block text-sm font-medium text-surface-700 mb-2">Variables</label>
              <div className="flex flex-wrap gap-2 mb-2">
                {variables.map((v) => (
                  <VariableChip key={v} name={v} onRemove={() => removeVariable(v)} />
                ))}
                {variables.length === 0 && <span className="text-xs text-surface-400">No variables defined</span>}
              </div>
              <div className="flex gap-2">
                <input
                  type="text"
                  value={newVar}
                  onChange={(e) => setNewVar(e.target.value)}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault()
                      addVariable()
                    }
                  }}
                  placeholder="Add variable name..."
                  className="flex-1 rounded-lg border border-surface-300 bg-white px-3 py-2 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                />
                <Button variant="outline" size="sm" onClick={addVariable} disabled={!newVar.trim()}>
                  Add
                </Button>
              </div>
            </div>
            <div className="flex justify-end gap-3 pt-2">
              <Button variant="outline" onClick={onClose}>
                Cancel
              </Button>
              <Button
                onClick={() => {
                  onSave({ name, content, variables })
                  onClose()
                }}
                disabled={!name || !content}
              >
                {initial ? 'Save Changes' : 'Create Template'}
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}

export function PromptsView() {
  const [templates, setTemplates] = useState<PromptTemplate[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [showCreateModal, setShowCreateModal] = useState(false)
  const [editTemplateId, setEditTemplateId] = useState<string | null>(null)
  const [search, setSearch] = useState('')
  const [previewMode, setPreviewMode] = useState(false)
  const [previewValues, setPreviewValues] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [showHowItWorks, setShowHowItWorks] = useState(false)
  const { exportCsv } = useExport()

  useEffect(() => {
    api.prompts
      .getPromptTemplates()
      .then((data) => setTemplates(Array.isArray(data) ? data : []))
      .catch(() => toast.error('Failed to load templates'))
      .finally(() => setLoading(false))
  }, [])

  const selected = templates.find((t) => t.id === selectedId) ?? null

  const filtered = search ? templates.filter((t) => t.name.toLowerCase().includes(search.toLowerCase())) : templates

  const createTemplate = (data: { name: string; content: string; variables: string[] }) => {
    const now = new Date().toISOString()
    api.prompts
      .createPromptTemplate(data)
      .then((res) => {
        const newTpl: PromptTemplate = {
          id: res.id || String(Date.now()),
          name: data.name,
          content: data.content,
          variables: data.variables,
          labels: [],
          description: '',
          version: '1.0',
          created_at: now,
          updated_at: now,
        }
        setTemplates((prev) => [...prev, newTpl])
        setSelectedId(newTpl.id)
        toast.success('Template created')
      })
      .catch(() => toast.error('Failed to create template'))
  }

  const updateTemplate = (data: { name: string; content: string; variables: string[] }) => {
    if (!selectedId) return
    const prev = templates
    setTemplates(
      templates.map((t) =>
        t.id === selectedId
          ? {
              ...t,
              name: data.name,
              content: data.content,
              variables: data.variables,
              updated_at: new Date().toISOString(),
            }
          : t,
      ),
    )
    setEditTemplateId(null)
    api.prompts
      .updatePromptTemplate(selectedId, data)
      .then(() => toast.success('Template updated'))
      .catch(() => {
        setTemplates(prev)
        toast.error('Failed to update template')
      })
  }

  const deleteTemplate = (id: string) => {
    const prev = templates
    setTemplates(templates.filter((t) => t.id !== id))
    if (selectedId === id) setSelectedId(null)
    api.prompts
      .deletePromptTemplate(id)
      .then(() => toast.success('Template deleted'))
      .catch(() => {
        setTemplates(prev)
        toast.error('Failed to delete template')
      })
  }

  const renderPreview = (content: string, vals: Record<string, string>): string => {
    let result = content
    for (const [key, value] of Object.entries(vals)) {
      result = result.replace(new RegExp(`\\{\\{\\s*\\.${key}\\s*\\}\\}`, 'g'), value || `{{.${key}}}`)
    }
    return result
  }

  return (
    <div className="space-y-6">
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div className="flex items-center gap-3">
            <h2 className="text-lg font-semibold text-surface-900">Prompts</h2>
            <Button variant="outline" size="sm" onClick={() => setShowHowItWorks(true)}>
              <Info size={14} />
              How It Works
            </Button>
          </div>
          <div className="flex items-center gap-2">
            {templates.length > 0 && (
              <Button
                variant="outline"
                size="sm"
                onClick={() =>
                  exportCsv(
                    templates.map((t) => ({
                      Name: t.name,
                      Variables: t.variables.join(', '),
                      Created: new Date(t.created_at).toLocaleDateString(),
                      Updated: new Date(t.updated_at).toLocaleDateString(),
                    })),
                    [
                      { key: 'Name' as const, header: 'Name' },
                      { key: 'Variables' as const, header: 'Variables' },
                      { key: 'Created' as const, header: 'Created' },
                      { key: 'Updated' as const, header: 'Updated' },
                    ],
                    'templates.csv',
                  )
                }
              >
                <Download size={14} />
                Export
              </Button>
            )}
            <Button onClick={() => setShowCreateModal(true)}>
              <Plus size={16} />
              Create Template
            </Button>
          </div>
        </div>

        <FilterBar searchPlaceholder="Search templates..." searchValue={search} onSearchChange={setSearch} />

        <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
          <div className="lg:col-span-1 space-y-2">
            {loading ? (
              Array.from({ length: 4 }).map((_, i) => <Skeleton key={i} className="h-20 rounded-xl" />)
            ) : filtered.length === 0 ? (
              <EmptyState
                title="No templates found"
                description={
                  search ? 'Try a different search term.' : 'Create your first prompt template to get started.'
                }
              />
            ) : (
              filtered.map((tpl) => (
                <button
                  key={tpl.id}
                  onClick={() => setSelectedId(tpl.id)}
                  className={cn(
                    'w-full text-left rounded-xl border p-4 transition-all',
                    selectedId === tpl.id
                      ? 'border-brand-300 bg-brand-50/50 shadow-sm'
                      : 'border-surface-200 bg-white hover:border-surface-300 hover:shadow-sm',
                  )}
                >
                  <p className="text-sm font-medium text-surface-900 truncate">{tpl.name}</p>
                  <div className="flex items-center gap-2 mt-1.5">
                    <span className="text-xs text-surface-400">{tpl.variables.length} variables</span>
                    <span className="text-xs text-surface-300">·</span>
                    <span className="text-xs text-surface-400">
                      Updated {new Date(tpl.updated_at).toLocaleDateString()}
                    </span>
                  </div>
                </button>
              ))
            )}
          </div>

          <div className="lg:col-span-2">
            {!selected ? (
              <EmptyState
                title="Select a template"
                description="Choose a template from the list to view and edit its content."
                className="py-20"
              />
            ) : editTemplateId === selected.id ? (
              <TemplateFormModal
                open={true}
                onClose={() => setEditTemplateId(null)}
                initial={selected}
                onSave={updateTemplate}
              />
            ) : (
              <Card>
                <CardHeader>
                  <div className="flex items-center justify-between">
                    <CardTitle>{selected.name}</CardTitle>
                    <div className="flex items-center gap-2">
                      <Button variant="outline" size="sm" onClick={() => setPreviewMode(!previewMode)}>
                        {previewMode ? 'Exit Preview' : 'Preview'}
                      </Button>
                      <Button variant="outline" size="sm" onClick={() => setEditTemplateId(selected.id)}>
                        <Edit3 size={14} />
                        Edit
                      </Button>
                      <Button
                        variant="destructive"
                        size="sm"
                        onClick={() => {
                          if (
                            window.confirm(
                              'Are you sure you want to delete this template? This action cannot be undone.',
                            )
                          ) {
                            deleteTemplate(selected.id)
                          }
                        }}
                      >
                        <Trash2 size={14} />
                      </Button>
                    </div>
                  </div>
                </CardHeader>
                <CardContent>
                  {previewMode ? (
                    <div>
                      {selected.variables.length > 0 ? (
                        <div className="mb-4 space-y-2">
                          {selected.variables.map((v) => (
                            <div key={v} className="flex items-center gap-2">
                              <label className="text-xs font-medium text-surface-500 w-32">{`{{.${v}}}`}</label>
                              <input
                                type="text"
                                placeholder={`Enter ${v}...`}
                                value={previewValues[v] ?? ''}
                                onChange={(e) => setPreviewValues({ ...previewValues, [v]: e.target.value })}
                                className="flex-1 rounded-lg border border-surface-300 bg-white px-3 py-1.5 text-sm text-surface-900 placeholder-surface-400 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
                              />
                            </div>
                          ))}
                        </div>
                      ) : (
                        <p className="mb-4 text-xs text-surface-400">
                          This template has no variables, so the preview matches the raw content below.
                        </p>
                      )}
                      <p className="text-xs font-medium text-surface-500 mb-2 uppercase tracking-wider">
                        Rendered Output
                      </p>
                      <div className="rounded-xl border border-brand-200 bg-brand-50/30 p-4">
                        <pre className="whitespace-pre-wrap text-sm text-surface-700 font-mono">
                          {renderPreview(selected.content, previewValues)}
                        </pre>
                      </div>
                    </div>
                  ) : (
                    <div>
                      <pre className="whitespace-pre-wrap text-sm text-surface-700 font-mono bg-surface-50 rounded-xl border border-surface-200 p-4 mb-4">
                        {selected.content}
                      </pre>
                      <div>
                        <p className="text-xs font-medium text-surface-500 mb-2 uppercase tracking-wider">Variables</p>
                        <div className="flex flex-wrap gap-2">
                          {selected.variables.map((v) => (
                            <VariableChip key={v} name={v} />
                          ))}
                          {selected.variables.length === 0 && (
                            <span className="text-xs text-surface-400">No variables in this template</span>
                          )}
                        </div>
                      </div>
                      <div className="mt-4 flex items-center gap-4 text-xs text-surface-400 border-t border-surface-100 pt-4">
                        <span>Created: {new Date(selected.created_at).toLocaleDateString()}</span>
                        <span>Updated: {new Date(selected.updated_at).toLocaleDateString()}</span>
                      </div>
                    </div>
                  )}
                </CardContent>
              </Card>
            )}
          </div>
        </div>
      </div>

      <TemplateFormModal open={showCreateModal} onClose={() => setShowCreateModal(false)} onSave={createTemplate} />
      {showHowItWorks && <PromptHelpPanel onClose={() => setShowHowItWorks(false)} />}
    </div>
  )
}
