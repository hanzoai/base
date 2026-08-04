import { createFileRoute, redirect } from '@tanstack/react-router';
import { useQuery } from '@tanstack/react-query';

import { base } from '~/lib/base';

function Dashboard() {
  const collections = useQuery({
    queryKey: [ 'collections' ],
    queryFn: () => base.collections.getFullList({ sort: 'name' }),
  });

  const logs = useQuery({
    queryKey: [ 'logs', 'tail' ],
    queryFn: () => base.logs.getList(1, 10, { sort: '-created' }),
  });

  return (
    <div className="page">
      <header className="page__head">
        <h1 className="page__title">Dashboard</h1>
      </header>

      <section className="stack stack--tight">
        <div className="eyebrow">Collections ({ collections.data?.length ?? 0 })</div>
        <ul className="row row--wrap">
          { collections.data?.map((c) => (
            <li key={ c.id } className="tag">{ c.name }</li>
          )) }
        </ul>
      </section>

      <section className="stack stack--tight">
        <div className="eyebrow">Recent logs</div>
        <ul className="stack stack--tight mono">
          { logs.data?.items.map((l) => (
            <li key={ l.id } className="row">
              <span className="muted num">{ l.created }</span>
              <span className="truncate">{ l.message }</span>
            </li>
          )) }
        </ul>
      </section>
    </div>
  );
}

export const Route = createFileRoute('/')({
  beforeLoad: () => {
    if (!base.authStore.token) throw redirect({ to: '/login' });
  },
  component: Dashboard,
});
