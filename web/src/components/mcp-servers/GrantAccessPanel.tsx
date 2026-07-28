import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { api, type McpGrant } from '../../lib/api'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { Plus, Trash2, X } from '../ui/icons'
import { Input } from '../ui/input'
import { Switch } from '../ui/switch'

interface GrantAccessPanelProps {
  serverId: string
  serverName: string
  onClose: () => void
}

export function GrantAccessPanel({ serverId, serverName, onClose }: GrantAccessPanelProps) {
  const [grants, setGrants] = useState<McpGrant[]>([])
  const [loading, setLoading] = useState(true)
  const [subjectType, setSubjectType] = useState<'key' | 'user' | 'group'>('key')
  const [subjectId, setSubjectId] = useState('')
  const [tools, setTools] = useState('*')
  const [effect, setEffect] = useState<'allow' | 'deny'>('allow')
  const [enabled, setEnabled] = useState(true)
  const [priority, setPriority] = useState(0)

  useEffect(() => {
    setLoading(true)
    api.access
      .listGrantsByServer(serverId)
      .then(setGrants)
      .catch(() => toast.error('Failed to load grants'))
      .finally(() => setLoading(false))
  }, [serverId])

  const fetchGrants = () => {
    api.access
      .listGrantsByServer(serverId)
      .then(setGrants)
      .catch(() => toast.error('Failed to load grants'))
  }

  const handleAdd = () => {
    if (!subjectId.trim()) {
      toast.error('Subject ID is required')
      return
    }
    api.access
      .createGrant({
        subject_type: subjectType,
        subject_id: subjectId.trim(),
        server_id: serverId,
        tools: tools.trim() || '*',
        effect,
        enabled,
        priority,
      })
      .then(() => {
        toast.success('Grant added')
        setSubjectId('')
        setTools('*')
        setEffect('allow')
        setEnabled(true)
        setPriority(0)
        fetchGrants()
      })
      .catch(() => toast.error('Failed to add grant'))
  }

  const handleDelete = (grantId: string) => {
    if (!window.confirm('Remove this access grant?')) return
    api.access
      .deleteGrant(grantId)
      .then(() => {
        toast.success('Grant removed')
        fetchGrants()
      })
      .catch(() => toast.error('Failed to remove grant'))
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40" onClick={onClose}>
      <Card className="w-full max-w-lg mx-4 max-h-[80vh] flex flex-col" onClick={(e) => e.stopPropagation()}>
        <CardHeader>
          <div className="flex items-center justify-between">
            <CardTitle>Access Grants — {serverName}</CardTitle>
            <button onClick={onClose} className="text-surface-400 hover:text-surface-600 transition-colors">
              <X size={18} />
            </button>
          </div>
        </CardHeader>
        <CardContent className="overflow-y-auto space-y-4">
          {/* Existing grants */}
          <div>
            <h4 className="text-sm font-medium text-surface-700 mb-2">Current Grants</h4>
            {loading ? (
              <p className="text-xs text-surface-400">Loading...</p>
            ) : grants.length === 0 ? (
              <p className="text-xs text-surface-400">No grants configured. Add one below.</p>
            ) : (
              <div className="space-y-2">
                {grants.map((g) => (
                  <div
                    key={g.id}
                    className="flex items-center gap-2 rounded-lg border border-surface-200 bg-surface-50 px-3 py-2 text-xs"
                  >
                    <span
                      className={`
                      inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium uppercase tracking-wider
                      ${g.subject_type === 'key' ? 'bg-blue-100 text-blue-700' : ''}
                      ${g.subject_type === 'user' ? 'bg-green-100 text-green-700' : ''}
                      ${g.subject_type === 'group' ? 'bg-purple-100 text-purple-700' : ''}
                    `}
                    >
                      {g.subject_type}
                    </span>
                    <code className="font-mono text-surface-700 flex-1 truncate">{g.subject_id}</code>
                    <span
                      className={`
                      inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-semibold
                      ${g.effect === 'allow' ? 'bg-green-100 text-green-700' : 'bg-red-100 text-red-700'}
                    `}
                    >
                      {g.effect}
                    </span>
                    <span className="text-surface-400">tools:</span>
                    <code className="font-mono text-surface-600">{g.tools}</code>
                    <span
                      className={`
                      inline-flex items-center rounded px-1.5 py-0.5 text-[10px] font-medium
                      ${g.enabled ? 'bg-green-100 text-green-700' : 'bg-surface-100 text-surface-500'}
                    `}
                    >
                      {g.enabled ? 'on' : 'off'}
                    </span>
                    <span className="text-surface-400">p:{g.priority}</span>
                    <button
                      onClick={() => handleDelete(g.id)}
                      className="text-surface-400 hover:text-destructive transition-colors shrink-0"
                      title="Remove grant"
                    >
                      <Trash2 size={14} />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* Add grant form */}
          <div className="border-t border-surface-200 pt-4">
            <h4 className="text-sm font-medium text-surface-700 mb-2">Add Grant</h4>
            <div className="flex flex-wrap gap-2">
              <select
                value={subjectType}
                onChange={(e) => setSubjectType(e.target.value as 'key' | 'user' | 'group')}
                className="rounded-lg border border-surface-300 bg-white px-2.5 py-1.5 text-xs text-surface-900 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              >
                <option value="key">Key</option>
                <option value="user">User</option>
                <option value="group">Group</option>
              </select>
              <Input
                type="text"
                value={subjectId}
                onChange={(e) => setSubjectId(e.target.value)}
                placeholder="Subject ID"
                className="flex-1 min-w-[120px]"
              />
              <select
                value={effect}
                onChange={(e) => setEffect(e.target.value as 'allow' | 'deny')}
                className="rounded-lg border border-surface-300 bg-white px-2.5 py-1.5 text-xs font-semibold focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500"
              >
                <option value="allow" className="text-green-700">
                  Allow
                </option>
                <option value="deny" className="text-red-700">
                  Deny
                </option>
              </select>
              <Input
                type="text"
                value={tools}
                onChange={(e) => setTools(e.target.value)}
                placeholder="tools (default: *)"
                className="w-28 font-mono"
              />
              <div className="flex items-center gap-1.5 text-xs text-surface-600">
                <Switch size="sm" checked={enabled} onCheckedChange={setEnabled} />
                <span>Enabled</span>
              </div>
              <div className="flex items-center gap-1">
                <Input
                  type="number"
                  value={priority}
                  onChange={(e) => setPriority(parseInt(e.target.value, 10) || 0)}
                  className="w-16 h-7 text-xs"
                  placeholder="Priority"
                />
                <span className="text-[10px] text-surface-400">prio</span>
              </div>
              <Button size="sm" onClick={handleAdd}>
                <Plus size={14} />
                Add
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
