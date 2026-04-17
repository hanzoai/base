import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useMemo, useRef, useState, type FormEvent } from 'react'
import { Link, useLocation } from 'wouter'
import {
  createRecord,
  deleteRecord,
  getCollection,
  getRecord,
  updateRecord,
  type CollectionField,
} from '~/lib/api'

type RecordFormValues = Record<string, unknown>

const AUTH_SKIP = new Set(['email', 'emailVisibility', 'verified', 'tokenKey', 'password'])

function defaultForType(type: string): unknown {
  switch (type) {
    case 'number': return 0
    case 'bool': return false
    default: return ''
  }
}

export function RecordEdit({ id, recordId }: { id: string; recordId: string }) {
  const qc = useQueryClient()
  const [, navigate] = useLocation()
  const isNew = recordId === '_new'

  const collection = useQuery({
    queryKey: ['collections', id],
    queryFn: () => getCollection(id),
  })

  const collectionName = collection.data?.name ?? id

  const record = useQuery({
    queryKey: ['records', collectionName, recordId],
    queryFn: () => getRecord(collectionName, recordId),
    enabled: !isNew && Boolean(collection.data),
  })

  const editableFields = useMemo(() => {
    if (!collection.data) return []
    return collection.data.fields.filter((f) => f.type !== 'autodate' && f.name !== 'id')
  }, [collection.data])

  const [formValues, setFormValues] = useState<RecordFormValues | null>(null)
  const fileUploads = useRef<Record<string, File[]>>({})

  const defaults = useMemo(() => {
    const vals: RecordFormValues = {}
    for (const f of editableFields) {
      vals[f.name] = record.data?.[f.name] ?? defaultForType(f.type)
    }
    return vals
  }, [editableFields, record.data])

  const values = formValues ?? defaults

  function setValue(name: string, value: unknown) {
    setFormValues({ ...values, [name]: value })
  }

  const isAuth = collection.data?.type === 'auth'
  const isSuperusers = collection.data?.name === '_superusers'

  function buildFormData(): FormData {
    const fd = new FormData()
    for (const field of editableFields) {
      if (field.type === 'autodate') continue
      if (isAuth && field.type === 'password') continue
      if (field.type === 'file') continue

      const val = values[field.name]
      if (field.type === 'json' && typeof val === 'string' && val.trim()) {
        try { JSON.parse(val) } catch { throw new Error(`Invalid JSON in field "${field.name}"`) }
        fd.append(field.name, val)
      } else if (val !== undefined && val !== null) {
        fd.append(field.name, String(val))
      }
    }
    if (isAuth) {
      const pw = values['password']
      if (typeof pw === 'string' && pw) {
        fd.append('password', pw)
        fd.append('passwordConfirm', String(values['passwordConfirm'] ?? pw))
      }
    }
    for (const [fieldName, files] of Object.entries(fileUploads.current)) {
      for (const file of files) fd.append(fieldName, file)
    }
    return fd
  }

  const save = useMutation({
    mutationFn: async () => {
      const fd = buildFormData()
      if (isNew) return createRecord(collectionName, fd)
      return updateRecord(collectionName, recordId, fd)
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['records', collectionName] })
      navigate(`/collections/${id}/records`)
    },
  })

  const del = useMutation({
    mutationFn: () => deleteRecord(collectionName, recordId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['records', collectionName] })
      navigate(`/collections/${id}/records`)
    },
  })

  const handleSubmit = useCallback((e: FormEvent) => {
    e.preventDefault()
    save.mutate()
  }, [save])

  if (collection.isPending) return <div className="text-sm text-neutral-400">Loading schema...</div>
  if (collection.error) return <div className="text-sm text-red-400">{String(collection.error)}</div>
  if (!isNew && record.isPending) return <div className="text-sm text-neutral-400">Loading record...</div>
  if (!isNew && record.error) return <div className="text-sm text-red-400">{String(record.error)}</div>

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-4">
      <header className="flex items-center gap-3">
        <Link to={`/collections/${id}/records`} className="text-neutral-400 hover:text-neutral-200">
          {collection.data!.name}
        </Link>
        <span className="text-neutral-600">/</span>
        <h1 className="text-xl font-semibold">{isNew ? 'New record' : `Edit ${recordId}`}</h1>
        <div className="ml-auto flex gap-2">
          {!isNew && (
            <button
              type="button"
              onClick={() => { if (confirm('Delete this record?')) del.mutate() }}
              disabled={del.isPending}
              className="rounded bg-red-900/50 px-3 py-1 text-sm text-red-300 hover:bg-red-900 disabled:opacity-50"
            >
              Delete
            </button>
          )}
          <button
            type="submit"
            disabled={save.isPending}
            className="rounded bg-indigo-600 px-3 py-1 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50"
          >
            {save.isPending ? 'Saving...' : isNew ? 'Create' : 'Save'}
          </button>
        </div>
      </header>

      {save.error && <div className="text-sm text-red-400">{String(save.error)}</div>}

      {!isNew && record.data && (
        <div className="flex flex-col gap-1">
          <span className="text-xs text-neutral-500">id</span>
          <span className="rounded bg-neutral-900 px-2 py-1 text-sm font-mono text-neutral-400">{record.data.id}</span>
        </div>
      )}

      {isAuth && (
        <div className="flex flex-col gap-3 rounded border border-neutral-800 p-3">
          <h3 className="text-xs font-medium uppercase tracking-wider text-neutral-500">Auth fields</h3>
          <label className="flex flex-col gap-1">
            <span className="text-xs text-neutral-400">Email</span>
            <input value={String(values['email'] ?? '')} onChange={(e) => setValue('email', e.target.value)} type="email" className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm" />
          </label>
          {!isSuperusers && (
            <>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={Boolean(values['emailVisibility'])} onChange={(e) => setValue('emailVisibility', e.target.checked)} />
                <span className="text-neutral-400">Email visible</span>
              </label>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" checked={Boolean(values['verified'])} onChange={(e) => setValue('verified', e.target.checked)} />
                <span className="text-neutral-400">Verified</span>
              </label>
            </>
          )}
          <label className="flex flex-col gap-1">
            <span className="text-xs text-neutral-400">{isNew ? 'Password' : 'New password (leave blank to keep)'}</span>
            <input value={String(values['password'] ?? '')} onChange={(e) => setValue('password', e.target.value)} type="password" autoComplete="new-password" className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm" />
          </label>
        </div>
      )}

      {editableFields
        .filter((f) => !(isAuth && AUTH_SKIP.has(f.name)))
        .map((f) => (
          <SchemaField
            key={f.name}
            field={f}
            value={values[f.name]}
            onChange={(v) => setValue(f.name, v)}
            onFileChange={(files) => { fileUploads.current[f.name] = files ? Array.from(files) : [] }}
          />
        ))}

      {!isNew && record.data && collection.data!.fields
        .filter((f) => f.type === 'autodate')
        .map((f) => (
          <div key={f.name} className="flex flex-col gap-1">
            <span className="text-xs text-neutral-500">{f.name}</span>
            <span className="text-sm text-neutral-400">{String(record.data![f.name] ?? '-')}</span>
          </div>
        ))}
    </form>
  )
}

