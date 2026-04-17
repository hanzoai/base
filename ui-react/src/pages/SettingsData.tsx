import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useRef, useState } from 'react'
import { importCollections, listCollections, type CollectionModel } from '~/lib/api'
import { SectionCard } from '~/components/SectionCard'

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm w-full'
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50'
const btnSecondary = 'rounded border border-neutral-700 px-3 py-1.5 text-sm hover:bg-neutral-800'
const btnWarning = 'rounded bg-yellow-600 px-4 py-1.5 text-sm font-medium hover:bg-yellow-500 disabled:opacity-50'

export function SettingsData() {
  const qc = useQueryClient()
  const fileRef = useRef<HTMLInputElement>(null)
  const [importJson, setImportJson] = useState('')
  const [importParsed, setImportParsed] = useState<CollectionModel[]>([])
  const [importError, setImportError] = useState('')

  const collections = useQuery({
    queryKey: ['collections'],
    queryFn: () => listCollections({ sort: 'name', batch: 200 }),
  })

  const [selected, setSelected] = useState<Set<string>>(new Set())
  const allCollections = collections.data ?? []
  const allSelected = selected.size === allCollections.length && allCollections.length > 0

  function toggleAll() {
    if (allSelected) setSelected(new Set())
    else setSelected(new Set(allCollections.map((c) => c.id)))
  }

  function toggleOne(id: string) {
    const next = new Set(selected)
    if (next.has(id)) next.delete(id)
    else next.add(id)
    setSelected(next)
  }

  function exportJson() {
    const exported = allCollections.filter((c) => selected.has(c.id)).map(({ created, updated, ...rest }) => rest)
    const blob = new Blob([JSON.stringify(exported, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url; a.download = 'collections-schema.json'; a.click()
    URL.revokeObjectURL(url)
  }

  function copyExport() {
    const exported = allCollections.filter((c) => selected.has(c.id)).map(({ created, updated, ...rest }) => rest)
    void navigator.clipboard.writeText(JSON.stringify(exported, null, 2))
  }

  function parseImport(text: string) {
    setImportError('')
    try {
      const parsed = JSON.parse(text)
      if (!Array.isArray(parsed)) { setImportError('Expected a JSON array.'); setImportParsed([]); return }
      setImportParsed(parsed as CollectionModel[])
    } catch { setImportError('Invalid JSON.'); setImportParsed([]) }
  }

  function handleFileLoad(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    const reader = new FileReader()
    reader.onload = (ev) => { const text = ev.target?.result as string; setImportJson(text); parseImport(text) }
    reader.readAsText(file)
  }

  const existingById = new Map(allCollections.map((c) => [c.id, c]))
  const toAdd = importParsed.filter((c) => !existingById.has(c.id))
  const toUpdate = importParsed.filter((c) => existingById.has(c.id))

  const importMutation = useMutation({
    mutationFn: () => importCollections(importParsed),
    onSuccess: () => { setImportJson(''); setImportParsed([]); qc.invalidateQueries({ queryKey: ['collections'] }) },
  })

  if (collections.isPending) return <div className="text-sm text-neutral-400">Loading...</div>

  return (
    <div className="flex flex-col gap-6">
      <SectionCard title="Export collections" description="Download your collection schemas as JSON.">
        <div className="mb-3 flex items-center gap-2">
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={allSelected} onChange={toggleAll} className="accent-indigo-500" />
            <span className="text-neutral-400">Select all ({allCollections.length})</span>
          </label>
        </div>
        <div className="mb-4 flex flex-wrap gap-1.5">
          {allCollections.map((c) => (
            <label key={c.id} className={'flex items-center gap-1.5 rounded border px-2 py-1 text-xs cursor-pointer ' +
              (selected.has(c.id) ? 'border-indigo-600 bg-indigo-900/20 text-indigo-300' : 'border-neutral-700 text-neutral-400 hover:bg-neutral-800')}>
              <input type="checkbox" checked={selected.has(c.id)} onChange={() => toggleOne(c.id)} className="accent-indigo-500" />
              {c.name}
            </label>
          ))}
        </div>
        <div className="flex gap-2">
          <button onClick={exportJson} disabled={selected.size === 0} className={btnPrimary}>Download JSON</button>
          <button onClick={copyExport} disabled={selected.size === 0} className={btnSecondary}>Copy to clipboard</button>
        </div>
      </SectionCard>

      <SectionCard title="Import collections" description="Upload a JSON schema to create or update collections.">
        <div className="mb-3 flex items-center gap-3">
          <input ref={fileRef} type="file" accept=".json" onChange={handleFileLoad} className="hidden" />
          <button onClick={() => fileRef.current?.click()} className={btnSecondary}>Load from file</button>
          <span className="text-xs text-neutral-500">or paste JSON below</span>
        </div>
        <textarea
          value={importJson}
          onChange={(e) => { setImportJson(e.target.value); parseImport(e.target.value) }}
          rows={10} spellCheck={false}
          placeholder='[{ "id": "...", "name": "...", ... }]'
          className={inputClass + ' font-mono text-xs'}
        />
        {importError && <div className="mt-2 text-xs text-red-400">{importError}</div>}
        {importParsed.length > 0 && (
          <div className="mt-4">
            <h4 className="mb-2 text-xs font-semibold uppercase tracking-wider text-neutral-500">Detected changes</h4>
            <ul className="flex flex-col gap-1 text-sm">
              {toAdd.map((c) => <li key={c.id} className="flex items-center gap-2"><span className="rounded bg-green-900 px-1.5 py-0.5 text-xs text-green-300">Add</span><span>{c.name}</span></li>)}
              {toUpdate.map((c) => <li key={c.id} className="flex items-center gap-2"><span className="rounded bg-yellow-900 px-1.5 py-0.5 text-xs text-yellow-300">Update</span><span>{c.name}</span></li>)}
            </ul>
          </div>
        )}
        <div className="mt-4 flex items-center gap-2">
          <button onClick={() => importMutation.mutate()} disabled={importParsed.length === 0 || importMutation.isPending} className={btnWarning}>
            {importMutation.isPending ? 'Importing...' : 'Apply import'}
          </button>
          {importJson && <button onClick={() => { setImportJson(''); setImportParsed([]); setImportError('') }} className="text-xs text-neutral-500 hover:text-neutral-300">Clear</button>}
          {importMutation.isSuccess && <span className="text-xs text-green-400">Imported.</span>}
          {importMutation.error && <span className="text-xs text-red-400">{String(importMutation.error)}</span>}
        </div>
      </SectionCard>
    </div>
  )
}
