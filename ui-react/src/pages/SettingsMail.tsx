import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useMemo, useState, type FormEvent } from 'react'
import { listCollections, updateCollection } from '~/lib/api'
import { SectionCard } from '~/components/SectionCard'

const templateKeys = [
  { key: 'verificationTemplate', label: 'Verification' },
  { key: 'resetPasswordTemplate', label: 'Password reset' },
  { key: 'confirmEmailChangeTemplate', label: 'Email change' },
] as const

type TemplateKey = typeof templateKeys[number]['key']

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm w-full'
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50'

export function SettingsMail() {
  const qc = useQueryClient()
  const [selectedCollection, setSelectedCollection] = useState('')
  const [activeTemplate, setActiveTemplate] = useState<TemplateKey>('verificationTemplate')

  const authCollections = useQuery({
    queryKey: ['collections', 'auth'],
    queryFn: () => listCollections({ filter: "type='auth'" }),
  })

  const collections = authCollections.data ?? []
  const collectionId = selectedCollection || collections[0]?.id || ''
  const collection = collections.find((c) => c.id === collectionId)

  const templateData = collection
    ? (collection as Record<string, unknown>)[activeTemplate] as { subject?: string; body?: string; actionUrl?: string } | undefined
    : undefined

  const [subject, setSubject] = useState(templateData?.subject ?? '')
  const [body, setBody] = useState(templateData?.body ?? '')
  const [actionUrl, setActionUrl] = useState(templateData?.actionUrl ?? '')
  const [dirty, setDirty] = useState(false)

  // Sync when template data changes
  if (templateData && !dirty) {
    if (subject !== (templateData.subject ?? '') || body !== (templateData.body ?? '') || actionUrl !== (templateData.actionUrl ?? '')) {
      setSubject(templateData.subject ?? '')
      setBody(templateData.body ?? '')
      setActionUrl(templateData.actionUrl ?? '')
    }
  }

  function markDirty<T>(setter: (v: T) => void) {
    return (v: T) => { setter(v); setDirty(true) }
  }

  const previewHtml = useMemo(() => {
    return body
      .replace(/\{APP_NAME\}/g, 'App Name')
      .replace(/\{APP_URL\}/g, 'https://example.com')
      .replace(/\{TOKEN\}/g, 'test-token-xxx')
      .replace(/\{ACTION_URL\}/g, '#')
      .replace(/\{OTP\}/g, '123456')
      .replace(/\{RECORD:.*?\}/g, 'value')
  }, [body])

  const saveMutation = useMutation({
    mutationFn: async () => {
      await updateCollection(collectionId, { [activeTemplate]: { subject, body, actionUrl } })
    },
    onSuccess: () => { setDirty(false); qc.invalidateQueries({ queryKey: ['collections', 'auth'] }) },
  })

  if (authCollections.isPending) return <div className="text-sm text-neutral-400">Loading...</div>
  if (collections.length === 0) return <div className="text-sm text-neutral-500">No auth collections found.</div>

  function handleSubmit(e: FormEvent) { e.preventDefault(); saveMutation.mutate() }

  return (
    <div className="flex flex-col gap-6">
      <SectionCard title="Mail templates" description="Edit email templates for auth collections.">
        <div className="mb-4 flex items-center gap-3">
          <select
            value={collectionId}
            onChange={(e) => { setSelectedCollection(e.target.value); setDirty(false) }}
            className={inputClass + ' max-w-xs'}
          >
            {collections.map((c) => <option key={c.id} value={c.id}>{c.name}</option>)}
          </select>
          <div className="flex gap-1">
            {templateKeys.map((t) => (
              <button
                key={t.key}
                type="button"
                onClick={() => { setActiveTemplate(t.key); setDirty(false) }}
                className={'rounded px-3 py-1 text-xs transition-colors ' +
                  (activeTemplate === t.key ? 'bg-neutral-700 text-neutral-100' : 'text-neutral-400 hover:bg-neutral-800')}
              >
                {t.label}
              </button>
            ))}
          </div>
        </div>

        <form onSubmit={handleSubmit}>
          <div className="grid grid-cols-2 gap-4">
            <div className="flex flex-col gap-3">
              <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Subject</span><input value={subject} onChange={(e) => markDirty(setSubject)(e.target.value)} required className={inputClass} /></label>
              <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Action URL</span><input value={actionUrl} onChange={(e) => markDirty(setActionUrl)(e.target.value)} className={inputClass} /></label>
              <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Body (HTML)</span>
                <textarea value={body} onChange={(e) => markDirty(setBody)(e.target.value)} rows={14} spellCheck={false} required className={inputClass + ' font-mono text-xs leading-relaxed'} />
              </label>
              <div className="text-xs text-neutral-600">
                Placeholders: {'{'}APP_NAME{'}'}, {'{'}APP_URL{'}'}, {'{'}TOKEN{'}'}, {'{'}ACTION_URL{'}'}, {'{'}OTP{'}'}
              </div>
            </div>
            <div className="flex flex-col gap-1">
              <span className="text-sm text-neutral-400">Preview</span>
              <iframe sandbox="" srcDoc={previewHtml} className="flex-1 overflow-auto rounded border border-neutral-700 bg-white" title="Mail preview" style={{ minHeight: '320px' }} />
            </div>
          </div>
          <div className="mt-4 flex items-center gap-2">
            <button type="submit" disabled={!dirty || saveMutation.isPending} className={btnPrimary}>{saveMutation.isPending ? 'Saving...' : 'Save template'}</button>
            {saveMutation.isSuccess && <span className="text-xs text-green-400">Saved.</span>}
            {saveMutation.error && <span className="text-xs text-red-400">{String(saveMutation.error)}</span>}
          </div>
        </form>
      </SectionCard>
    </div>
  )
}
