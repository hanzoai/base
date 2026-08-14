import { createFileRoute, useNavigate } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ChevronRight, Search } from '@hanzogui/lucide-icons-2';
import { useState } from 'react';

import { requireSession } from '~/lib/guard';
import { base } from '~/lib/base';

function Collections() {
  const qc = useQueryClient();
  const nav = useNavigate();
  const [filter, setFilter] = useState('');

  const list = useQuery({
    queryKey: ['collections'],
    queryFn: () => base.collections.getFullList({ sort: 'name' }),
  });

  const del = useMutation({
    mutationFn: (id: string) => base.collections.delete(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ['collections'] }),
  });

  const filtered =
    list.data?.filter((c) => !filter || c.name.toLowerCase().includes(filter.toLowerCase())) ?? [];

  return (
    <div className="page">
      <header className="page__head">
        <h1 className="page__title">Collections</h1>
        <div className="search push">
          <Search size={ 16 } />
          <input
            value={ filter }
            onChange={ (e) => setFilter(e.target.value) }
            placeholder="Filter…"
            className="input"
          />
        </div>
      </header>

      { list.isPending && <div className="muted">Loading…</div> }
      { list.error && <div className="danger">{ String(list.error) }</div> }

      <div className="list">
        { filtered.map((c) => (
          <div
            key={ c.id }
            onClick={ () => nav({ to: '/collections/$id/records', params: { id: c.id } }) }
            className="list__row list__row--clickable"
          >
            <span>{ c.name }</span>
            <span className="tag">{ c.type }</span>
            { c.system && <span className="tag">system</span> }
            { !c.system && (
              <button
                type="button"
                className="link link--danger push"
                disabled={ del.isPending }
                onClick={ (e) => {
                  e.stopPropagation();
                  if (confirm(`Delete collection "${c.name}"?`)) del.mutate(c.id);
                } }
              >
                Delete
              </button>
            ) }
            <span className={ c.system ? "muted push" : "muted" }><ChevronRight size={ 16 } /></span>
          </div>
        )) }
        { !list.isPending && filtered.length === 0 && (
          <div className="empty">No collections.</div>
        ) }
      </div>
    </div>
  );
}

export const Route = createFileRoute('/collections')({
  beforeLoad: requireSession,
  component: Collections,
});
