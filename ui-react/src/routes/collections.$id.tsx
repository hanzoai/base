import { createFileRoute, redirect, useNavigate } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback, useMemo, useState } from 'react';
import { useFieldArray, useForm } from 'react-hook-form';

import { base } from '~/lib/base';

import type { CollectionModel, CollectionField } from 'pocketbase';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type Tab = 'fields' | 'indexes' | 'rules';

interface FieldEntry extends CollectionField {
    _toDelete?: boolean;
}

interface CollectionFormValues {
    name: string;
    type: string;
    fields: FieldEntry[];
    indexes: string[];
    listRule: string;
    viewRule: string;
    createRule: string;
    updateRule: string;
    deleteRule: string;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const FIELD_TYPES = [
    'text', 'number', 'bool', 'email', 'url', 'editor',
    'date', 'select', 'json', 'file', 'relation', 'password',
    'autodate', 'geoPoint',
] as const;

function toFormValues(c: CollectionModel): CollectionFormValues {
    return {
        name: c.name,
        type: c.type,
        fields: (c.fields ?? []).map((f) => ({ ...f })),
        indexes: c.indexes ?? [],
        listRule: c.listRule ?? '',
        viewRule: c.viewRule ?? '',
        createRule: c.createRule ?? '',
        updateRule: c.updateRule ?? '',
        deleteRule: c.deleteRule ?? '',
    };
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

function CollectionEditor() {
    const { id } = Route.useParams();
    const qc = useQueryClient();
    const nav = useNavigate();
    const [activeTab, setActiveTab] = useState<Tab>('fields');

    // Fetch collection
    const collection = useQuery({
        queryKey: ['collections', id],
        queryFn: () => base.collections.getOne(id),
    });

    const defaults = useMemo(
        () => collection.data ? toFormValues(collection.data) : undefined,
        [collection.data],
    );

    // Form — reset when data arrives
    const form = useForm<CollectionFormValues>({
        values: defaults,
    });

    const fieldArray = useFieldArray({
        control: form.control,
        name: 'fields',
        keyName: '_rhfId',
    });

    // Save
    const save = useMutation({
        mutationFn: (data: CollectionFormValues) => {
            // Strip deleted fields before sending
            const payload = {
                ...data,
                fields: data.fields.filter((f) => !f._toDelete),
            };
            return base.collections.update(id, payload);
        },
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['collections'] });
            qc.invalidateQueries({ queryKey: ['collections', id] });
        },
    });

    // Delete collection
    const del = useMutation({
        mutationFn: () => base.collections.delete(id),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['collections'] });
            nav({ to: '/collections' });
        },
    });

    const handleSave = useCallback(
        (data: CollectionFormValues) => { save.mutate(data); },
        [save],
    );

    const handleDelete = useCallback(() => {
        if (confirm(`Delete collection "${collection.data?.name}"? All records will be lost.`)) {
            del.mutate();
        }
    }, [del, collection.data?.name]);

    const handleAddField = useCallback(() => {
        const name = `field${fieldArray.fields.length + 1}`;
        fieldArray.append({
            id: '',
            name,
            type: 'text',
            system: false,
            hidden: false,
            presentable: false,
        } as FieldEntry);
    }, [fieldArray]);

    // Loading / error
    if (collection.isPending) return <div className="text-sm text-neutral-400">Loading...</div>;
    if (collection.error) return <div className="text-sm text-red-400">{String(collection.error)}</div>;

    const isSystem = collection.data?.system ?? false;
    const isView = collection.data?.type === 'view';

    return (
        <form onSubmit={form.handleSubmit(handleSave)} className="flex flex-col gap-4">
            {/* Header */}
            <header className="flex items-center gap-3">
                <h1 className="text-xl font-semibold">
                    Edit collection
                </h1>
                <input
                    {...form.register('name', { required: true })}
                    disabled={isSystem}
                    className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm font-medium"
                />
                <span className="rounded bg-neutral-800 px-2 py-0.5 text-xs text-neutral-400">
                    {collection.data?.type}
                </span>
                <div className="ml-auto flex gap-2">
                    {!isSystem && (
                        <button
                            type="button"
                            onClick={handleDelete}
                            disabled={del.isPending}
                            className="rounded bg-red-900/50 px-3 py-1 text-sm text-red-300 hover:bg-red-900 disabled:opacity-50"
                        >
                            Delete
                        </button>
                    )}
                    <button
                        type="submit"
                        disabled={!form.formState.isDirty || save.isPending}
                        className="rounded bg-indigo-600 px-3 py-1 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50"
                    >
                        {save.isPending ? 'Saving...' : 'Save'}
                    </button>
                </div>
            </header>

            {save.error && (
                <div className="text-sm text-red-400">{String(save.error)}</div>
            )}

            {/* Tabs */}
            <div className="flex gap-1 border-b border-neutral-800">
                <TabButton active={activeTab === 'fields'} onClick={() => setActiveTab('fields')}>
                    {isView ? 'Query' : 'Fields'}
                </TabButton>
                <TabButton active={activeTab === 'indexes'} onClick={() => setActiveTab('indexes')}>
                    Indexes ({form.watch('indexes')?.length ?? 0})
                </TabButton>
                <TabButton active={activeTab === 'rules'} onClick={() => setActiveTab('rules')}>
                    API Rules
                </TabButton>
            </div>

            {/* Fields tab */}
            {activeTab === 'fields' && (
                <div className="flex flex-col gap-2">
                    {fieldArray.fields.map((field, idx) => {
                        if (field._toDelete) return null;
                        return (
                            <FieldRow
                                key={field._rhfId}
                                index={idx}
                                register={form.register}
                                isSystem={field.system}
                                onRemove={() => {
                                    if (field.id) {
                                        // Mark for server-side deletion
                                        form.setValue(`fields.${idx}._toDelete`, true, { shouldDirty: true });
                                    } else {
                                        fieldArray.remove(idx);
                                    }
                                }}
                            />
                        );
                    })}
                    {!isView && (
                        <button
                            type="button"
                            onClick={handleAddField}
                            className="w-full rounded border border-dashed border-neutral-700 py-2 text-sm text-neutral-400 hover:border-neutral-500 hover:text-neutral-200"
                        >
                            + Add field
                        </button>
                    )}
                </div>
            )}

            {/* Indexes tab */}
            {activeTab === 'indexes' && (
                <IndexesPanel
                    indexes={form.watch('indexes') ?? []}
                    onChange={(indexes: string[]) => form.setValue('indexes', indexes, { shouldDirty: true })}
                />
            )}

            {/* Rules tab */}
            {activeTab === 'rules' && (
                <div className="flex flex-col gap-3">
                    <RuleField label="List/Search rule" name="listRule" register={form.register} />
                    <RuleField label="View rule" name="viewRule" register={form.register} />
                    {!isView && (
                        <>
                            <RuleField label="Create rule" name="createRule" register={form.register} />
                            <RuleField label="Update rule" name="updateRule" register={form.register} />
                            <RuleField label="Delete rule" name="deleteRule" register={form.register} />
                        </>
                    )}
                    <p className="text-xs text-neutral-500">
                        Leave a rule empty to require superuser access. Use filter syntax to restrict access.
                    </p>
                </div>
            )}
        </form>
    );
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function TabButton({ active, onClick, children }: {
    active: boolean;
    onClick: () => void;
    children: React.ReactNode;
}) {
    return (
        <button
            type="button"
            onClick={onClick}
            className={`px-3 py-2 text-sm ${
                active
                    ? 'border-b-2 border-indigo-500 text-neutral-100'
                    : 'text-neutral-400 hover:text-neutral-200'
            }`}
        >
            {children}
        </button>
    );
}

