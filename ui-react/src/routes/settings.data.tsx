import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { CollectionModel } from '~/lib/base';
import { useRef, useState } from 'react';

import { base } from '~/lib/base';
import { SectionCard } from '~/components/SectionCard';
import { bytes, count } from '~/lib/format';

type CollectionRecord = CollectionModel & Record<string, unknown>;

// An uploaded file is a document until this says otherwise. Both the preview
// below and the server address an entry by its id or its name, and the preview
// renders them, so those are what a parsed entry has to carry: text, at least
// one of the two. Anything else is caught here, where it can be said, rather
// than in a render, where it cannot.
function isSchema(entry: unknown): boolean {
    if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return false;
    const { id, name } = entry as { id?: unknown; name?: unknown };
    const text = (v: unknown) => v === undefined || typeof v === 'string';
    return text(id) && text(name) && (typeof id === 'string' || typeof name === 'string');
}

// What the engine is called where it is bought, as against what it is called
// where SQL is written: the report names the dialect, because that is what
// builds the SQL, and this is the product it belongs to. An engine with no
// product name reads as itself rather than as nothing.
const PRODUCT: Record<string, string> = {
    sqlite: 'Hanzo SQLite',
    postgres: 'Hanzo SQL',
};

function Storage() {
    const qc = useQueryClient();
    const db = useQuery({ queryKey: ['database'], queryFn: () => base.database.get() });

    const reclaim = useMutation({
        mutationFn: () => base.database.reclaim(),
        onSuccess: () => qc.invalidateQueries({ queryKey: ['database'] }),
    });

    if (db.isPending) return <SectionCard title="Storage"><div className="muted">Loading...</div></SectionCard>;
    if (db.error) return <SectionCard title="Storage"><div className="danger">{ db.error.message }</div></SectionCard>;

    const data = db.data!;
    const counted = data.collections.filter((c) => c.records !== null);
    const records = counted.reduce((sum, c) => sum + (c.records ?? 0), 0);
    const uncounted = data.collections.length - counted.length;
    const freed = reclaim.data ? reclaim.data.before - reclaim.data.after : 0;

    // Biggest first: the reason to look at this list is to find what is using
    // the space, and a name is easier to find in a list than a number is.
    const collections = [...data.collections].sort((a, b) => (b.records ?? -1) - (a.records ?? -1));

    return (
        <SectionCard title="Storage" description="Where this Base keeps its records.">
            <div className="row">
                <span className="tag ok">{ PRODUCT[data.engine] ?? data.engine }</span>
                <span className="muted small">
                    { data.local
                        ? 'Embedded — the files below are this Base\'s own. The same API runs on Hanzo SQL, once a deployment points an instance at a server.'
                        : 'On a server — sizing it, backing it up and rewriting it happen there.' }
                </span>
            </div>

            <div className="grid" style={ { ['--cols' as string]: 3 } }>
                <div className="stack stack--tight">
                    <span className="eyebrow">Records</span>
                    <span className="num">{ count(records) }{ uncounted > 0 && '+' }</span>
                </div>
                <div className="stack stack--tight">
                    <span className="eyebrow">Collections</span>
                    <span className="num">{ count(data.collections.length) }</span>
                </div>
                <div className="stack stack--tight">
                    <span className="eyebrow">On disk</span>
                    <span className="num">{ data.local ? bytes(data.data.size + data.aux.size) : '—' }</span>
                </div>
            </div>

            { data.local && (
                <ul className="stack stack--tight small">
                    { [data.data, data.aux].map((file) => (
                        <li key={ file.path } className="row">
                            <span className="mono truncate grow">{ file.path }</span>
                            <span className="muted num">{ bytes(file.size) }</span>
                        </li>
                    )) }
                </ul>
            ) }

            <div className="row">
                <button
                    onClick={ () => reclaim.mutate() }
                    disabled={ reclaim.isPending }
                    className="btn btn--outline"
                >
                    { reclaim.isPending ? 'Reclaiming...' : 'Reclaim unused space' }
                </button>
                <span className="muted small">
                    Rewrites both databases without the pages deleted records left behind.
                </span>
                { reclaim.isSuccess && (
                    <span className="ok small">
                        { data.local ? `Freed ${bytes(freed)}.` : 'Rewritten at the server.' }
                    </span>
                ) }
                { reclaim.error && <span className="danger small">{ reclaim.error.message }</span> }
            </div>

            <div className="table-wrap" style={ { maxHeight: '18rem' } }>
                <table className="table">
                    <thead>
                        <tr>
                            <th>Collection</th>
                            <th>Type</th>
                            <th align="right">Records</th>
                        </tr>
                    </thead>
                    <tbody>
                        { collections.map((c) => (
                            <tr key={ c.id }>
                                <td>
                                    { c.name }
                                    { c.system && <span className="type-tag"> system</span> }
                                </td>
                                <td className="muted">{ c.type }</td>
                                <td align="right" className="num">
                                    { c.records === null ? <span className="muted" title="the engine refused to count this one">?</span> : count(c.records) }
                                </td>
                            </tr>
                        )) }
                    </tbody>
                </table>
            </div>
        </SectionCard>
    );
}

