import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { api, type LoopSettingsConfig } from '../../lib/api'
import { qk } from '../../lib/query'
import { Button } from '../ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '../ui/card'
import { Loader2, Save } from '../ui/icons'
import { Input } from '../ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '../ui/select'

interface FieldRowProps {
  label: string
  description?: string
  children: React.ReactNode
}

function FieldRow({ label, description, children }: FieldRowProps) {
  return (
    <div className="flex items-center justify-between gap-4 py-2">
      <div className="flex-1 min-w-0">
        <label className="block text-sm font-medium text-surface-700">{label}</label>
        {description && <p className="text-xs text-surface-400 mt-0.5">{description}</p>}
      </div>
      <div className="w-48 shrink-0">{children}</div>
    </div>
  )
}

export function LoopSettingsForm() {
  const queryClient = useQueryClient()

  const { data: settings, isLoading } = useQuery({
    queryKey: qk.loopSettings,
    queryFn: () => api.loops.getLoopSettings(),
  })

  const [form, setForm] = useState<LoopSettingsConfig | null>(null)

  const loadedRef = useRef(false)

  useEffect(() => {
    if (settings && !loadedRef.current) {
      loadedRef.current = true
      setForm(settings)
    }
  }, [settings])

  const saveMutation = useMutation({
    mutationFn: (data: Partial<LoopSettingsConfig>) => api.loops.updateLoopSettings(data),
    onSuccess: (updated) => {
      setForm(updated)
      queryClient.invalidateQueries({ queryKey: qk.loopSettings })
    },
  })

  const updateField = <K extends keyof LoopSettingsConfig>(key: K, value: LoopSettingsConfig[K]) => {
    if (!form) return
    setForm({ ...form, [key]: value })
  }

  if (isLoading || !form) return null

  const hasChanges = JSON.stringify(form) !== JSON.stringify(settings)

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Loop Detection Settings</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="divide-y divide-surface-100">
          {/* ── Request loop detection ── */}
          <div className="pb-2">
            <h4 className="text-xs font-semibold uppercase tracking-wider text-surface-500 mb-1">
              Request Loop Detection
            </h4>
            <FieldRow label="Rate Threshold" description="Requests per minute to trigger a warning (0 = disabled)">
              <Input
                type="number"
                min={0}
                value={form.rate_threshold}
                onChange={(e) => updateField('rate_threshold', parseInt(e.target.value, 10) || 0)}
              />
            </FieldRow>
            <FieldRow label="Fingerprint Window" description="Window size (in requests) to track repeating content">
              <Input
                type="number"
                min={1}
                value={form.fingerprint_window}
                onChange={(e) => updateField('fingerprint_window', parseInt(e.target.value, 10) || 1)}
              />
            </FieldRow>
            <FieldRow label="Fingerprint Duplicates" description="Same content repeats before flagging">
              <Input
                type="number"
                min={1}
                value={form.fingerprint_duplicates}
                onChange={(e) => updateField('fingerprint_duplicates', parseInt(e.target.value, 10) || 1)}
              />
            </FieldRow>
            <FieldRow label="Cost Window" description="Time window for cost threshold (e.g. 5m, 10m, 1h)">
              <Input value={form.cost_window} onChange={(e) => updateField('cost_window', e.target.value)} />
            </FieldRow>
            <FieldRow label="Cost Threshold" description="Total cost in the window before flagging ($)">
              <Input
                type="number"
                min={0}
                step={0.5}
                value={form.cost_threshold}
                onChange={(e) => updateField('cost_threshold', parseFloat(e.target.value) || 0)}
              />
            </FieldRow>
            <FieldRow label="Session Max Requests" description="Max requests per session before flagging">
              <Input
                type="number"
                min={1}
                value={form.session_max_requests}
                onChange={(e) => updateField('session_max_requests', parseInt(e.target.value, 10) || 1)}
              />
            </FieldRow>
          </div>

          {/* ── Output loop detection ── */}
          <div className="pt-2">
            <h4 className="text-xs font-semibold uppercase tracking-wider text-surface-500 mb-1">
              Output Loop Detection
            </h4>
            <FieldRow label="Mode" description="off = disabled, observe = log only, enforce = cut stream">
              <Select
                value={form.output_loop_mode}
                onValueChange={(v: string | null) => updateField('output_loop_mode', v ?? 'off')}
              >
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="off">Off</SelectItem>
                  <SelectItem value="observe">Observe</SelectItem>
                  <SelectItem value="enforce">Enforce</SelectItem>
                </SelectContent>
              </Select>
            </FieldRow>
            <FieldRow label="Threshold" description="Consecutive identical sentences to trigger">
              <Input
                type="number"
                min={2}
                value={form.output_loop_threshold}
                onChange={(e) => updateField('output_loop_threshold', parseInt(e.target.value, 10) || 2)}
              />
            </FieldRow>
            <FieldRow label="Min Sentence Length" description="Minimum characters for a sentence to be considered">
              <Input
                type="number"
                min={1}
                value={form.output_min_sentence_len}
                onChange={(e) => updateField('output_min_sentence_len', parseInt(e.target.value, 10) || 1)}
              />
            </FieldRow>
          </div>
        </div>

        <div className="mt-4 flex justify-end">
          <Button
            variant="default"
            size="sm"
            disabled={!hasChanges || saveMutation.isPending}
            onClick={() => saveMutation.mutate(form)}
          >
            {saveMutation.isPending ? <Loader2 size={14} className="animate-spin" /> : <Save size={14} />}
            {saveMutation.isPending ? 'Saving...' : 'Save Changes'}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
