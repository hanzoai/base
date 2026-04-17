import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState, type FormEvent } from 'react'
import { createBackup, deleteBackup, getBackupDownloadURL, getFileToken, getSettings, listBackups, restoreBackup, updateSettings } from '~/lib/api'
import { SectionCard } from '~/components/SectionCard'

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm'
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50'
const btnSecondary = 'rounded border border-neutral-700 px-3 py-1.5 text-sm hover:bg-neutral-800'

function formatBytes(bytes: number): string {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
}

export function SettingsBackups() {
  const qc = useQueryClient()
  const [showOptions, setShowOptions] = useState(false)
  const [newName, setNewName] = useState('')
  const [restoreTarget, setRestoreTarget] = useState<string | null>(null)

  const settings = useQuery({ queryKey: ['settings'], queryFn: getSettings })
  const backupsList = useQuery({
    queryKey: ['backups'],
    queryFn: listBackups,
    select: (data) => [...data].sort((a, b) => (a.key < b.key ? 1 : -1)),
  })

  const backupsSettings = settings.data?.backups as Record<string, unknown> | undefined
  const s3Config = (backupsSettings?.s3 ?? {}) as Record<string, unknown>

  const [optForm, setOptForm] = useState<Record<string, unknown> | null>(null)
  const optVals = optForm ?? {
    cron: (backupsSettings?.cron as string) ?? '',
    cronMaxKeep: (backupsSettings?.cronMaxKeep as number) ?? 5,
    s3: {
      enabled: (s3Config.enabled as boolean) ?? false,
      endpoint: (s3Config.endpoint as string) ?? '',
      bucket: (s3Config.bucket as string) ?? '',
      region: (s3Config.region as string) ?? '',
      accessKey: (s3Config.accessKey as string) ?? '',
      secret: (s3Config.secret as string) ?? '',
      forcePathStyle: (s3Config.forcePathStyle as boolean) ?? false,
    },
  }
  const s3Vals = optVals.s3 as Record<string, unknown>

  function setOpt(k: string, v: unknown) { setOptForm({ ...optVals, [k]: v }) }
  function setS3(k: string, v: unknown) { setOptForm({ ...optVals, s3: { ...s3Vals, [k]: v } }) }

  const saveOptions = useMutation({
    mutationFn: () => {
      const s3Payload: Record<string, unknown> = { ...s3Vals }
      if (s3Payload.secret === (s3Config.secret as string)) delete s3Payload.secret
      return updateSettings({ backups: { ...optVals, s3: s3Payload } })
    },
    onSuccess: () => { setOptForm(null); qc.invalidateQueries({ queryKey: ['settings'] }) },
  })

  const createMut = useMutation({
    mutationFn: (name: string) => createBackup(name),
    onSuccess: () => { setNewName(''); qc.invalidateQueries({ queryKey: ['backups'] }) },
  })

  const deleteMut = useMutation({
    mutationFn: (key: string) => deleteBackup(key),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['backups'] }),
  })

  const restoreMut = useMutation({
    mutationFn: (key: string) => restoreBackup(key),
    onSuccess: () => setRestoreTarget(null),
  })

  const downloadMut = useMutation({
    mutationFn: async (key: string) => {
      const token = await getFileToken()
      window.open(getBackupDownloadURL(key, token), '_blank')
    },
  })

  if (settings.isPending) return <div className="text-sm text-neutral-400">Loading...</div>

  return (
    <div className="flex flex-col gap-6">
      <SectionCard title="Backups" description="Create, restore, download, or delete database backups.">
        <div className="mb-4 flex items-center gap-2">
          <input value={newName} onChange={(e) => setNewName(e.target.value)} placeholder="backup-name.zip" className={inputClass + ' flex-1'} />
          <button onClick={() => createMut.mutate(newName || `backup-${Date.now()}.zip`)} disabled={createMut.isPending} className={btnPrimary}>
            {createMut.isPending ? 'Creating...' : 'New backup'}
          </button>
          <button onClick={() => qc.invalidateQueries({ queryKey: ['backups'] })} className={btnSecondary}>Refresh</button>
        </div>
        {createMut.error && <div className="mb-2 text-xs text-red-400">{String(createMut.error)}</div>}

        {backupsList.isPending && <div className="text-sm text-neutral-400">Loading backups...</div>}
        {backupsList.data && backupsList.data.length === 0 && <div className="text-sm text-neutral-500">No backups yet.</div>}

        {backupsList.data && backupsList.data.length > 0 && (
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase tracking-wider text-neutral-500">
              <tr><th className="py-2">Name</th><th className="py-2">Size</th><th className="py-2 text-right">Actions</th></tr>
            </thead>
            <tbody>
              {backupsList.data.map((b) => (
                <tr key={b.key} className="border-t border-neutral-800 hover:bg-neutral-900">
                  <td className="py-2 font-mono text-xs">{b.key}</td>
                  <td className="py-2 text-neutral-400">{formatBytes(b.size)}</td>
                  <td className="py-2 text-right">
                    <div className="flex items-center justify-end gap-2">
                      <button onClick={() => downloadMut.mutate(b.key)} className="text-xs text-indigo-400 hover:text-indigo-300">Download</button>
                      {restoreTarget === b.key ? (
                        <>
                          <span className="text-xs text-yellow-400">Confirm?</span>
                          <button onClick={() => restoreMut.mutate(b.key)} disabled={restoreMut.isPending} className="text-xs text-yellow-400 hover:text-yellow-300">Yes</button>
                          <button onClick={() => setRestoreTarget(null)} className="text-xs text-neutral-500 hover:text-neutral-300">No</button>
                        </>
                      ) : (
                        <button onClick={() => setRestoreTarget(b.key)} className="text-xs text-yellow-400 hover:text-yellow-300">Restore</button>
                      )}
                      <button onClick={() => { if (confirm(`Delete backup "${b.key}"?`)) deleteMut.mutate(b.key) }} disabled={deleteMut.isPending} className="text-xs text-red-400 hover:text-red-300">Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
        {restoreMut.error && <div className="mt-2 text-xs text-red-400">{String(restoreMut.error)}</div>}
      </SectionCard>

      <SectionCard title="Backup options" description="Configure auto backups and S3 storage.">
        <button type="button" onClick={() => setShowOptions(!showOptions)} className={btnSecondary}>{showOptions ? 'Hide options' : 'Show options'}</button>
        {showOptions && (
          <form onSubmit={(e: FormEvent) => { e.preventDefault(); saveOptions.mutate() }} className="mt-4 flex flex-col gap-4">
            <div className="grid grid-cols-2 gap-4">
              <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Cron expression (UTC)</span><input value={String(optVals.cron)} onChange={(e) => setOpt('cron', e.target.value)} placeholder="0 0 * * *" className={inputClass + ' font-mono'} /><span className="text-xs text-neutral-600">Leave empty to disable auto backups</span></label>
              <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Max auto backups to keep</span><input type="number" value={Number(optVals.cronMaxKeep)} onChange={(e) => setOpt('cronMaxKeep', Number(e.target.value))} min="1" className={inputClass} /></label>
            </div>
            <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={Boolean(s3Vals.enabled)} onChange={(e) => setS3('enabled', e.target.checked)} className="accent-indigo-500" /><span>Store backups in S3</span></label>
            {s3Vals.enabled && (
              <div className="grid grid-cols-2 gap-4">
                <label className="col-span-2 flex flex-col gap-1 text-sm"><span className="text-neutral-400">Endpoint</span><input value={String(s3Vals.endpoint)} onChange={(e) => setS3('endpoint', e.target.value)} className={inputClass} /></label>
                <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Bucket</span><input value={String(s3Vals.bucket)} onChange={(e) => setS3('bucket', e.target.value)} className={inputClass} /></label>
                <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Region</span><input value={String(s3Vals.region)} onChange={(e) => setS3('region', e.target.value)} className={inputClass} /></label>
                <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Access key</span><input value={String(s3Vals.accessKey)} onChange={(e) => setS3('accessKey', e.target.value)} className={inputClass} /></label>
                <label className="flex flex-col gap-1 text-sm"><span className="text-neutral-400">Secret</span><input type="password" value={String(s3Vals.secret)} onChange={(e) => setS3('secret', e.target.value)} className={inputClass} /></label>
                <label className="col-span-2 flex items-center gap-2 text-sm"><input type="checkbox" checked={Boolean(s3Vals.forcePathStyle)} onChange={(e) => setS3('forcePathStyle', e.target.checked)} className="accent-indigo-500" /><span>Force path-style</span></label>
              </div>
            )}
            <div className="flex items-center gap-2 pt-2">
              <button type="submit" disabled={optForm === null || saveOptions.isPending} className={btnPrimary}>{saveOptions.isPending ? 'Saving...' : 'Save options'}</button>
              {saveOptions.isSuccess && <span className="text-xs text-green-400">Saved.</span>}
              {saveOptions.error && <span className="text-xs text-red-400">{String(saveOptions.error)}</span>}
            </div>
          </form>
        )}
      </SectionCard>
    </div>
  )
}
