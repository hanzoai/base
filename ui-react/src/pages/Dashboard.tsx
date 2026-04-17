import { useQuery } from '@tanstack/react-query'
import { listCollections, listLogs } from '~/lib/api'

export function Dashboard() {
  const collections = useQuery({
    queryKey: ['collections'],
    queryFn: () => listCollections({ sort: 'name' }),
  })

  const logs = useQuery({
    queryKey: ['logs', 'tail'],
    queryFn: () => listLogs(1, 10, { sort: '-created' }),
  })

  return (
    <div className="flex flex-col gap-6">
      <header className="flex items-baseline justify-between">
        <h1 className="text-xl font-semibold">Dashboard</h1>
      </header>

      <section>
        <div className="mb-2 text-xs uppercase tracking-wider text-neutral-500">
          Collections ({collections.data?.length ?? 0})
        </div>
        <ul className="flex flex-wrap gap-2 text-sm">
          {collections.data?.map((c) => (
            <li key={c.id} className="rounded border border-neutral-800 px-3 py-1">
              {c.name}
            </li>
          ))}
        </ul>
      </section>

      <section>
        <div className="mb-2 text-xs uppercase tracking-wider text-neutral-500">
          Recent logs
        </div>
        <ul className="flex flex-col gap-1 text-xs font-mono">
          {logs.data?.items.map((l) => (
            <li key={l.id} className="flex gap-2 text-neutral-300">
              <span className="shrink-0 text-neutral-500">{l.created}</span>
              <span className="truncate">{l.message}</span>
            </li>
          ))}
        </ul>
      </section>
    </div>
  )
}
