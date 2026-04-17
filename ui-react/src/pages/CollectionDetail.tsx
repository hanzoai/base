import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useMemo, useState, type ReactNode } from 'react'
import { useLocation } from 'wouter'
import {
  deleteCollection,
  getCollection,
  updateCollection,
  type CollectionField,
  type CollectionModel,
} from '~/lib/api'

type Tab = 'fields' | 'indexes' | 'rules'

const FIELD_TYPES = [
  'text', 'number', 'bool', 'email', 'url', 'editor',
  'date', 'select', 'json', 'file', 'relation', 'password',
  'autodate', 'geoPoint',
] as const

interface FieldEntry extends CollectionField {
  _toDelete?: boolean
}

interface FormValues {
  name: string
  type: string
  fields: FieldEntry[]
  indexes: string[]
  listRule: string
  viewRule: string
  createRule: string
  updateRule: string
  deleteRule: string
}

function toFormValues(c: CollectionModel): FormValues {
  return {
    name: c.name,
    type: c.type,
    fields: (c.fields ?? []).map((f) => ({ ...f })),
    indexes: c.indexes ?? [],
    listRule: c.listRule ?? '',
    viewRule: c.viewRule ?? '',
    createRule: c.createRule ?? '',
    updateRule: c.updateRule ?? '',
    deleteRule: c.deleteRule ?? '',
  }
}

export function CollectionDetail({ id }: { id: string }) {
  const qc = useQueryClient()
  const [, navigate] = useLocation()
  const [activeTab, setActiveTab] = useState<Tab>('fields')

  const collection = useQuery({
    queryKey: ['collections', id],
    queryFn: () => getCollection(id),
  })

  const [form, setForm] = useState<FormValues | null>(null)
  const [dirty, setDirty] = useState(false)

  const values = useMemo(() => {
    if (form) return form
    if (collection.data) return toFormValues(collection.data)
    return null
  }, [form, collection.data])

  // Reset form when collection loads and no local edits
  if (collection.data && !form && !dirty) {
    // handled by the memo above
  }

  function update(patch: Partial<FormValues>) {
    setForm({ ...(values ?? toFormValues(collection.data!)), ...patch })
    setDirty(true)
  }

  function updateField(idx: number, patch: Partial<FieldEntry>) {
    if (!values) return
    const fields = [...values.fields]
    fields[idx] = { ...fields[idx], ...patch }
    update({ fields })
  }

  const save = useMutation({
    mutationFn: (data: FormValues) => {
      const payload = { ...data, fields: data.fields.filter((f) => !f._toDelete) }
      return updateCollection(id, payload)
    },
    onSuccess: () => {
      setDirty(false)
      setForm(null)
      qc.invalidateQueries({ queryKey: ['collections'] })
      qc.invalidateQueries({ queryKey: ['collections', id] })
    },
  })

  const del = useMutation({
    mutationFn: () => deleteCollection(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['collections'] })
      navigate('/collections')
    },
  })

  const handleSave = useCallback(() => {
    if (values) save.mutate(values)
  }, [values, save])

  if (collection.isPending) return <div className="text-sm text-neutral-400">Loading...</div>
  if (collection.error) return <div className="text-sm text-red-400">{String(collection.error)}</div>
  if (!values) return null

  const isSystem = collection.data?.system ?? false
  const isView = collection.data?.type === 'view'

  return (
    <div className="flex flex-col gap-4">
      <header className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">Edit collection</h1>
        <input
          value={values.name}
          onChange={(e) => update({ name: e.target.value })}
          disabled={isSystem}
          className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm font-medium"
        />
        <span className="rounded bg-neutral-800 px-2 py-0.5 text-xs text-neutral-400">
          {collection.data?.type}
        </span>
        <div className="ml-auto flex gap-2">
          <button
            onClick={() => navigate(`/collections/${id}/records`)}
            className="rounded border border-neutral-700 px-3 py-1 text-sm hover:bg-neutral-800"
          >
            Records
          </button>
          {!isSystem && (
            <button
              onClick={() => { if (confirm(`Delete collection "${collection.data?.name}"?`)) del.mutate() }}
              disabled={del.isPending}
              className="rounded bg-red-900/50 px-3 py-1 text-sm text-red-300 hover:bg-red-900 disabled:opacity-50"
            >
              Delete
            </button>
          )}
          <button
            onClick={handleSave}
            disabled={!dirty || save.isPending}
            className="rounded bg-indigo-600 px-3 py-1 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50"
          >
            {save.isPending ? 'Saving...' : 'Save'}
          </button>
        </div>
      </header>

      {save.error && <div className="text-sm text-red-400">{String(save.error)}</div>}

      <div className="flex gap-1 border-b border-neutral-800">
        <TabButton active={activeTab === 'fields'} onClick={() => setActiveTab('fields')}>
          {isView ? 'Query' : 'Fields'}
        </TabButton>
        <TabButton active={activeTab === 'indexes'} onClick={() => setActiveTab('indexes')}>
          Indexes ({values.indexes.length})
        </TabButton>
        <TabButton active={activeTab === 'rules'} onClick={() => setActiveTab('rules')}>
          API Rules
        </TabButton>
      </div>

      {activeTab === 'fields' && (
        <div className="flex flex-col gap-2">
          {values.fields.map((field, idx) => {
            if (field._toDelete) return null
            return (
              <div key={field.id || idx} className="flex items-center gap-2 rounded border border-neutral-800 p-2">
                <input
                  value={field.name}
                  onChange={(e) => updateField(idx, { name: e.target.value })}
                  disabled={field.system}
                  placeholder="Field name"
                  className="w-40 rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm"
                />
                <select
                  value={field.type}
                  onChange={(e) => updateField(idx, { type: e.target.value })}
                  disabled={field.system}
                  className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm"
                >
                  {FIELD_TYPES.map((t) => <option key={t} value={t}>{t}</option>)}
                </select>
                <label className="flex items-center gap-1 text-xs text-neutral-400">
                  <input
                    type="checkbox"
                    checked={field.hidden}
                    onChange={(e) => updateField(idx, { hidden: e.target.checked })}
                    disabled={field.system}
                  />
                  Hidden
                </label>
                <label className="flex items-center gap-1 text-xs text-neutral-400">
                  <input
                    type="checkbox"
                    checked={field.presentable}
                    onChange={(e) => updateField(idx, { presentable: e.target.checked })}
                    disabled={field.system}
                  />
                  Presentable
                </label>
                {field.system && <span className="text-xs text-neutral-500">system</span>}
                {!field.system && (
                  <button
                    onClick={() => {
                      if (field.id) {
                        updateField(idx, { _toDelete: true })
                      } else {
                        const fields = values.fields.filter((_, i) => i !== idx)
                        update({ fields })
                      }
                    }}
                    className="ml-auto text-xs text-red-400 hover:text-red-300"
                  >
                    Remove
                  </button>
                )}
              </div>
            )
          })}
          {!isView && (
            <button
              onClick={() => {
                const name = `field${values.fields.length + 1}`
                update({
                  fields: [...values.fields, {
                    id: '', name, type: 'text', system: false, hidden: false, presentable: false,
                  }],
                })
              }}
              className="w-full rounded border border-dashed border-neutral-700 py-2 text-sm text-neutral-400 hover:border-neutral-500 hover:text-neutral-200"
            >
              + Add field
            </button>
          )}
        </div>
      )}

      {activeTab === 'indexes' && (
        <IndexesPanel indexes={values.indexes} onChange={(indexes) => update({ indexes })} />
      )}

      {activeTab === 'rules' && (
        <div className="flex flex-col gap-3">
          <RuleField label="List/Search rule" value={values.listRule} onChange={(v) => update({ listRule: v })} />
          <RuleField label="View rule" value={values.viewRule} onChange={(v) => update({ viewRule: v })} />
          {!isView && (
            <>
              <RuleField label="Create rule" value={values.createRule} onChange={(v) => update({ createRule: v })} />
              <RuleField label="Update rule" value={values.updateRule} onChange={(v) => update({ updateRule: v })} />
              <RuleField label="Delete rule" value={values.deleteRule} onChange={(v) => update({ deleteRule: v })} />
            </>
          )}
          <p className="text-xs text-neutral-500">
            Leave a rule empty to require superuser access. Use filter syntax to restrict access.
          </p>
        </div>
      )}
    </div>
  )
}

