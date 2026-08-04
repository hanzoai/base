import { createFileRoute, redirect } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';
import { useEffect, useState } from 'react';

import { base } from '~/lib/base';

// A log line's level is the one place this admin colours by meaning rather than
// by rank, so it maps to the design system's state hues, not to a palette.
const LEVEL_CLASS: Record<string, string> = { ERROR: 'danger', WARN: 'warn' };

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
            <span className={ LEVEL_CLASS[l.level] ?? '' }>{ l.level }</span>
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
  beforeLoad: () => {
    if (!base.authStore.token) throw redirect({ to: '/login' });
  },
  component: Logs,
});
