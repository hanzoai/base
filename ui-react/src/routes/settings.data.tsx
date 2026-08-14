import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import type { CollectionModel } from '~/lib/base';
import { useRef, useState } from 'react';

import { base } from '~/lib/base';
import { SectionCard } from '~/components/SectionCard';

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
