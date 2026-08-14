import { createFileRoute } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';

import { requireSession } from '~/lib/guard';
import { base } from '~/lib/base';

// slog's levels, which is what `core.Log.Level` stores and sends — a number,
// not a name. Keying this by 'ERROR'/'WARN' meant no line was ever coloured and
// every line printed a bare integer.
const LEVELS = [
  { at: 8, name: 'ERROR', className: 'danger' },
  { at: 4, name: 'WARN', className: 'warn' },
  { at: 0, name: 'INFO', className: '' },
  { at: -4, name: 'DEBUG', className: 'muted' },
] as const;

function level(value: number) {
  return LEVELS.find((l) => value >= l.at) ?? LEVELS[LEVELS.length - 1];
}

function Logs() {
  const [ filter, setFilter ] = useState('');
  const [ page, setPage ] = useState(1);

  const logs = useQuery({
    queryKey: [ 'logs', page, filter ],
    queryFn: () => base.logs.getList(page, 50, {
      sort: '-created',
      filter: filter ? `message ~ "${ filter.replace(/"/g, '\\"') }"` : undefined,
    }),
  });

  // Realtime tail: subscribe to the logs collection. Newer entries appear
  // without a round-trip to the server.
  useEffect(() => {
    const unsub = base.logs.subscribe(() => {
      if (page === 1) logs.refetch();
    });
    return () => { void unsub.then((f) => f()); };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ page ]);

  return (
    <div className="page">
      <header className="page__head">
        <h1 className="page__title">Logs</h1>
        <div className="search push">
          <input
            value={ filter }
            onChange={ (e) => { setFilter(e.target.value); setPage(1); } }
            placeholder="Filter message…"
            className="input"
          />
        </div>
      </header>

      <ul className="stack stack--tight mono">
        { logs.data?.items.map((l) => (
          <li key={ l.id } className="row">
            <span className="muted num">{ l.created }</span>
            <span className={ level(l.level).className }>{ level(l.level).name }</span>
            <span className="truncate">{ l.message }</span>
          </li>
        )) }
      </ul>

      <footer className="row">
        <button
          type="button"
          onClick={ () => setPage((p) => Math.max(1, p - 1)) }
          disabled={ page === 1 }
          className="btn btn--outline btn--sm"
        >Prev</button>
        <span className="muted">page { page }</span>
        <button
          type="button"
          onClick={ () => setPage((p) => p + 1) }
          disabled={ !logs.data || logs.data.items.length < 50 }
          className="btn btn--outline btn--sm"
        >Next</button>
      </footer>
    </div>
  );
}

export const Route = createFileRoute('/logs')({
  beforeLoad: requireSession,
  component: Logs,
});
