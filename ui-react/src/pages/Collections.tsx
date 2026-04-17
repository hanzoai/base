import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useLocation } from 'wouter'
import { deleteCollection, listCollections } from '~/lib/api'

export function Collections() {
  const qc = useQueryClient()
  const [, navigate] = useLocation()
  const [filter, setFilter] = useState('')

  const list = useQuery({
    queryKey: ['collections'],
    queryFn: () => listCollections({ sort: 'name' }),
  })

  const del = useMutation({
    mutationFn: (id: string) => deleteCollection(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['collections'] }),
  })

  const filtered = list.data?.filter(
    (c) => !filter || c.name.toLowerCase().includes(filter.toLowerCase()),
  ) ?? []

  return (
    <div className="flex flex-col gap-4">
      <header className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">Collections</h1>
        <input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Filter..."
          className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm"
        />
        <button
          onClick={() => navigate('/collections')}
          className="ml-auto rounded bg-indigo-600 px-3 py-1 text-sm hover:bg-indigo-500"
        >
          New collection
        </button>
      </header>

      {list.isPending && <div className="text-sm text-neutral-400">Loading...</div>}
      {list.error && <div className="text-sm text-red-400">{String(list.error)}</div>}

      <table className="w-full text-sm">
        <thead className="text-left text-xs uppercase tracking-wider text-neutral-500">
          <tr>
            <th className="py-2">Name</th>
            <th className="py-2">Type</th>
            <th className="py-2">System</th>
            <th className="py-2" />
          </tr>
        </thead>
        <tbody>
          {filtered.map((c) => (
            <tr
              key={c.id}
              className="cursor-pointer border-t border-neutral-800 hover:bg-neutral-900"
              onClick={() => navigate(`/collections/${c.id}`)}
            >
              <td className="py-2 font-medium">{c.name}</td>
              <td className="py-2 text-neutral-400">{c.type}</td>
              <td className="py-2 text-neutral-400">{c.system ? 'yes' : 'no'}</td>
              <td className="py-2 text-right">
                <button
                  onClick={(e) => {
                    e.stopPropagation()
                    if (confirm(`Delete collection "${c.name}"?`)) del.mutate(c.id)
                  }}
                  disabled={c.system || del.isPending}
                  className="text-xs text-red-400 hover:text-red-300 disabled:text-neutral-600"
                >
                  Delete
                </button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}
