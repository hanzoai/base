import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ChevronRight, Search } from 'lucide-react';
import { useState } from 'react';

import { Badge } from '~/components/ui/badge';
import { Button } from '~/components/ui/button';
import { Input } from '~/components/ui/input';
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
    <div className="flex flex-col gap-4">
      <header className="flex items-center gap-3">
        <h1 className="text-xl font-semibold">Collections</h1>
        <div className="relative ml-auto w-64">
          <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input value={ filter } onChange={ (e) => setFilter(e.target.value) } placeholder="Filter…" className="pl-8" />
        </div>
      </header>

      { list.isPending && <div className="text-sm text-muted-foreground">Loading…</div> }
      { list.error && <div className="text-sm text-destructive">{ String(list.error) }</div> }

      <div className="overflow-hidden rounded-lg border border-border">
        { filtered.map((c) => (
          <div
            key={ c.id }
            onClick={ () => nav({ to: '/collections/$id/records', params: { id: c.id } }) }
            className="flex cursor-pointer items-center gap-3 border-b border-border/60 px-4 py-3 last:border-0 hover:bg-accent/40"
          >
            <span className="font-medium">{ c.name }</span>
            <Badge variant={ c.type === 'view' ? 'outline' : 'default' }>{ c.type }</Badge>
            { c.system && <Badge variant="outline">system</Badge> }
            <div className="ml-auto flex items-center gap-3">
              { !c.system && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="text-destructive hover:text-destructive"
                  disabled={ del.isPending }
                  onClick={ (e) => {
                    e.stopPropagation();
                    if (confirm(`Delete collection "${c.name}"?`)) del.mutate(c.id);
                  } }
                >
                  Delete
                </Button>
              ) }
              <ChevronRight className="size-4 text-muted-foreground" />
            </div>
          </div>
        )) }
        { !list.isPending && filtered.length === 0 && (
          <div className="px-4 py-12 text-center text-sm text-muted-foreground">No collections.</div>
        ) }
      </div>
    </div>
  );
}

export const Route = createFileRoute('/collections')({
  beforeLoad: () => {
    if (!base.authStore.token) throw redirect({ to: '/login' });
  },
  component: Collections,
});
