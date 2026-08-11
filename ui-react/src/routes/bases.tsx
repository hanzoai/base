import { createFileRoute, redirect } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';

import { request } from '~/lib/api';
import { base } from '~/lib/base';

// A Base is what an org gets: its own SQLite file, its own key, opened the first
// time a request arrives carrying that org. So this page answers two questions
// and refuses to invent a third — which orgs you can reach, and whether each has
// been used yet. There is no create button because there is nothing to create:
// using an org opens its Base.
interface Base {
  org: string;
  exists: boolean;
  bytes?: number;
}

// Sizes here span a fresh file and a working store, so a fixed unit would print
// either 0 MB or seven digits. Powers of 1024, because that is what the
// filesystem reported.
function size(bytes: number): string {
  if (!bytes) return '—';
  const units = [ 'B', 'KB', 'MB', 'GB' ];
  let n = bytes;
  let u = 0;
  while (n >= 1024 && u < units.length - 1) { n /= 1024; u++; }
  return `${ n < 10 && u > 0 ? n.toFixed(1) : Math.round(n) } ${ units[u] }`;
}

function Bases() {
  const bases = useQuery({
    queryKey: [ 'bases' ],
    queryFn: () => request<Base[]>('/v1/bases'),
  });

  const rows = bases.data ?? [];
  const open = rows.filter((b) => b.exists).length;

  return (
    <div className="page">
      <header className="page__head">
        <h1 className="page__title">Bases</h1>
        <span className="chip push">
          { open } of { rows.length } open
        </span>
      </header>

      { bases.isError && (
        <p className="empty">Could not read your Bases: { String(bases.error) }</p>
      ) }

      { !bases.isError && !bases.isLoading && rows.length === 0 && (
        <p className="empty">
          Your account belongs to no organisation yet. A Base belongs to an org,
          so one appears here as soon as you join one.
        </p>
      ) }

      { rows.length > 0 && (
        <table className="table">
          <thead>
            <tr>
              <th>Organisation</th>
              <th>State</th>
              <th>Size</th>
            </tr>
          </thead>
          <tbody>
            { rows.map((b) => (
              <tr key={ b.org }>
                <td>{ b.org }</td>
                <td>
                  <span className={ b.exists ? 'chip' : 'chip chip--muted' }>
                    { b.exists ? 'Open' : 'Not used yet' }
                  </span>
                </td>
                <td>{ b.exists ? size(b.bytes ?? 0) : '—' }</td>
              </tr>
            )) }
          </tbody>
        </table>
      ) }

      <p className="hint">
        Each Base is a separate encrypted database. An org you have not used yet
        has none — it is opened on the first request that carries the org, so
        there is nothing to create here.
      </p>
    </div>
  );
}

export const Route = createFileRoute('/bases')({
  beforeLoad: () => {
    if (!base.authStore.token) throw redirect({ to: '/login' });
  },
  component: Bases,
});