function FieldRow({ index, register, isSystem, onRemove }: {
    index: number;
    register: ReturnType<typeof useForm<CollectionFormValues>>['register'];
    isSystem: boolean;
    onRemove: () => void;
}) {
    return (
        <div className="flex items-center gap-2 rounded border border-neutral-800 p-2">
            <input
                {...register(`fields.${index}.name`, { required: true })}
                disabled={isSystem}
                placeholder="Field name"
                className="w-40 rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm"
            />
            <select
                {...register(`fields.${index}.type`)}
                disabled={isSystem}
                className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm"
            >
                {FIELD_TYPES.map((t) => (
                    <option key={t} value={t}>{t}</option>
                ))}
            </select>
            <label className="flex items-center gap-1 text-xs text-neutral-400">
                <input type="checkbox" {...register(`fields.${index}.hidden`)} disabled={isSystem} />
                Hidden
            </label>
            <label className="flex items-center gap-1 text-xs text-neutral-400">
                <input type="checkbox" {...register(`fields.${index}.presentable`)} disabled={isSystem} />
                Presentable
            </label>
            {isSystem && (
                <span className="text-xs text-neutral-500">system</span>
            )}
            {!isSystem && (
                <button
                    type="button"
                    onClick={onRemove}
                    className="ml-auto text-xs text-red-400 hover:text-red-300"
                >
                    Remove
                </button>
            )}
        </div>
    );
}

function RuleField({ label, name, register }: {
    label: string;
    name: keyof CollectionFormValues;
    register: ReturnType<typeof useForm<CollectionFormValues>>['register'];
}) {
    return (
        <label className="flex flex-col gap-1">
            <span className="text-xs text-neutral-400">{label}</span>
            <input
                {...register(name)}
                placeholder='e.g. @request.auth.id != ""'
                className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm font-mono"
            />
        </label>
    );
}

function IndexesPanel({ indexes, onChange }: {
    indexes: string[];
    onChange: (v: string[]) => void;
}) {
    const [draft, setDraft] = useState('');

    const handleAdd = useCallback(() => {
        const trimmed = draft.trim();
        if (!trimmed) return;
        onChange([...indexes, trimmed]);
        setDraft('');
    }, [draft, indexes, onChange]);

    const handleRemove = useCallback((idx: number) => {
        onChange(indexes.filter((_, i) => i !== idx));
    }, [indexes, onChange]);

    return (
        <div className="flex flex-col gap-2">
            {indexes.map((idx, i) => (
                <div key={i} className="flex items-center gap-2 text-sm">
                    <code className="flex-1 rounded bg-neutral-900 px-2 py-1 font-mono text-xs">
                        {idx}
                    </code>
                    <button
                        type="button"
                        onClick={() => handleRemove(i)}
                        className="text-xs text-red-400 hover:text-red-300"
                    >
                        Remove
                    </button>
                </div>
            ))}
            <div className="flex gap-2">
                <input
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    placeholder="CREATE INDEX idx_name ON tablename (column)"
                    className="flex-1 rounded border border-neutral-700 bg-neutral-900 px-2 py-1 text-sm font-mono"
                />
                <button
                    type="button"
                    onClick={handleAdd}
                    className="rounded bg-neutral-800 px-3 py-1 text-sm hover:bg-neutral-700"
                >
                    Add
                </button>
            </div>
        </div>
    );
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

export const Route = createFileRoute('/collections/$id')({
    beforeLoad: () => {
        if (!base.authStore.token) throw redirect({ to: '/login' });
    },
    component: CollectionEditor,
});
