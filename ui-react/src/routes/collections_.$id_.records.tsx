import { Link, createFileRoute, redirect, useNavigate } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus, Search, Trash2, X } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';

import { RecordGrid } from '~/components/grid/RecordGrid';
import { Button } from '~/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '~/components/ui/dialog';
import { Input } from '~/components/ui/input';
import { base } from '~/lib/base';
import type { CollectionField, ListResult, RecordModel } from '~/lib/base';

const PER_PAGE = 50;
const SYSTEM_KEYS = new Set(['id', 'created', 'updated', 'collectionId', 'collectionName', 'expand']);

function RecordsList() {
  const { id } = Route.useParams();
  const nav = useNavigate();
  const qc = useQueryClient();

  const [page, setPage] = useState(1);
  // Empty default = API insertion order, valid for any schema (base collections
  // in this fork have no `created` field). Sorting is opt-in via column headers.
  const [sort, setSort] = useState('');
  const [filter, setFilter] = useState('');
  const [filterInput, setFilterInput] = useState('');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [confirm, setConfirm] = useState<{ ids: string[]; label: string } | null>(null);

  const collection = useQuery({
    queryKey: ['collections', id],
    queryFn: () => base.collections.getOne(id),
  });

  const name = collection.data?.name ?? id;
  const isView = collection.data?.type === 'view';
  const fields = useMemo<CollectionField[]>(
    () => (collection.data?.fields ?? []).filter((f) => !f.hidden && f.name !== 'id'),
    [collection.data],
  );

  const listKey = ['records', name, page, sort, filter] as const;
  const records = useQuery({
    queryKey: listKey,
    queryFn: () => base.collection(name).getList(page, PER_PAGE, { sort, filter }),
    enabled: Boolean(collection.data),
  });

  // Optimistic single-field cell edit.
  const patch = useMutation({
    mutationFn: ({ record, data }: { record: RecordModel; data: Record<string, unknown> }) =>
      base.collection(name).update(record.id, data),
    onMutate: async ({ record, data }) => {
      await qc.cancelQueries({ queryKey: listKey });
      const prev = qc.getQueryData<ListResult<RecordModel>>(listKey);
      qc.setQueryData<ListResult<RecordModel>>(listKey, (old) =>
        old
          ? { ...old, items: old.items.map((it) => (it.id === record.id ? { ...it, ...data } : it)) }
          : old,
      );
      return { prev };
    },
    onError: (_e, _v, ctx) => {
      if (ctx?.prev) qc.setQueryData(listKey, ctx.prev);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ['records', name] }),
  });

  const duplicate = useMutation({
    mutationFn: (record: RecordModel) => {
      const copy: Record<string, unknown> = {};
      for (const [k, v] of Object.entries(record)) if (!SYSTEM_KEYS.has(k)) copy[k] = v;
      return base.collection(name).create(copy);
    },
    onSuccess: () => qc.invalidateQueries({ queryKey: ['records', name] }),
  });

  const del = useMutation({
    mutationFn: (ids: string[]) => Promise.all(ids.map((rid) => base.collection(name).delete(rid))),
    onSuccess: () => {
      setSelected(new Set());
      setConfirm(null);
      qc.invalidateQueries({ queryKey: ['records', name] });
    },
  });

  const onCommitCell = useCallback(
    (record: RecordModel, field: CollectionField, value: unknown) =>
      patch.mutate({ record, data: { [field.name]: value } }),
    [patch],
  );

  const onSort = useCallback((fieldName: string) => {
    setSort((prev) =>
      prev === fieldName ? `-${fieldName}` : prev === `-${fieldName}` ? fieldName : fieldName,
    );
    setPage(1);
  }, []);

  const toggleSelect = useCallback((rid: string) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(rid)) next.delete(rid);
      else next.add(rid);
      return next;
    });
  }, []);

  const toggleAll = useCallback(() => {
    const items = records.data?.items ?? [];
    setSelected((prev) =>
      items.length > 0 && items.every((r) => prev.has(r.id)) ? new Set() : new Set(items.map((r) => r.id)),
    );
  }, [records.data]);

  const openRecord = useCallback(
    (record: RecordModel) =>
      nav({ to: '/collections/$id/records/$recordId', params: { id, recordId: record.id } }),
    [nav, id],
  );

  if (collection.isPending) return <Muted>Loading collection…</Muted>;
  if (collection.error) return <ErrorText error={ collection.error } />;

  const total = records.data?.totalItems ?? 0;
  const totalPages = records.data?.totalPages ?? 0;

  return (
    <div className="flex flex-col gap-4">
      <header className="flex items-center gap-2">
        <Link to="/collections/$id" params={{ id }} className="text-muted-foreground hover:text-foreground">
          { name }
        </Link>
        <span className="text-muted-foreground/40">/</span>
        <h1 className="text-xl font-semibold">Records</h1>
        <span className="text-sm text-muted-foreground">{ total }</span>
        { !isView && (
          <Button
            size="sm"
            className="ml-auto"
            onClick={ () => nav({ to: '/collections/$id/records/$recordId', params: { id, recordId: '_new' } }) }
          >
            <Plus /> New record
          </Button>
        ) }
      </header>

      <form
        onSubmit={ (e) => { e.preventDefault(); setFilter(filterInput); setPage(1); } }
        className="flex items-center gap-2"
      >
        <div className="relative flex-1">
          <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={ filterInput }
            onChange={ (e) => setFilterInput(e.target.value) }
            placeholder={ 'Filter — e.g. status = "done"' }
            className="pl-8 font-mono"
          />
        </div>
        <Button type="submit" variant="secondary" size="sm">Filter</Button>
        { filter && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            onClick={ () => { setFilter(''); setFilterInput(''); setPage(1); } }
          >
            <X /> Clear
          </Button>
        ) }
      </form>

      { selected.size > 0 && (
        <div className="flex items-center gap-3 rounded-md border border-border bg-card px-3 py-2 text-sm">
          <span className="text-muted-foreground">{ selected.size } selected</span>
          <Button variant="ghost" size="sm" onClick={ () => setSelected(new Set()) }>Deselect</Button>
          <Button
            variant="ghost"
            size="sm"
            className="ml-auto text-destructive hover:text-destructive"
            onClick={ () => setConfirm({ ids: [...selected], label: `${selected.size} record(s)` }) }
          >
            <Trash2 /> Delete selected
          </Button>
        </div>
      ) }

      { records.isPending && <Muted>Loading records…</Muted> }
      { records.error && <ErrorText error={ records.error } /> }

      { records.data && (
        <RecordGrid
          fields={ fields }
          records={ records.data.items }
          sort={ sort }
          onSort={ onSort }
          selected={ selected }
          onToggleSelect={ toggleSelect }
          onToggleAll={ toggleAll }
          onCommitCell={ onCommitCell }
          onEditRecord={ openRecord }
          onDuplicate={ (r) => duplicate.mutate(r) }
          onDelete={ (r) => setConfirm({ ids: [r.id], label: `record "${r.id}"` }) }
          isView={ isView }
        />
      ) }

      { totalPages > 1 && (
        <div className="flex items-center justify-center gap-3 text-sm">
          <Button variant="outline" size="sm" disabled={ page <= 1 } onClick={ () => setPage((p) => p - 1) }>
            Prev
          </Button>
          <span className="text-muted-foreground">Page { page } of { totalPages }</span>
          <Button variant="outline" size="sm" disabled={ page >= totalPages } onClick={ () => setPage((p) => p + 1) }>
            Next
          </Button>
        </div>
      ) }

      <Dialog open={ confirm !== null } onOpenChange={ (o) => { if (!o) setConfirm(null); } }>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>Delete { confirm?.label }?</DialogTitle>
            <DialogDescription>This cannot be undone.</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={ () => setConfirm(null) }>Cancel</Button>
            <Button
              variant="destructive"
              disabled={ del.isPending }
              onClick={ () => confirm && del.mutate(confirm.ids) }
            >
              { del.isPending ? 'Deleting…' : 'Delete' }
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function Muted({ children }: { children: React.ReactNode }) {
  return <div className="text-sm text-muted-foreground">{ children }</div>;
}

function ErrorText({ error }: { error: unknown }) {
  return <div className="text-sm text-destructive">{ String(error) }</div>;
}

export const Route = createFileRoute('/collections_/$id_/records')({
  beforeLoad: () => {
    if (!base.authStore.token) throw redirect({ to: '/login' });
  },
  component: RecordsList,
});
