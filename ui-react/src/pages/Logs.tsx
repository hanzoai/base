import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { listLogs } from '~/lib/api'

export function Logs() {
  const [filter, setFilter] = useState('')
  const [page, setPage] = useState(1)

  const logs = useQuery({
    queryKey: ['logs', page, filter],
    queryFn: () => listLogs(page, 50, {
      sort: '-created',
      filter: filter ? `message ~ "${filter.replace(/"/g, '\\"')}"` : undefined,
    }),
  })

  return (
    <div className="flex flex-col gap-4">
      <header className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">Logs</h1>
        <input
          value={filter}
          onChange={(e) => { setFilter(e.target.value); setPage(1) }}
          placeholder="Filter message..."
          className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm"
        />
      </header>

      <ul className="flex flex-col gap-1 font-mono text-xs">
        {logs.data?.items.map((l) => (
          <li key={l.id} className="flex gap-3 text-neutral-300">
            <span className="w-44 shrink-0 text-neutral-500">{l.created}</span>
            <span className={
              l.level === 'ERROR' ? 'text-red-400' :
                l.level === 'WARN' ? 'text-yellow-400' :
                  'text-neutral-300'
            }>{l.level}</span>
            <span className="truncate">{l.message}</span>
          </li>
        ))}
      </ul>

      <footer className="flex items-center gap-2 text-sm">
        <button
          onClick={() => setPage((p) => Math.max(1, p - 1))}
          disabled={page === 1}
          className="rounded border border-neutral-700 px-2 py-1 disabled:opacity-30"
        >Prev</button>
        <span className="text-neutral-400">page {page}</span>
        <button
          onClick={() => setPage((p) => p + 1)}
          disabled={!logs.data || logs.data.items.length < 50}
          className="rounded border border-neutral-700 px-2 py-1 disabled:opacity-30"
        >Next</button>
      </footer>
    </div>
  )
}