function SchemaField({ field, value, onChange, onFileChange }: {
  field: CollectionField
  value: unknown
  onChange: (v: unknown) => void
  onFileChange: (files: FileList | null) => void
}) {
  const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm'
  const lbl = (
    <span className="flex items-center gap-1 text-xs text-neutral-400">
      <span>{field.name}</span>
      <span className="text-neutral-600">({field.type})</span>
      {field.system && <span className="text-neutral-600">system</span>}
    </span>
  )

  switch (field.type) {
    case 'text': case 'email': case 'url':
      return <label className="flex flex-col gap-1">{lbl}<input value={String(value ?? '')} onChange={(e) => onChange(e.target.value)} type={field.type === 'email' ? 'email' : field.type === 'url' ? 'url' : 'text'} className={inputClass} /></label>
    case 'editor':
      return <label className="flex flex-col gap-1">{lbl}<textarea value={String(value ?? '')} onChange={(e) => onChange(e.target.value)} rows={6} className={inputClass} /></label>
    case 'number':
      return <label className="flex flex-col gap-1">{lbl}<input value={String(value ?? 0)} onChange={(e) => onChange(Number(e.target.value))} type="number" step="any" className={inputClass} /></label>
    case 'bool':
      return <label className="flex items-center gap-2 text-sm"><input type="checkbox" checked={Boolean(value)} onChange={(e) => onChange(e.target.checked)} />{lbl}</label>
    case 'select':
      return <label className="flex flex-col gap-1">{lbl}<input value={String(value ?? '')} onChange={(e) => onChange(e.target.value)} placeholder="value (or comma-separated)" className={inputClass} /></label>
    case 'date':
      return <label className="flex flex-col gap-1">{lbl}<input value={String(value ?? '')} onChange={(e) => onChange(e.target.value)} type="datetime-local" className={inputClass} /></label>
    case 'json':
      return <label className="flex flex-col gap-1">{lbl}<textarea value={String(value ?? '')} onChange={(e) => onChange(e.target.value)} rows={4} placeholder="{}" className={inputClass + ' font-mono'} /></label>
    case 'file':
      return <div className="flex flex-col gap-1">{lbl}<input type="file" multiple={Boolean((field as Record<string, unknown>).maxSelect !== 1)} onChange={(e) => onFileChange(e.target.files)} className="text-sm text-neutral-400" /></div>
    case 'relation':
      return <label className="flex flex-col gap-1">{lbl}<input value={String(value ?? '')} onChange={(e) => onChange(e.target.value)} placeholder="Record ID (or comma-separated)" className={inputClass + ' font-mono'} /></label>
    case 'password':
      return <label className="flex flex-col gap-1">{lbl}<input value={String(value ?? '')} onChange={(e) => onChange(e.target.value)} type="password" autoComplete="new-password" className={inputClass} /></label>
    case 'geoPoint':
      return <label className="flex flex-col gap-1">{lbl}<input value={String(value ?? '')} onChange={(e) => onChange(e.target.value)} placeholder='{"lon": 0, "lat": 0}' className={inputClass + ' font-mono'} /></label>
    default:
      return <label className="flex flex-col gap-1">{lbl}<input value={String(value ?? '')} onChange={(e) => onChange(e.target.value)} className={inputClass} /></label>
  }
}
