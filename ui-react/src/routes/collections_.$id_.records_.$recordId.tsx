import { Link, createFileRoute, useNavigate } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo, useRef, useState } from 'react';

import { requireSession } from '~/lib/guard';
import { base } from '~/lib/base';
import { editorKind, isMultiValue, selectValues, toEditorString } from '~/lib/fields';
import type { CollectionField } from '~/lib/base';

// Fields the schema owns but the panel never edits directly.
const HIDDEN = new Set(['id', 'tokenKey', 'emailVisibility']);

function RecordEditor() {
  const { id, recordId } = Route.useParams();
  const qc = useQueryClient();
  const nav = useNavigate();
  const isNew = recordId === '_new';

  const collection = useQuery({
    queryKey: ['collections', id],
    queryFn: () => base.collections.getOne(id),
  });
  const name = collection.data?.name ?? id;

  const record = useQuery({
    queryKey: ['records', name, recordId],
    queryFn: () => base.collection(name).getOne(recordId),
    enabled: !isNew && Boolean(collection.data),
  });

  const editable = useMemo<CollectionField[]>(
    () =>
      (collection.data?.fields ?? []).filter(
        (f) => f.type !== 'autodate' && !HIDDEN.has(f.name),
      ),
    [collection.data],
  );

  const [values, setValues] = useState<Record<string, unknown>>({});
  const [error, setError] = useState<{ message: string; status?: number } | null>(null);
  const files = useRef<Record<string, File[]>>({});

  // Seed the form once the schema (and record, when editing) resolve.
  useEffect(() => {
    if (!collection.data) return;
    if (!isNew && !record.data) return;
    const seed: Record<string, unknown> = {};
    for (const f of editable) seed[f.name] = record.data?.[f.name] ?? defaultForKind(f);
    setValues(seed);
  }, [collection.data, record.data, isNew, editable]);

  const set = (field: string, value: unknown) => setValues((v) => ({ ...v, [field]: value }));

  const save = useMutation({
    mutationFn: () => {
      const fd = buildFormData(editable, values, files.current);
      return isNew ? base.collection(name).create(fd) : base.collection(name).update(recordId, fd);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['records', name] });
      nav({ to: '/collections/$id/records', params: { id } });
    },
    onError: (e) => setError({
      message: String((e as Error)?.message ?? e),
      status: (e as { status?: number })?.status,
    }),
  });

  const del = useMutation({
    mutationFn: () => base.collection(name).delete(recordId),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['records', name] });
      nav({ to: '/collections/$id/records', params: { id } });
    },
  });

  const onSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);
    try {
      // Validate JSON fields up-front so we fail before the network call.
      for (const f of editable) {
        if (editorKind(f) === 'json') {
          const raw = values[f.name];
          if (typeof raw === 'string' && raw.trim()) JSON.parse(raw);
        }
      }
      save.mutate();
    } catch {
      setError({ message: 'Invalid JSON in one of the fields' });
    }
  };

  // An expired bearer is the one failure that must not cost the edit. Signing
  // in happens in another tab, so this one keeps every value on screen; the
  // session lands in the storage every tab on this origin shares, and the next
  // Save carries it. The stale bearer is cleared on the way out, because the
  // sign-in page sends a live session back to the dashboard.
  const expired = error?.status === 401;

  if (collection.isPending) return <div className="muted">Loading schema…</div>;
  if (collection.error) return <div className="danger">{ String(collection.error) }</div>;
  if (!isNew && record.isPending) return <div className="muted">Loading record…</div>;
  if (!isNew && record.error) return <div className="danger">{ String(record.error) }</div>;

  return (
    <form onSubmit={ onSubmit } className="page" style={{ maxWidth: '42rem', margin: '0 auto' }}>
      <header className="page__head">
        <Link to="/collections/$id/records" params={{ id }} className="muted">{ name }</Link>
        <span className="muted">/</span>
        <h1 className="page__title">{ isNew ? 'New record' : record.data?.id }</h1>
        <div className="row push">
          { !isNew && (
            <button type="button" className="btn btn--danger" disabled={ del.isPending } onClick={ () => del.mutate() }>
              { del.isPending ? 'Deleting…' : 'Delete' }
            </button>
          ) }
          <button type="submit" className="btn" disabled={ save.isPending }>
            { save.isPending ? 'Saving…' : isNew ? 'Create' : 'Save' }
          </button>
        </div>
      </header>

      { error && (
        <div className="row">
          <p className="danger">
            { expired ? 'Your session expired. Sign in again and press Save — nothing here is lost.' : error.message }
          </p>
          { expired && (
            <a
              href={ `${import.meta.env.BASE_URL}login` }
              target="_blank"
              rel="noreferrer"
              className="btn btn--sm"
              onClick={ () => base.authStore.clear() }
            >
              Sign in again
            </a>
          ) }
        </div>
      ) }

      <div className="card stack">
        { !isNew && record.data && <ReadonlyRow label="id" value={ record.data.id } mono /> }

        { editable.map((field) => (
          <FieldEditor
            key={ field.name }
            field={ field }
            value={ values[field.name] }
            onChange={ (v) => set(field.name, v) }
            onFiles={ (fl) => { files.current[field.name] = fl; } }
          />
        )) }

        { !isNew && record.data && (
          <div className="grid">
            { (collection.data?.fields ?? [])
              .filter((f) => f.type === 'autodate')
              .map((f) => <ReadonlyRow key={ f.name } label={ f.name } value={ String(record.data?.[f.name] ?? '—') } />) }
          </div>
        ) }
      </div>
    </form>
  );
}