function TabButton({ active, onClick, children }: { active: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`px-3 py-2 text-sm ${
        active ? 'border-b-2 border-indigo-500 text-neutral-100' : 'text-neutral-400 hover:text-neutral-200'
      }`}
    >
      {children}
    </button>
  )
}

function RuleField({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-xs text-neutral-400">{label}</span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder='e.g. @request.auth.id != ""'
        className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm font-mono"
      />
    </label>
  )
}

function IndexesPanel({ indexes, onChange }: { indexes: string[]; onChange: (v: string[]) => void }) {
  const [draft, setDraft] = useState('')

  return (
    <div className="flex flex-col gap-2">
      {indexes.map((idx, i) => (
        <div key={i} className="flex items-center gap-2 text-sm">
          <code className="flex-1 rounded bg-neutral-900 px-2 py-1 font-mono text-xs">{idx}</code>
          <button onClick={() => onChange(indexes.filter((_, j) => j !== i))} className="text-xs text-red-400 hover:text-red-300">Remove</button>
        </div>
      ))}
      <div className="flex gap-2">
        <input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          placeholder="CREATE INDEX idx_name ON tablename (column)"
          className="flex-1 rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm font-mono"
        />
        <button
          onClick={() => { if (draft.trim()) { onChange([...indexes, draft.trim()]); setDraft('') } }}
          className="rounded bg-neutral-800 px-3 py-1 text-sm hover:bg-neutral-700"
        >
          Add
        </button>
      </div>
    </div>
  )
}