function DataSettings() {
    const qc = useQueryClient();
    const fileRef = useRef<HTMLInputElement>(null);
    const [importJson, setImportJson] = useState('');
    const [importParsed, setImportParsed] = useState<CollectionRecord[]>([]);
    const [importError, setImportError] = useState('');

    const collections = useQuery({
        queryKey: ['collections'],
        queryFn: () => base.collections.getFullList({ sort: 'name', batch: 200 }),
    });

    // Export: selected collection IDs
    const [selected, setSelected] = useState<Set<string>>(new Set());
    const allCollections = (collections.data ?? []) as CollectionRecord[];
    const allSelected = selected.size === allCollections.length && allCollections.length > 0;

    function toggleAll() {
        if (allSelected) {
            setSelected(new Set());
        } else {
            setSelected(new Set(allCollections.map((c) => c.id)));
        }
    }

    function toggleOne(id: string) {
        const next = new Set(selected);
        if (next.has(id)) next.delete(id);
        else next.add(id);
        setSelected(next);
    }

    // A schema, without the stamps that say when THIS server last touched it —
    // `Collection` serializes them as `createdAt`/`updatedAt`, and an import
    // binds whatever the document names, so carrying them writes one server's
    // history onto another's. The two deletes here named `created`/`updated`,
    // which is what the database columns are called and what no document has.
    function schemas(): CollectionRecord[] {
        return allCollections
            .filter((c) => selected.has(c.id))
            .map(({ createdAt: _c, updatedAt: _u, ...schema }) => schema as CollectionRecord);
    }

    function exportJson() {
        const exported = schemas();
        const blob = new Blob([JSON.stringify(exported, null, 2)], { type: 'application/json' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = 'collections-schema.json';
        a.click();
        URL.revokeObjectURL(url);
    }

    function copyExport() {
        void navigator.clipboard.writeText(JSON.stringify(schemas(), null, 2));
    }

    // Import
    function handleFileLoad(e: React.ChangeEvent<HTMLInputElement>) {
        const file = e.target.files?.[0];
        if (!file) return;
        const reader = new FileReader();
        reader.onload = (ev) => {
            const text = ev.target?.result as string;
            setImportJson(text);
            parseImport(text);
        };
        reader.readAsText(file);
    }

    function parseImport(text: string) {
        setImportError('');
        try {
            const parsed: unknown = JSON.parse(text);
            if (!Array.isArray(parsed)) {
                setImportError('Expected a JSON array of collection schemas.');
                setImportParsed([]);
                return;
            }
            const wrong = parsed.findIndex((entry) => !isSchema(entry));
            if (wrong !== -1) {
                setImportError(`Entry ${wrong + 1} is not a collection schema — id and name are text, and one of them has to be there.`);
                setImportParsed([]);
                return;
            }
            setImportParsed(parsed as CollectionRecord[]);
        } catch {
            setImportError('Invalid JSON.');
            setImportParsed([]);
        }
    }

    // Diff preview
    const existingById = new Map(allCollections.map((c) => [c.id, c]));
    const toAdd = importParsed.filter((c) => !existingById.has(c.id));
    const toUpdate = importParsed.filter((c) => existingById.has(c.id));

    const importMutation = useMutation({
        mutationFn: async () => {
            await base.collections.import(importParsed);
        },
        onSuccess: () => {
            setImportJson('');
            setImportParsed([]);
            void qc.invalidateQueries({ queryKey: ['collections'] });
        },
    });

    if (collections.isPending) return <div className="muted">Loading...</div>;

    return (
        <div className="page">
            <Storage />

            <SectionCard title="Export collections" description="Download your collection schemas as JSON.">
                <div className="row">
                    <label className="field field--inline">
                        <input
                            type="checkbox"
                            checked={ allSelected }
                            onChange={ toggleAll }
                            
                        />
                        <span className="field__label">Select all ({ allCollections.length })</span>
                    </label>
                </div>

                <div className="row row--wrap row--tight">
                    { allCollections.map((c) => (
                        <label
                            key={ c.id }
                            className="chip"
                            data-active={ selected.has(c.id) ? 'true' : undefined }
                        >
                            <input
                                type="checkbox"
                                checked={ selected.has(c.id) }
                                onChange={ () => toggleOne(c.id) }
                            />
                            { c.name }
                        </label>
                    )) }
                </div>

                <div className="row">
                    <button onClick={ exportJson } disabled={ selected.size === 0 } className="btn">
                        Download JSON
                    </button>
                    <button onClick={ copyExport } disabled={ selected.size === 0 } className="btn btn--outline">
                        Copy to clipboard
                    </button>
                </div>
            </SectionCard>

            <SectionCard title="Import collections" description="Upload a JSON schema to create or update collections.">
                <div className="row">
                    <input
                        ref={ fileRef }
                        type="file"
                        accept=".json"
                        onChange={ handleFileLoad }
                        className="hidden"
                    />
                    <button onClick={ () => fileRef.current?.click() } className="btn btn--outline">
                        Load from file
                    </button>
                    <span className="muted small">or paste JSON below</span>
                </div>

                <textarea
                    value={ importJson }
                    onChange={ (e) => { setImportJson(e.target.value); parseImport(e.target.value); } }
                    rows={ 10 }
                    spellCheck={ false }
                    placeholder="[{ &quot;id&quot;: &quot;...&quot;, &quot;name&quot;: &quot;...&quot;, ... }]"
                    className="input input--mono"
                />

                { importError && <div className="danger small">{ importError }</div> }

                { importParsed.length > 0 && (
                    <div className="stack">
                        <h4 className="eyebrow">
                            Detected changes
                        </h4>
                        <ul className="field">
                            { toAdd.map((c) => (
                                <li key={ c.id } className="row">
                                    <span className="tag ok">Add</span>
                                    <span>{ c.name }</span>
                                    <span className="muted small">{ c.id }</span>
                                </li>
                            )) }
                            { toUpdate.map((c) => (
                                <li key={ c.id } className="row">
                                    <span className="tag warn">Update</span>
                                    <span>{ c.name }</span>
                                    <span className="muted small">{ c.id }</span>
                                </li>
                            )) }
                        </ul>
                    </div>
                ) }

                <div className="row">
                    <button
                        onClick={ () => importMutation.mutate() }
                        disabled={ importParsed.length === 0 || importMutation.isPending }
                        className="btn btn--outline"
                    >
                        { importMutation.isPending ? 'Importing...' : 'Apply import' }
                    </button>
                    { importJson && (
                        <button
                            onClick={ () => { setImportJson(''); setImportParsed([]); setImportError(''); } }
                            className="link small"
                        >
                            Clear
                        </button>
                    ) }
                    { importMutation.isSuccess && <span className="ok small">Imported.</span> }
                    { importMutation.error && <span className="danger small">{ importMutation.error.message }</span> }
                </div>
            </SectionCard>
        </div>
    );
}

export const Route = createFileRoute('/settings/data')({
    component: DataSettings,
});