function FieldEditor({
  field,
  value,
  onChange,
  onFiles,
}: {
  field: CollectionField;
  value: unknown;
  onChange: (value: unknown) => void;
  onFiles: (files: File[]) => void;
}) {
  const kind = editorKind(field);

  if (kind === 'bool') {
    return (
      <label className="field field--inline">
        <input type="checkbox" checked={ Boolean(value) } onChange={ (e) => onChange(e.target.checked) } />
        <FieldLabel field={ field } />
      </label>
    );
  }

  return (
    <label className="field">
      <FieldLabel field={ field } />
      { kind === 'select' && !isMultiValue(field) ? (
        <select className="select" value={ String(value ?? '') } onChange={ (e) => onChange(e.target.value) }>
          <option value="">Select…</option>
          { selectValues(field).map((o) => <option key={ o } value={ o }>{ o }</option>) }
        </select>
      ) : kind === 'textarea' || kind === 'json' ? (
        <textarea
          rows={ kind === 'json' ? 5 : 4 }
          className={ kind === 'json' ? 'textarea textarea--mono' : 'textarea' }
          value={ asText(value, field) }
          placeholder={ kind === 'json' ? '{ }' : '' }
          onChange={ (e) => onChange(e.target.value) }
        />
      ) : kind === 'file' ? (
        <input
          type="file"
          multiple={ isMultiValue(field) }
          onChange={ (e) => onFiles(e.target.files ? Array.from(e.target.files) : []) }
          className="muted"
        />
      ) : (
        <input
          className="input"
          type={ kind === 'number' ? 'number' : kind === 'date' ? 'datetime-local' : 'text' }
          step={ kind === 'number' ? 'any' : undefined }
          placeholder={ kind === 'relation' ? 'record id(s), comma-separated' : '' }
          value={ asText(value, field) }
          onChange={ (e) => onChange(e.target.value) }
        />
      ) }
    </label>
  );
}

function FieldLabel({ field }: { field: CollectionField }) {
  return (
    <span className="row row--tight">
      <span>{ field.name }</span>
      <span className="type-tag">{ field.type }</span>
      { field.system && <span className="type-tag">system</span> }
    </span>
  );
}

function ReadonlyRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="field">
      <span className="type-tag">{ label }</span>
      <span className={ mono ? 'mono muted' : 'muted' }>{ value }</span>
    </div>
  );
}

function defaultForKind(field: CollectionField): unknown {
  switch (editorKind(field)) {
    case 'bool': return false;
    default: return '';
  }
}

function asText(value: unknown, field: CollectionField): string {
  if (value === null || value === undefined) return '';
  if (typeof value === 'string') return value;
  return toEditorString(value, field);
}

function buildFormData(
  fields: CollectionField[],
  values: Record<string, unknown>,
  files: Record<string, File[]>,
): FormData {
  const fd = new FormData();
  for (const field of fields) {
    const kind = editorKind(field);
    if (kind === 'file') continue; // handled below
    const val = values[field.name];
    if (val === undefined || val === null) continue;
    if (kind === 'bool') fd.append(field.name, val ? 'true' : 'false');
    else if (kind === 'number') { if (val !== '') fd.append(field.name, String(val)); }
    else fd.append(field.name, typeof val === 'string' ? val : JSON.stringify(val));
  }
  for (const [fieldName, fl] of Object.entries(files)) {
    for (const file of fl) fd.append(fieldName, file);
  }
  return fd;
}

export const Route = createFileRoute('/collections_/$id_/records_/$recordId')({
  beforeLoad: requireSession,
  component: RecordEditor,
});
