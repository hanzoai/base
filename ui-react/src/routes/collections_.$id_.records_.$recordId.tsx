import { Link, createFileRoute, redirect, useNavigate } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useEffect, useMemo, useRef, useState } from 'react';

import { Button } from '~/components/ui/button';
import { Checkbox } from '~/components/ui/checkbox';
import { Input } from '~/components/ui/input';
import { Label } from '~/components/ui/label';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '~/components/ui/select';
import { Textarea } from '~/components/ui/textarea';
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
  const isAuth = collection.data?.type === 'auth';

  const record = useQuery({
    queryKey: ['records', name, recordId],
    queryFn: () => base.collection(name).getOne(recordId),
    enabled: !isNew && Boolean(collection.data),
  });

  const editable = useMemo<CollectionField[]>(
    () =>
      (collection.data?.fields ?? []).filter(
        (f) => f.type !== 'autodate' && !HIDDEN.has(f.name) && f.type !== 'password',
      ),
    [collection.data],
  );

  const [values, setValues] = useState<Record<string, unknown>>({});
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
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
      if (isAuth && password) {
        fd.append('password', password);
        fd.append('passwordConfirm', password);
      }
      return isNew ? base.collection(name).create(fd) : base.collection(name).update(recordId, fd);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['records', name] });
      nav({ to: '/collections/$id/records', params: { id } });
    },
    onError: (e) => setError(String((e as Error)?.message ?? e)),
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
    setError('');
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
      setError('Invalid JSON in one of the fields');
    }
  };

  if (collection.isPending) return <Muted>Loading schema…</Muted>;
  if (collection.error) return <ErrorText error={ collection.error } />;
  if (!isNew && record.isPending) return <Muted>Loading record…</Muted>;
  if (!isNew && record.error) return <ErrorText error={ record.error } />;

  return (
    <form onSubmit={ onSubmit } className="mx-auto flex max-w-2xl flex-col gap-5">
      <header className="flex items-center gap-2">
        <Link to="/collections/$id/records" params={{ id }} className="text-muted-foreground hover:text-foreground">
          { name }
        </Link>
        <span className="text-muted-foreground/40">/</span>
        <h1 className="text-xl font-semibold">{ isNew ? 'New record' : record.data?.id }</h1>
        <div className="ml-auto flex gap-2">
          { !isNew && (
            <Button type="button" variant="destructive" disabled={ del.isPending } onClick={ () => del.mutate() }>
              { del.isPending ? 'Deleting…' : 'Delete' }
            </Button>
          ) }
          <Button type="submit" disabled={ save.isPending }>
            { save.isPending ? 'Saving…' : isNew ? 'Create' : 'Save' }
          </Button>
        </div>
      </header>

      { error && <p className="text-sm text-destructive">{ error }</p> }

      <div className="flex flex-col gap-4 rounded-lg border border-border bg-card p-6">
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

        { isAuth && (
          <div className="flex flex-col gap-1.5">
            <Label>{ isNew ? 'Password' : 'New password (blank keeps current)' }</Label>
            <Input
              type="password"
              autoComplete="new-password"
              value={ password }
              onChange={ (e) => setPassword(e.target.value) }
            />
          </div>
        ) }

        { !isNew && record.data && (
          <div className="grid grid-cols-2 gap-4 border-t border-border pt-4">
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
      <label className="flex items-center gap-2 text-sm">
        <Checkbox checked={ Boolean(value) } onCheckedChange={ (c) => onChange(Boolean(c)) } />
        <FieldLabel field={ field } inline />
      </label>
    );
  }

  return (
    <div className="flex flex-col gap-1.5">
      <FieldLabel field={ field } />
      { kind === 'select' && !isMultiValue(field) ? (
        <Select value={ String(value ?? '') } onValueChange={ onChange }>
          <SelectTrigger><SelectValue placeholder="Select…" /></SelectTrigger>
          <SelectContent>
            { selectValues(field).map((o) => <SelectItem key={ o } value={ o }>{ o }</SelectItem>) }
          </SelectContent>
        </Select>
      ) : kind === 'textarea' || kind === 'json' ? (
        <Textarea
          rows={ kind === 'json' ? 5 : 4 }
          className={ kind === 'json' ? 'font-mono text-xs' : '' }
          value={ asText(value, field) }
          placeholder={ kind === 'json' ? '{ }' : '' }
          onChange={ (e) => onChange(e.target.value) }
        />
      ) : kind === 'file' ? (
        <input
          type="file"
          multiple={ isMultiValue(field) }
          onChange={ (e) => onFiles(e.target.files ? Array.from(e.target.files) : []) }
          className="text-sm text-muted-foreground file:mr-3 file:rounded-md file:border file:border-border file:bg-secondary file:px-3 file:py-1 file:text-sm file:text-foreground"
        />
      ) : (
        <Input
          type={ kind === 'number' ? 'number' : kind === 'date' ? 'datetime-local' : 'text' }
          step={ kind === 'number' ? 'any' : undefined }
          placeholder={ kind === 'relation' ? 'record id(s), comma-separated' : '' }
          value={ asText(value, field) }
          onChange={ (e) => onChange(kind === 'number' ? e.target.value : e.target.value) }
        />
      ) }
    </div>
  );
}

function FieldLabel({ field, inline }: { field: CollectionField; inline?: boolean }) {
  return (
    <span className={ inline ? 'text-sm text-foreground' : 'flex items-center gap-1.5' }>
      <span className="text-sm font-medium">{ field.name }</span>
      <span className="text-[10px] uppercase text-muted-foreground/60">{ field.type }</span>
      { field.system && <span className="text-[10px] text-muted-foreground/60">system</span> }
    </span>
  );
}

function ReadonlyRow({ label, value, mono }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[10px] uppercase text-muted-foreground/60">{ label }</span>
      <span className={ mono ? 'font-mono text-sm text-muted-foreground' : 'text-sm text-muted-foreground' }>
        { value }
      </span>
    </div>
  );
}

function Muted({ children }: { children: React.ReactNode }) {
  return <div className="text-sm text-muted-foreground">{ children }</div>;
}

function ErrorText({ error }: { error: unknown }) {
  return <div className="text-sm text-destructive">{ String(error) }</div>;
}

function defaultForKind(field: CollectionField): unknown {
  switch (editorKind(field)) {
    case 'number': return '';
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
  beforeLoad: () => {
    if (!base.authStore.token) throw redirect({ to: '/login' });
  },
  component: RecordEditor,
});
