import { createFileRoute, Link, redirect } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback, useEffect, useRef, useState } from 'react';

import { base } from '~/lib/base';

import type { RecordModel } from 'pocketbase';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const PER_PAGE = 40;

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

function RecordsList() {
    const { id } = Route.useParams();
    const qc = useQueryClient();

    const [page, setPage] = useState(1);
    const [sort, setSort] = useState('-created');
    const [filter, setFilter] = useState('');
    const [filterInput, setFilterInput] = useState('');
    const [bulkSelected, setBulkSelected] = useState<Set<string>>(new Set());

    // Track subscription cleanup
    const unsubRef = useRef<(() => void) | null>(null);

    // Fetch collection metadata
    const collection = useQuery({
        queryKey: ['collections', id],
        queryFn: () => base.collections.getOne(id),
    });

    const collectionName = collection.data?.name ?? id;
    const isView = collection.data?.type === 'view';
    const visibleFields = (collection.data?.fields ?? []).filter((f) => !f.hidden);

    // Fetch records
    const records = useQuery({
        queryKey: ['records', collectionName, page, sort, filter],
        queryFn: () => base.collection(collectionName).getList(page, PER_PAGE, { sort, filter }),
        enabled: Boolean(collection.data),
    });

    // Realtime subscription — auto-refresh on inserts/updates/deletes
    useEffect(() => {
        if (!collection.data) return;

        const name = collection.data.name;
        let cancelled = false;

        base.collection(name).subscribe('*', () => {
            if (!cancelled) {
                qc.invalidateQueries({ queryKey: ['records', name] });
            }
        }).then((unsub) => {
            if (cancelled) {
                unsub();
            } else {
                unsubRef.current = unsub;
            }
        });

        return () => {
            cancelled = true;
            unsubRef.current?.();
            unsubRef.current = null;
        };
    }, [collection.data?.name, qc]);

    // Delete mutation
    const del = useMutation({
        mutationFn: (recordId: string) => base.collection(collectionName).delete(recordId),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['records', collectionName] });
        },
    });

    // Bulk delete
    const bulkDel = useMutation({
        mutationFn: async (ids: string[]) => {
            await Promise.all(ids.map((rid) => base.collection(collectionName).delete(rid)));
        },
        onSuccess: () => {
            setBulkSelected(new Set());
            qc.invalidateQueries({ queryKey: ['records', collectionName] });
        },
    });

    // Handlers
    const handleSort = useCallback((fieldName: string) => {
        setSort((prev) => {
            if (prev === fieldName) return `-${fieldName}`;
            if (prev === `-${fieldName}`) return fieldName;
            return `-${fieldName}`;
        });
        setPage(1);
    }, []);

    const handleFilterSubmit = useCallback((e: React.FormEvent) => {
        e.preventDefault();
        setFilter(filterInput);
        setPage(1);
    }, [filterInput]);

    const handleToggleSelect = useCallback((recordId: string) => {
        setBulkSelected((prev) => {
            const next = new Set(prev);
            if (next.has(recordId)) {
                next.delete(recordId);
            } else {
                next.add(recordId);
            }
            return next;
        });
    }, []);

    const handleSelectAll = useCallback(() => {
        if (!records.data) return;
        setBulkSelected((prev) => {
            const allIds = records.data.items.map((r) => r.id);
            const allSelected = allIds.every((rid) => prev.has(rid));
            if (allSelected) return new Set();
            return new Set(allIds);
        });
    }, [records.data]);

    const handleBulkDelete = useCallback(() => {
        const count = bulkSelected.size;
        if (!count) return;
        if (confirm(`Delete ${count} record${count > 1 ? 's' : ''}?`)) {
            bulkDel.mutate(Array.from(bulkSelected));
        }
    }, [bulkSelected, bulkDel]);

    const handleDeleteRecord = useCallback((record: RecordModel) => {
        if (confirm(`Delete record "${record.id}"?`)) {
            del.mutate(record.id);
        }
    }, [del]);

    // Loading / error
    if (collection.isPending) return <div className="text-sm text-neutral-400">Loading collection...</div>;
    if (collection.error) return <div className="text-sm text-red-400">{String(collection.error)}</div>;

    const totalPages = records.data?.totalPages ?? 0;
    const allSelected = records.data
        ? records.data.items.length > 0 && records.data.items.every((r) => bulkSelected.has(r.id))
        : false;

    return (
        <div className="flex flex-col gap-4">
            {/* Header */}
            <header className="flex items-center gap-3">
                <Link
                    to="/collections/$id"
                    params={{ id }}
                    className="text-neutral-400 hover:text-neutral-200"
                >
                    {collection.data.name}
                </Link>
                <span className="text-neutral-600">/</span>
                <h1 className="text-xl font-semibold">Records</h1>
                {!isView && (
                    <Link
                        to="/collections/$id/records/$recordId"
                        params={{ id, recordId: '_new' }}
                        className="ml-auto rounded bg-indigo-600 px-3 py-1 text-sm hover:bg-indigo-500"
                    >
                        New record
                    </Link>
                )}
            </header>

            {/* Filter bar */}
            <form onSubmit={handleFilterSubmit} className="flex gap-2">
                <input
                    value={filterInput}
                    onChange={(e) => setFilterInput(e.target.value)}
                    placeholder='Filter... e.g. name = "test"'
                    className="flex-1 rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm font-mono"
                />
                <button type="submit" className="rounded bg-neutral-800 px-3 py-1 text-sm hover:bg-neutral-700">
                    Filter
                </button>
                {filter && (
                    <button
                        type="button"
                        onClick={() => { setFilter(''); setFilterInput(''); setPage(1); }}
                        className="text-xs text-neutral-400 hover:text-neutral-200"
                    >
                        Clear
                    </button>
                )}
            </form>

            {/* Bulk action bar */}
            {bulkSelected.size > 0 && (
                <div className="flex items-center gap-3 rounded bg-neutral-900 px-3 py-2 text-sm">
                    <span className="text-neutral-300">
                        {bulkSelected.size} selected
                    </span>
                    <button
                        type="button"
                        onClick={() => setBulkSelected(new Set())}
                        className="text-xs text-neutral-400 hover:text-neutral-200"
                    >
                        Deselect
                    </button>
                    <button
                        type="button"
                        onClick={handleBulkDelete}
                        disabled={bulkDel.isPending}
                        className="ml-auto text-xs text-red-400 hover:text-red-300 disabled:opacity-50"
                    >
                        {bulkDel.isPending ? 'Deleting...' : 'Delete selected'}
                    </button>
                </div>
            )}

            {/* Table */}
            {records.isPending && <div className="text-sm text-neutral-400">Loading records...</div>}
            {records.error && <div className="text-sm text-red-400">{String(records.error)}</div>}

            {records.data && (
                <div className="overflow-auto">
                    <table className="w-full text-sm">
                        <thead className="text-left text-xs uppercase tracking-wider text-neutral-500">
                            <tr>
                                {!isView && (
                                    <th className="w-8 py-2">
                                        <input
                                            type="checkbox"
                                            checked={allSelected}
                                            onChange={handleSelectAll}
                                            disabled={records.data.items.length === 0}
                                        />
                                    </th>
                                )}
                                {visibleFields.map((f) => (
                                    <SortHeader
                                        key={f.id}
                                        name={f.name}
                                        currentSort={sort}
                                        onClick={handleSort}
                                    />
                                ))}
                                <th className="py-2 text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            {records.data.items.map((record) => (
                                <tr
                                    key={record.id}
                                    className="border-t border-neutral-800 hover:bg-neutral-900"
                                >
                                    {!isView && (
                                        <td className="py-2">
                                            <input
                                                type="checkbox"
                                                checked={bulkSelected.has(record.id)}
                                                onChange={() => handleToggleSelect(record.id)}
                                            />
                                        </td>
                                    )}
                                    {visibleFields.map((f) => (
                                        <td key={f.id} className="max-w-xs truncate py-2 text-neutral-300">
                                            <CellValue record={record} field={f} />
                                        </td>
                                    ))}
                                    <td className="py-2 text-right">
                                        <div className="flex items-center justify-end gap-2">
                                            {!isView && (
                                                <Link
                                                    to="/collections/$id/records/$recordId"
                                                    params={{ id, recordId: record.id }}
                                                    className="text-xs text-indigo-400 hover:text-indigo-300"
                                                >
                                                    Edit
                                                </Link>
                                            )}
                                            {!isView && (
                                                <button
                                                    type="button"
                                                    onClick={() => handleDeleteRecord(record)}
                                                    disabled={del.isPending}
                                                    className="text-xs text-red-400 hover:text-red-300 disabled:opacity-50"
                                                >
                                                    Delete
                                                </button>
                                            )}
                                        </div>
                                    </td>
                                </tr>
                            ))}
                            {records.data.items.length === 0 && (
                                <tr>
                                    <td
                                        colSpan={visibleFields.length + (isView ? 1 : 2)}
                                        className="py-8 text-center text-neutral-500"
                                    >
                                        No records found.
                                    </td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>
            )}

            {/* Pagination */}
            {totalPages > 1 && (
                <div className="flex items-center justify-center gap-2 text-sm">
                    <button
                        type="button"
                        disabled={page <= 1}
                        onClick={() => setPage((p) => Math.max(1, p - 1))}
                        className="rounded bg-neutral-800 px-2 py-1 hover:bg-neutral-700 disabled:opacity-50"
                    >
                        Prev
                    </button>
                    <span className="text-neutral-400">
                        Page {page} of {totalPages}
                    </span>
                    <button
                        type="button"
                        disabled={page >= totalPages}
                        onClick={() => setPage((p) => p + 1)}
                        className="rounded bg-neutral-800 px-2 py-1 hover:bg-neutral-700 disabled:opacity-50"
                    >
                        Next
                    </button>
                </div>
            )}
        </div>
    );
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function SortHeader({ name, currentSort, onClick }: {
    name: string;
    currentSort: string;
    onClick: (name: string) => void;
}) {
    const arrow = currentSort === name ? ' ^' : currentSort === `-${name}` ? ' v' : '';
    return (
        <th className="cursor-pointer select-none py-2" onClick={() => onClick(name)}>
            {name}{arrow}
        </th>
    );
}

function CellValue({ record, field }: {
    record: RecordModel;
    field: { name: string; type: string };
}) {
    const val = record[field.name];

    if (val === null || val === undefined) return <span className="text-neutral-600">-</span>;

    switch (field.type) {
        case 'bool':
            return <span>{val ? 'true' : 'false'}</span>;
        case 'json':
            return <span className="font-mono text-xs">{JSON.stringify(val).slice(0, 80)}</span>;
        case 'file':
            if (Array.isArray(val)) return <span>{val.length} file(s)</span>;
            return <span>{String(val)}</span>;
        case 'relation':
            if (Array.isArray(val)) return <span>{val.join(', ')}</span>;
            return <span>{String(val)}</span>;
        default:
            return <span>{String(val)}</span>;
    }
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

export const Route = createFileRoute('/collections/$id/records')({
    beforeLoad: () => {
        if (!base.authStore.token) throw redirect({ to: '/login' });
    },
    component: RecordsList,
});
