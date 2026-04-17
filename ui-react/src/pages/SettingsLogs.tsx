import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { getSettings, updateSettings } from '~/lib/api'
import { SectionCard } from '~/components/SectionCard'

const logLevels = [
  { value: 0, label: 'Default' },
  { value: -4, label: 'DEBUG (-4)' },
  { value: 4, label: 'WARN (4)' },
  { value: 8, label: 'ERROR (8)' },
] as const

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm'
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50'

export function SettingsLogs() {
  const qc = useQueryClient()
  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })

  const logs = settings.data?.logs as Record<string, unknown> | undefined

  const [maxDays, setMaxDays] = useState((logs?.maxDays as number) ?? 7)
  const [minLevel, setMinLevel] = useState((logs?.minLevel as number) ?? 0)
  const [logIP, setLogIP] = useState((logs?.logIP as boolean) ?? true)
  const [dirty, setDirty] = useState(false)

  if (logs && !dirty) {
    const d = (logs.maxDays as number) ?? 7
    const m = (logs.minLevel as number) ?? 0
    const ip = (logs.logIP as boolean) ?? true
    if (maxDays !== d || minLevel !== m || logIP !== ip) {
      setMaxDays(d); setMinLevel(m); setLogIP(ip)
    }
  }

  const saveMutation = useMutation({
    mutationFn: () => updateSettings({ logs: { maxDays, minLevel, logIP } }),
    onSuccess: () => { setDirty(false); qc.invalidateQueries({ queryKey: ['settings'] }) },
  })

  if (settings.isPending) return <div className="text-sm text-neutral-400">Loading...</div>
  if (settings.error) return <div className="text-sm text-red-400">{String(settings.error)}</div>

  function handleSubmit(e: FormEvent) { e.preventDefault(); saveMutation.mutate() }

  return (
    <div className="flex flex-col gap-6">
      <SectionCard title="Log settings" description="Configure log retention and minimum log level.">
        <form onSubmit={handleSubmit} className="flex flex-col gap-4 max-w-md">
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-neutral-400">Log retention (days)</span>
            <input type="number" min="1" value={maxDays} onChange={(e) => { setMaxDays(Number(e.target.value)); setDirty(true) }} className={inputClass} />
            <span className="text-xs text-neutral-600">0 = keep indefinitely</span>
          </label>
          <label className="flex flex-col gap-1 text-sm">
            <span className="text-neutral-400">Minimum log level</span>
            <select value={minLevel} onChange={(e) => { setMinLevel(Number(e.target.value)); setDirty(true) }} className={inputClass}>
              {logLevels.map((l) => <option key={l.value} value={l.value}>{l.label}</option>)}
            </select>
          </label>
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={logIP} onChange={(e) => { setLogIP(e.target.checked); setDirty(true) }} className="accent-indigo-500" />
            <span>Log client IP addresses</span>
          </label>
          <div className="flex items-center gap-2 pt-2">
            <button type="submit" disabled={!dirty || saveMutation.isPending} className={btnPrimary}>{saveMutation.isPending ? 'Saving...' : 'Save log settings'}</button>
            {saveMutation.isSuccess && <span className="text-xs text-green-400">Saved.</span>}
            {saveMutation.error && <span className="text-xs text-red-400">{String(saveMutation.error)}</span>}
          </div>
        </form>
      </SectionCard>
    </div>
  )
}
