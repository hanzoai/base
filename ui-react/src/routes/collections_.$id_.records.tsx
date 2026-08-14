import { Link, createFileRoute, useNavigate } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Plus, Search, Trash2, X } from '@hanzogui/lucide-icons-2';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@hanzo/ui';
import { useCallback, useMemo, useState } from 'react';

import { RecordGrid } from '~/components/grid/RecordGrid';
import { requireSession } from '~/lib/guard';
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

  if (collection.isPending) return <div className="muted">Loading collection…</div>;
  if (collection.error) return <div className="danger">{ String(collection.error) }</div>;

  const total = records.data?.totalItems ?? 0;
  const totalPages = records.data?.totalPages ?? 0;

  return (
    <div className="page">
      <header className="page__head">
        <Link to="/collections/$id" params={{ id }} className="muted">{ name }</Link>
        <span className="muted">/</span>
        <h1 className="page__title">Records</h1>
        <span className="muted">{ total }</span>
        { !isView && (
          <button
            type="button"
            className="btn btn--sm push"
            onClick={ () => nav({ to: '/collections/$id/records/$recordId', params: { id, recordId: '_new' } }) }
          >
            <Plus size={ 14 } /> New record
          </button>
        ) }
      </header>

      <form
        onSubmit={ (e) => { e.preventDefault(); setFilter(filterInput); setPage(1); } }
        className="row"
      >
        <div className="search grow">
          <Search size={ 16 } />
          <input
            value={ filterInput }
            onChange={ (e) => setFilterInput(e.target.value) }
            placeholder={ 'Filter — e.g. status = "done"' }
            className="input input--mono"
          />
        </div>
        <button type="submit" className="btn btn--outline btn--sm">Filter</button>
        { filter && (
          <button
            type="button"
            className="btn btn--ghost btn--sm"
            onClick={ () => { setFilter(''); setFilterInput(''); setPage(1); } }
          >
            <X size={ 14 } /> Clear
          </button>
        ) }
      </form>

      { selected.size > 0 && (
        <div className="list__row">
          <span className="muted">{ selected.size } selected</span>
          <button type="button" className="btn btn--ghost btn--sm" onClick={ () => setSelected(new Set()) }>
            Deselect
          </button>
          <button
            type="button"
            className="btn btn--ghost btn--sm push danger"
            onClick={ () => setConfirm({ ids: [...selected], label: `${selected.size} record(s)` }) }
          >
            <Trash2 size={ 14 } /> Delete selected
          </button>
        </div>
      ) }

      { records.isPending && <div className="muted">Loading records…</div> }
      { records.error && <div className="danger">{ String(records.error) }</div> }

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
        <div className="row" style={{ justifyContent: 'center' }}>
          <button type="button" className="btn btn--outline btn--sm" disabled={ page <= 1 } onClick={ () => setPage((p) => p - 1) }>
            Prev
          </button>
          <span className="muted">Page { page } of { totalPages }</span>
          <button type="button" className="btn btn--outline btn--sm" disabled={ page >= totalPages } onClick={ () => setPage((p) => p + 1) }>
            Next
          </button>
        </div>
      ) }

      <Dialog open={ confirm !== null } onOpenChange={ (o: boolean) => { if (!o) setConfirm(null); } }>
        <DialogContent maxW={ 384 }>
          <DialogHeader>
            <DialogTitle>Delete { confirm?.label }?</DialogTitle>
            <DialogDescription>This cannot be undone.</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <button type="button" className="btn btn--ghost" onClick={ () => setConfirm(null) }>Cancel</button>
            <button
              type="button"
              className="btn btn--danger"
              disabled={ del.isPending }
              onClick={ () => confirm && del.mutate(confirm.ids) }
            >
              { del.isPending ? 'Deleting…' : 'Delete' }
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export const Route = createFileRoute('/collections_/$id_/records')({
  beforeLoad: requireSession,
  component: RecordsList,
});
