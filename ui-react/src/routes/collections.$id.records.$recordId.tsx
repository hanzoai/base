import { createFileRoute, Link, redirect, useNavigate } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useCallback, useMemo, useRef } from 'react';
import { useForm } from 'react-hook-form';

import { base } from '~/lib/base';

import type { CollectionField } from 'pocketbase';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type RecordFormValues = Record<string, unknown>;

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

function RecordEditor() {
    const { id, recordId } = Route.useParams();
    const qc = useQueryClient();
    const nav = useNavigate();

    const isNew = recordId === '_new';

    // Fetch collection schema
    const collection = useQuery({
        queryKey: ['collections', id],
        queryFn: () => base.collections.getOne(id),
    });

    const collectionName = collection.data?.name ?? id;

    // Fetch existing record (skip for new)
    const record = useQuery({
        queryKey: ['records', collectionName, recordId],
        queryFn: () => base.collection(collectionName).getOne(recordId),
        enabled: !isNew && Boolean(collection.data),
    });

    // Build editable fields list (skip system autodate, hidden id)
    const editableFields = useMemo(() => {
        if (!collection.data) return [];
        return collection.data.fields.filter((f) => {
            if (f.type === 'autodate') return false;
            if (f.name === 'id') return false;
            return true;
        });
    }, [collection.data]);

    // Default form values from existing record or empty
    const defaults = useMemo(() => {
        const vals: RecordFormValues = {};
        for (const f of editableFields) {
            vals[f.name] = record.data?.[f.name] ?? defaultForType(f.type);
        }
        return vals;
    }, [editableFields, record.data]);

    const form = useForm<RecordFormValues>({ values: defaults });

    // Track file uploads per field
    const fileUploads = useRef<Record<string, File[]>>({});

    // Save mutation
    const save = useMutation({
        mutationFn: async (data: RecordFormValues) => {
            const formData = buildFormData(data, editableFields, fileUploads.current, collection.data?.type === 'auth');
            if (isNew) {
                return base.collection(collectionName).create(formData);
            }
            return base.collection(collectionName).update(recordId, formData);
        },
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['records', collectionName] });
            nav({ to: '/collections/$id/records', params: { id } });
        },
    });

    // Delete mutation
    const del = useMutation({
        mutationFn: () => base.collection(collectionName).delete(recordId),
        onSuccess: () => {
            qc.invalidateQueries({ queryKey: ['records', collectionName] });
            nav({ to: '/collections/$id/records', params: { id } });
        },
    });

    const handleSave = useCallback(
        (data: RecordFormValues) => { save.mutate(data); },
        [save],
    );

    const handleDelete = useCallback(() => {
        if (confirm('Delete this record?')) del.mutate();
    }, [del]);

    const handleFileChange = useCallback((fieldName: string, files: FileList | null) => {
        fileUploads.current[fieldName] = files ? Array.from(files) : [];
    }, []);

    // Loading
    if (collection.isPending) return <div className="text-sm text-neutral-400">Loading schema...</div>;
    if (collection.error) return <div className="text-sm text-red-400">{String(collection.error)}</div>;
    if (!isNew && record.isPending) return <div className="text-sm text-neutral-400">Loading record...</div>;
    if (!isNew && record.error) return <div className="text-sm text-red-400">{String(record.error)}</div>;

    return (
        <form onSubmit={form.handleSubmit(handleSave)} className="flex flex-col gap-4">
            {/* Header */}
            <header className="flex items-center gap-3">
                <Link
                    to="/collections/$id/records"
                    params={{ id }}
                    className="text-neutral-400 hover:text-neutral-200"
                >
                    {collection.data.name}
                </Link>
                <span className="text-neutral-600">/</span>
                <h1 className="text-xl font-semibold">
                    {isNew ? 'New record' : `Edit ${recordId}`}
                </h1>
                <div className="ml-auto flex gap-2">
                    {!isNew && (
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
                        disabled={(!isNew && !form.formState.isDirty) || save.isPending}
                        className="rounded bg-indigo-600 px-3 py-1 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50"
                    >
                        {save.isPending ? 'Saving...' : isNew ? 'Create' : 'Save'}
                    </button>
                </div>
            </header>

            {save.error && (
                <div className="text-sm text-red-400">{String(save.error)}</div>
            )}

            {/* ID field (read-only on edit) */}
            {!isNew && record.data && (
                <div className="flex flex-col gap-1">
                    <span className="text-xs text-neutral-500">id</span>
                    <span className="rounded bg-neutral-900 px-2 py-1 text-sm font-mono text-neutral-400">
                        {record.data.id}
                    </span>
                </div>
            )}

            {/* Auth fields — password/email/verified */}
            {collection.data.type === 'auth' && (
                <AuthFields
                    register={form.register}
                    isNew={isNew}
                    isSuperusers={collection.data.name === '_superusers'}
                />
            )}

            {/* Dynamic fields from schema */}
            {editableFields
                .filter((f) => !isAuthSkipField(f.name, collection.data.type === 'auth'))
                .map((f) => (
                    <SchemaField
                        key={f.name}
                        field={f}
                        register={form.register}
                        onFileChange={handleFileChange}
                    />
                ))}

            {/* Autodate fields (read-only) */}
            {!isNew && record.data && collection.data.fields
                .filter((f) => f.type === 'autodate')
                .map((f) => (
                    <div key={f.name} className="flex flex-col gap-1">
                        <span className="text-xs text-neutral-500">{f.name}</span>
                        <span className="text-sm text-neutral-400">{record.data[f.name] ?? '-'}</span>
                    </div>
                ))}
        </form>
    );
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const AUTH_SKIP_FIELDS = new Set(['email', 'emailVisibility', 'verified', 'tokenKey', 'password']);

function isAuthSkipField(name: string, isAuth: boolean): boolean {
    return isAuth && AUTH_SKIP_FIELDS.has(name);
}

function defaultForType(type: string): unknown {
    switch (type) {
        case 'number': return 0;
        case 'bool': return false;
        case 'json': return '';
        case 'select': return '';
        case 'relation': return '';
        case 'file': return '';
        case 'date': return '';
        default: return '';
    }
}

function buildFormData(
    data: RecordFormValues,
    fields: CollectionField[],
    fileUploads: Record<string, File[]>,
    isAuth: boolean,
): FormData {
    const fd = new FormData();

    for (const field of fields) {
        if (field.type === 'autodate') continue;
        if (isAuth && field.type === 'password') continue;

        const val = data[field.name];

        if (field.type === 'json' && typeof val === 'string' && val.trim()) {
            try {
                JSON.parse(val);
            } catch {
                throw new Error(`Invalid JSON in field "${field.name}"`);
            }
            fd.append(field.name, val);
        } else if (field.type === 'file') {
            // Files handled separately below
        } else if (val !== undefined && val !== null) {
            fd.append(field.name, String(val));
        }
    }

    // Auth password fields — only if explicitly set
    if (isAuth) {
        const pw = data['password'];
        if (typeof pw === 'string' && pw) {
            fd.append('password', pw);
            fd.append('passwordConfirm', String(data['passwordConfirm'] ?? pw));
        }
    }

    // File uploads
    for (const [fieldName, files] of Object.entries(fileUploads)) {
        for (const file of files) {
            fd.append(fieldName, file);
        }
    }

    return fd;
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

function AuthFields({ register, isNew, isSuperusers }: {
    register: ReturnType<typeof useForm<RecordFormValues>>['register'];
    isNew: boolean;
    isSuperusers: boolean;
}) {
    return (
        <div className="flex flex-col gap-3 rounded border border-neutral-800 p-3">
            <h3 className="text-xs font-medium uppercase tracking-wider text-neutral-500">
                Auth fields
            </h3>
            <label className="flex flex-col gap-1">
                <span className="text-xs text-neutral-400">Email</span>
                <input
                    {...register('email')}
                    type="email"
                    className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm"
                />
            </label>
            {!isSuperusers && (
                <>
                    <label className="flex items-center gap-2 text-sm">
                        <input {...register('emailVisibility')} type="checkbox" />
                        <span className="text-neutral-400">Email visible</span>
                    </label>
                    <label className="flex items-center gap-2 text-sm">
                        <input {...register('verified')} type="checkbox" />
                        <span className="text-neutral-400">Verified</span>
                    </label>
                </>
            )}
            <label className="flex flex-col gap-1">
                <span className="text-xs text-neutral-400">
                    {isNew ? 'Password' : 'New password (leave blank to keep)'}
                </span>
                <input
                    {...register('password')}
                    type="password"
                    autoComplete="new-password"
                    className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm"
                />
            </label>
        </div>
    );
}

function SchemaField({ field, register, onFileChange }: {
    field: CollectionField;
    register: ReturnType<typeof useForm<RecordFormValues>>['register'];
    onFileChange: (fieldName: string, files: FileList | null) => void;
}) {
    switch (field.type) {
        case 'text':
        case 'email':
        case 'url':
            return (
                <label className="flex flex-col gap-1">
                    <FieldLabel field={field} />
                    <input
                        {...register(field.name)}
                        type={field.type === 'email' ? 'email' : field.type === 'url' ? 'url' : 'text'}
                        className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm"
                    />
                </label>
            );

        case 'editor':
            return (
                <label className="flex flex-col gap-1">
                    <FieldLabel field={field} />
                    <textarea
                        {...register(field.name)}
                        rows={6}
                        className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm"
                    />
                </label>
            );

        case 'number':
            return (
                <label className="flex flex-col gap-1">
                    <FieldLabel field={field} />
                    <input
                        {...register(field.name, { valueAsNumber: true })}
                        type="number"
                        step="any"
                        className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm"
                    />
                </label>
            );

        case 'bool':
            return (
                <label className="flex items-center gap-2 text-sm">
                    <input {...register(field.name)} type="checkbox" />
                    <FieldLabel field={field} />
                </label>
            );

        case 'select':
            return (
                <label className="flex flex-col gap-1">
                    <FieldLabel field={field} />
                    <input
                        {...register(field.name)}
                        type="text"
                        placeholder="value (or comma-separated for multi)"
                        className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm"
                    />
                </label>
            );

        case 'date':
            return (
                <label className="flex flex-col gap-1">
                    <FieldLabel field={field} />
                    <input
                        {...register(field.name)}
                        type="datetime-local"
                        className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm"
                    />
                </label>
            );

        case 'json':
            return (
                <label className="flex flex-col gap-1">
                    <FieldLabel field={field} />
                    <textarea
                        {...register(field.name)}
                        rows={4}
                        placeholder="{}"
                        className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 font-mono text-sm"
                    />
                </label>
            );

        case 'file':
            return (
                <div className="flex flex-col gap-1">
                    <FieldLabel field={field} />
                    <input
                        type="file"
                        multiple={Boolean((field as Record<string, unknown>).maxSelect !== 1)}
                        onChange={(e) => onFileChange(field.name, e.target.files)}
                        className="text-sm text-neutral-400"
                    />
                </div>
            );

        case 'relation':
            return (
                <label className="flex flex-col gap-1">
                    <FieldLabel field={field} />
                    <input
                        {...register(field.name)}
                        type="text"
                        placeholder="Record ID (or comma-separated for multi)"
                        className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 font-mono text-sm"
                    />
                </label>
            );

        case 'password':
            return (
                <label className="flex flex-col gap-1">
                    <FieldLabel field={field} />
                    <input
                        {...register(field.name)}
                        type="password"
                        autoComplete="new-password"
                        className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm"
                    />
                </label>
            );

        case 'geoPoint':
            return (
                <label className="flex flex-col gap-1">
                    <FieldLabel field={field} />
                    <input
                        {...register(field.name)}
                        type="text"
                        placeholder='{"lon": 0, "lat": 0}'
                        className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 font-mono text-sm"
                    />
                </label>
            );

        default:
            return (
                <label className="flex flex-col gap-1">
                    <FieldLabel field={field} />
                    <input
                        {...register(field.name)}
                        type="text"
                        className="rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm"
                    />
                </label>
            );
    }
}

function FieldLabel({ field }: { field: CollectionField }) {
    return (
        <span className="flex items-center gap-1 text-xs text-neutral-400">
            <span>{field.name}</span>
            <span className="text-neutral-600">({field.type})</span>
            {field.system && <span className="text-neutral-600">system</span>}
        </span>
    );
}

// ---------------------------------------------------------------------------
// Route
// ---------------------------------------------------------------------------

export const Route = createFileRoute('/collections/$id/records/$recordId')({
    beforeLoad: () => {
        if (!base.authStore.token) throw redirect({ to: '/login' });
    },
    component: RecordEditor,
});
