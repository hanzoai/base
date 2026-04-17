import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { getSettings, updateSettings } from '~/lib/api'
import { SectionCard } from '~/components/SectionCard'

interface RateLimitRule {
  label: string
  maxRequests: number
  duration: number
  audience: string
}

const audienceOptions = [
  { value: '', label: 'All' },
  { value: '@guest', label: 'Guest only' },
  { value: '@auth', label: 'Auth only' },
]

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm'
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50'
const btnSecondary = 'rounded border border-neutral-700 px-3 py-1 text-sm hover:bg-neutral-800'

export function SettingsRateLimits() {
  const qc = useQueryClient()
  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })

  const initial = settings.data?.rateLimits as { enabled?: boolean; rules?: RateLimitRule[] } | undefined

  const [enabled, setEnabled] = useState(initial?.enabled ?? false)
  const [rules, setRules] = useState<RateLimitRule[]>(initial?.rules ?? [])
  const [dirty, setDirty] = useState(false)
  const [synced, setSynced] = useState(false)

  if (initial && !synced) {
    setSynced(true)
    setEnabled(initial.enabled ?? false)
    setRules(initial.rules ?? [])
  }

  function addRule() {
    setRules([...rules, { label: '', maxRequests: 300, duration: 10, audience: '' }])
    setDirty(true)
    if (rules.length === 0) setEnabled(true)
  }

  function removeRule(i: number) {
    const next = rules.filter((_, idx) => idx !== i)
    setRules(next); setDirty(true)
    if (next.length === 0) setEnabled(false)
  }

  function updateRule(i: number, field: keyof RateLimitRule, value: string | number) {
    const next = [...rules]; next[i] = { ...next[i], [field]: value }
    setRules(next); setDirty(true)
  }

  const saveMutation = useMutation({
    mutationFn: () => updateSettings({ rateLimits: { enabled, rules } }),
    onSuccess: () => { setDirty(false); qc.invalidateQueries({ queryKey: ['settings'] }) },
  })

  if (settings.isPending) return <div className="text-sm text-neutral-400">Loading...</div>
  if (settings.error) return <div className="text-sm text-red-400">{String(settings.error)}</div>

  return (
    <div className="flex flex-col gap-6">
      <SectionCard title="Rate limits" description="Configure per-route request rate limiting.">
        <label className="mb-4 flex items-center gap-2 text-sm">
          <input type="checkbox" checked={enabled} onChange={(e) => { setEnabled(e.target.checked); setDirty(true) }} className="accent-indigo-500" />
          <span>Enable rate limiting</span>
          <span className="text-xs text-neutral-600">(experimental)</span>
        </label>

        {rules.length > 0 && (
          <table className="mb-4 w-full text-sm">
            <thead className="text-left text-xs uppercase tracking-wider text-neutral-500">
              <tr><th className="py-2">Label</th><th className="py-2">Max requests</th><th className="py-2">Interval (sec)</th><th className="py-2">Audience</th><th className="py-2" /></tr>
            </thead>
            <tbody>
              {rules.map((rule, i) => (
                <tr key={i} className="border-t border-neutral-800">
                  <td className="py-1.5"><input value={rule.label} onChange={(e) => updateRule(i, 'label', e.target.value)} placeholder="tag or /path/" className={inputClass + ' w-full'} /></td>
                  <td className="px-1 py-1.5"><input type="number" min="1" value={rule.maxRequests} onChange={(e) => updateRule(i, 'maxRequests', parseInt(e.target.value, 10) || 1)} className={inputClass + ' w-24'} /></td>
                  <td className="px-1 py-1.5"><input type="number" min="1" value={rule.duration} onChange={(e) => updateRule(i, 'duration', parseInt(e.target.value, 10) || 1)} className={inputClass + ' w-24'} /></td>
                  <td className="px-1 py-1.5">
                    <select value={rule.audience} onChange={(e) => updateRule(i, 'audience', e.target.value)} className={inputClass}>
                      {audienceOptions.map((o) => <option key={o.value} value={o.value}>{o.label}</option>)}
                    </select>
                  </td>
                  <td className="py-1.5 text-right"><button onClick={() => removeRule(i)} className="text-xs text-red-400 hover:text-red-300">Remove</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}

        <div className="flex items-center gap-2">
          <button type="button" onClick={addRule} className={btnSecondary}>Add rule</button>
          <button type="button" onClick={() => saveMutation.mutate()} disabled={!dirty || saveMutation.isPending} className={btnPrimary}>
            {saveMutation.isPending ? 'Saving...' : 'Save changes'}
          </button>
          {saveMutation.isSuccess && <span className="text-xs text-green-400">Saved.</span>}
          {saveMutation.error && <span className="text-xs text-red-400">{String(saveMutation.error)}</span>}
        </div>
      </SectionCard>
    </div>
  )
}
