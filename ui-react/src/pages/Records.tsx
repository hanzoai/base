import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useCallback, useState } from 'react'
import { Link } from 'wouter'
import {
  deleteRecord,
  getCollection,
  listRecords,
  type CollectionField,
  type RecordModel,
} from '~/lib/api'

const PER_PAGE = 40

export function Records({ id }: { id: string }) {
  const qc = useQueryClient()
  const [page, setPage] = useState(1)
  const [sort, setSort] = useState('-created')
  const [filter, setFilter] = useState('')
  const [filterInput, setFilterInput] = useState('')
  const [bulkSelected, setBulkSelected] = useState<Set<string>>(new Set())

  const collection = useQuery({
    queryKey: ['collections', id],
    queryFn: () => getCollection(id),
  })

  const collectionName = collection.data?.name ?? id
  const isView = collection.data?.type === 'view'
  const visibleFields = (collection.data?.fields ?? []).filter((f: CollectionField) => !f.hidden)

  const records = useQuery({
    queryKey: ['records', collectionName, page, sort, filter],
    queryFn: () => listRecords(collectionName, page, PER_PAGE, { sort, filter: filter || undefined }),
    enabled: Boolean(collection.data),
  })

  const del = useMutation({
    mutationFn: (recordId: string) => deleteRecord(collectionName, recordId),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['records', collectionName] }),
  })

  const bulkDel = useMutation({
    mutationFn: async (ids: string[]) => {
      await Promise.all(ids.map((rid) => deleteRecord(collectionName, rid)))
    },
    onSuccess: () => {
      setBulkSelected(new Set())
      qc.invalidateQueries({ queryKey: ['records', collectionName] })
    },
  })

  const handleSort = useCallback((fieldName: string) => {
    setSort((prev) => {
      if (prev === fieldName) return `-${fieldName}`
      if (prev === `-${fieldName}`) return fieldName
      return `-${fieldName}`
    })
    setPage(1)
  }, [])

  const handleFilterSubmit = useCallback((e: React.FormEvent) => {
    e.preventDefault()
    setFilter(filterInput)
    setPage(1)
  }, [filterInput])

  const handleToggleSelect = useCallback((recordId: string) => {
    setBulkSelected((prev) => {
      const next = new Set(prev)
      if (next.has(recordId)) next.delete(recordId)
      else next.add(recordId)
      return next
    })
  }, [])

  const handleSelectAll = useCallback(() => {
    if (!records.data) return
    setBulkSelected((prev) => {
      const allIds = records.data.items.map((r: RecordModel) => r.id)
      const allSelected = allIds.every((rid: string) => prev.has(rid))
      if (allSelected) return new Set()
      return new Set(allIds)
    })
  }, [records.data])

  if (collection.isPending) return <div className="text-sm text-neutral-400">Loading collection...</div>
  if (collection.error) return <div className="text-sm text-red-400">{String(collection.error)}</div>

  const totalPages = records.data?.totalPages ?? 0
  const allSelected = records.data
    ? records.data.items.length > 0 && records.data.items.every((r: RecordModel) => bulkSelected.has(r.id))
    : false

  return (
    <div className="flex flex-col gap-4">
      <header className="flex items-center gap-3">
        <Link to={`/collections/${id}`} className="text-neutral-400 hover:text-neutral-200">
          {collection.data!.name}
        </Link>
        <span className="text-neutral-600">/</span>
        <h1 className="text-xl font-semibold">Records</h1>
        {!isView && (
          <Link
            to={`/collections/${id}/records/_new`}
            className="ml-auto rounded bg-indigo-600 px-3 py-1 text-sm hover:bg-indigo-500"
          >
            New record
          </Link>
        )}
      </header>

      <form onSubmit={handleFilterSubmit} className="flex gap-2">
        <input
          value={filterInput}
          onChange={(e) => setFilterInput(e.target.value)}
          placeholder='Filter... e.g. name = "test"'
          className="flex-1 rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm font-mono"
        />
        <button type="submit" className="rounded bg-neutral-800 px-3 py-1 text-sm hover:bg-neutral-700">
          Filter
        </button>
        {filter && (
          <button
            type="button"
            onClick={() => { setFilter(''); setFilterInput(''); setPage(1) }}
            className="text-xs text-neutral-400 hover:text-neutral-200"
          >
            Clear
          </button>
        )}
      </form>

      {bulkSelected.size > 0 && (
        <div className="flex items-center gap-3 rounded bg-neutral-900 px-3 py-2 text-sm">
          <span className="text-neutral-300">{bulkSelected.size} selected</span>
          <button onClick={() => setBulkSelected(new Set())} className="text-xs text-neutral-400 hover:text-neutral-200">Deselect</button>
          <button
            onClick={() => {
              const count = bulkSelected.size
              if (confirm(`Delete ${count} record${count > 1 ? 's' : ''}?`)) {
                bulkDel.mutate(Array.from(bulkSelected))
              }
            }}
            disabled={bulkDel.isPending}
            className="ml-auto text-xs text-red-400 hover:text-red-300 disabled:opacity-50"
          >
            {bulkDel.isPending ? 'Deleting...' : 'Delete selected'}
          </button>
        </div>
      )}

      {records.isPending && <div className="text-sm text-neutral-400">Loading records...</div>}
      {records.error && <div className="text-sm text-red-400">{String(records.error)}</div>}

      {records.data && (
        <div className="overflow-auto">
          <table className="w-full text-sm">
            <thead className="text-left text-xs uppercase tracking-wider text-neutral-500">
              <tr>
                {!isView && (
                  <th className="w-8 py-2">
                    <input type="checkbox" checked={allSelected} onChange={handleSelectAll} disabled={records.data.items.length === 0} />
                  </th>
                )}
                {visibleFields.map((f: CollectionField) => (
                  <th key={f.id} className="cursor-pointer select-none py-2" onClick={() => handleSort(f.name)}>
                    {f.name}{sort === f.name ? ' ^' : sort === `-${f.name}` ? ' v' : ''}
                  </th>
                ))}
                <th className="py-2 text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {records.data.items.map((record: RecordModel) => (
                <tr key={record.id} className="border-t border-neutral-800 hover:bg-neutral-900">
                  {!isView && (
                    <td className="py-2">
                      <input type="checkbox" checked={bulkSelected.has(record.id)} onChange={() => handleToggleSelect(record.id)} />
                    </td>
                  )}
                  {visibleFields.map((f: CollectionField) => (
                    <td key={f.id} className="max-w-xs truncate py-2 text-neutral-300">
                      <CellValue record={record} field={f} />
                    </td>
                  ))}
                  <td className="py-2 text-right">
                    <div className="flex items-center justify-end gap-2">
                      {!isView && (
                        <Link to={`/collections/${id}/records/${record.id}`} className="text-xs text-indigo-400 hover:text-indigo-300">
                          Edit
                        </Link>
                      )}
                      {!isView && (
                        <button
                          onClick={() => { if (confirm(`Delete record "${record.id}"?`)) del.mutate(record.id) }}
                          disabled={del.isPending}
                          className="text-xs text-red-400 hover:text-red-300 disabled:opacity-50"
                        >
                          Delete
                        </button>
                      )}
                    </div>
                  </td>
                </tr>
              ))}
              {records.data.items.length === 0 && (
                <tr>
                  <td colSpan={visibleFields.length + (isView ? 1 : 2)} className="py-8 text-center text-neutral-500">
                    No records found.
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      )}

      {totalPages > 1 && (
        <div className="flex items-center justify-center gap-2 text-sm">
          <button
            disabled={page <= 1}
            onClick={() => setPage((p) => Math.max(1, p - 1))}
            className="rounded bg-neutral-800 px-2 py-1 hover:bg-neutral-700 disabled:opacity-50"
          >
            Prev
          </button>
          <span className="text-neutral-400">Page {page} of {totalPages}</span>
          <button
            disabled={page >= totalPages}
            onClick={() => setPage((p) => p + 1)}
            className="rounded bg-neutral-800 px-2 py-1 hover:bg-neutral-700 disabled:opacity-50"
          >
            Next
          </button>
        </div>
      )}
    </div>
  )
}

function CellValue({ record, field }: { record: RecordModel; field: { name: string; type: string } }) {
  const val = record[field.name]
  if (val === null || val === undefined) return <span className="text-neutral-600">-</span>
  switch (field.type) {
    case 'bool': return <span>{val ? 'true' : 'false'}</span>
    case 'json': return <span className="font-mono text-xs">{JSON.stringify(val).slice(0, 80)}</span>
    case 'file': return Array.isArray(val) ? <span>{val.length} file(s)</span> : <span>{String(val)}</span>
    case 'relation': return Array.isArray(val) ? <span>{val.join(', ')}</span> : <span>{String(val)}</span>
    default: return <span>{String(val)}</span>
  }
}
