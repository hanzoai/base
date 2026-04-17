import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { getCollection, listCollections, updateCollection } from '~/lib/api'
import { SectionCard } from '~/components/SectionCard'

const knownProviders = [
  'google', 'github', 'apple', 'discord', 'microsoft', 'facebook',
  'gitlab', 'twitter', 'spotify', 'twitch', 'strava', 'kakao',
  'livechat', 'gitee', 'gitea', 'bitbucket', 'patreon', 'mailcow',
  'vk', 'yandex', 'oidc', 'oidc2', 'oidc3',
] as const

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm w-full'
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50'

export function SettingsAuth() {
  const qc = useQueryClient()
  const [editing, setEditing] = useState<string | null>(null)

  const authCollections = useQuery({
    queryKey: ['collections', 'auth'],
    queryFn: () => listCollections({ filter: "type='auth'" }),
  })

  if (authCollections.isPending) return <div className="text-sm text-neutral-400">Loading...</div>

  return (
    <div className="flex flex-col gap-6">
      <SectionCard title="OAuth2 providers" description="Configure OAuth2 / OIDC providers for auth collections.">
        {authCollections.data?.filter((c) => c.type === 'auth').map((col) => {
          const oauth2 = col.oauth2 as { enabled?: boolean; providers?: Array<Record<string, unknown>> } | undefined
          const providers = oauth2?.providers ?? []

          return (
            <div key={col.id} className="mb-6">
              <h3 className="mb-2 text-sm font-medium text-neutral-300">
                {col.name}
                <span className="ml-2 text-xs text-neutral-500">OAuth2 {oauth2?.enabled ? 'enabled' : 'disabled'}</span>
              </h3>
              <div className="grid grid-cols-3 gap-2 sm:grid-cols-4 lg:grid-cols-6">
                {knownProviders.map((provName) => {
                  const existing = providers.find((p) => p.name === provName)
                  const configured = existing && (existing.clientId as string)
                  const key = `${col.name}:${provName}`
                  return (
                    <button
                      key={provName}
                      type="button"
                      onClick={() => setEditing(editing === key ? null : key)}
                      className={'rounded border px-3 py-2 text-xs transition-colors ' +
                        (configured
                          ? 'border-green-700 bg-green-900/30 text-green-300 hover:bg-green-900/50'
                          : 'border-neutral-700 text-neutral-400 hover:bg-neutral-800')}
                    >
                      {provName}
                    </button>
                  )
                })}
              </div>
              {editing && editing.startsWith(col.name + ':') && (
                <ProviderEditor
                  collectionId={col.id}
                  collectionName={col.name}
                  providerName={editing.split(':')[1]}
                  existing={providers.find((p) => p.name === editing.split(':')[1])}
                  onClose={() => setEditing(null)}
                  onSaved={() => { qc.invalidateQueries({ queryKey: ['collections', 'auth'] }); setEditing(null) }}
                />
              )}
            </div>
          )
        })}
        {(!authCollections.data || authCollections.data.filter((c) => c.type === 'auth').length === 0) && (
          <div className="text-sm text-neutral-500">No auth collections found.</div>
        )}
      </SectionCard>
    </div>
  )
}

function ProviderEditor({ collectionId, collectionName, providerName, existing, onClose, onSaved }: {
  collectionId: string; collectionName: string; providerName: string
  existing: Record<string, unknown> | undefined; onClose: () => void; onSaved: () => void
}) {
  const [form, setForm] = useState({
    clientId: (existing?.clientId as string) ?? '',
    clientSecret: (existing?.clientSecret as string) ?? '',
    authURL: (existing?.authURL as string) ?? '',
    tokenURL: (existing?.tokenURL as string) ?? '',
    userInfoURL: (existing?.userInfoURL as string) ?? '',
    displayName: (existing?.displayName as string) ?? providerName,
  })

  function set(k: string, v: string) { setForm({ ...form, [k]: v }) }

  const saveMutation = useMutation({
    mutationFn: async () => {
      const col = await getCollection(collectionId)
      const oauth2 = col.oauth2 as { enabled?: boolean; providers?: Array<Record<string, unknown>>; mappedFields?: Record<string, string> } | undefined
      const providers = [...(oauth2?.providers ?? [])]
      const idx = providers.findIndex((p) => p.name === providerName)
      const entry: Record<string, unknown> = {
        name: providerName, clientId: form.clientId, authURL: form.authURL,
        tokenURL: form.tokenURL, userInfoURL: form.userInfoURL, displayName: form.displayName,
      }
      if (form.clientSecret !== (existing?.clientSecret as string)) entry.clientSecret = form.clientSecret
      if (idx >= 0) providers[idx] = { ...providers[idx], ...entry }
      else providers.push(entry)
      await updateCollection(collectionId, { oauth2: { ...oauth2, enabled: true, providers } })
    },
    onSuccess: onSaved,
  })

  function handleSubmit(e: FormEvent) { e.preventDefault(); saveMutation.mutate() }

  return (
    <div className="mt-3 rounded border border-neutral-700 bg-neutral-900 p-4">
      <div className="mb-3 flex items-center justify-between">
        <h4 className="text-sm font-medium">{collectionName} / {providerName}</h4>
        <button onClick={onClose} className="text-xs text-neutral-500 hover:text-neutral-300">Close</button>
      </div>
      <form onSubmit={handleSubmit} className="flex flex-col gap-3">
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Client ID</span><input value={form.clientId} onChange={(e) => set('clientId', e.target.value)} required className={inputClass} /></label>
          <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Client secret</span><input value={form.clientSecret} onChange={(e) => set('clientSecret', e.target.value)} required type="password" className={inputClass} /></label>
        </div>
        <div className="grid grid-cols-2 gap-3">
          <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Auth URL</span><input value={form.authURL} onChange={(e) => set('authURL', e.target.value)} className={inputClass} placeholder="Auto-detected" /></label>
          <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Token URL</span><input value={form.tokenURL} onChange={(e) => set('tokenURL', e.target.value)} className={inputClass} placeholder="Auto-detected" /></label>
        </div>
        <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">User info URL</span><input value={form.userInfoURL} onChange={(e) => set('userInfoURL', e.target.value)} className={inputClass} placeholder="Auto-detected" /></label>
        <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Display name</span><input value={form.displayName} onChange={(e) => set('displayName', e.target.value)} className={inputClass} /></label>
        <div className="flex items-center gap-2 pt-1">
          <button type="submit" disabled={saveMutation.isPending} className={btnPrimary}>{saveMutation.isPending ? 'Saving...' : 'Save provider'}</button>
          {saveMutation.error && <span className="text-xs text-red-400">{String(saveMutation.error)}</span>}
        </div>
      </form>
    </div>
  )
}
