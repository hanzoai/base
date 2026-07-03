// EditableCell — the display/edit split. A cell renders its value in display
// mode and, when the grid puts it in edit mode, swaps to a floating editor
// (Popover) with a single commit/cancel protocol. bool toggles in place.
import { useEffect, useRef, useState } from 'react';

import { Checkbox } from '~/components/ui/checkbox';
import { Input } from '~/components/ui/input';
import { Popover, PopoverAnchor, PopoverContent } from '~/components/ui/popover';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '~/components/ui/select';
import { Textarea } from '~/components/ui/textarea';
import { cn } from '~/lib/cn';
import {
  type EditorKind,
  coerceForApi,
  editorKind,
  formatDisplay,
  isMultiValue,
  selectValues,
  toEditorString,
} from '~/lib/fields';
import type { CollectionField, RecordModel } from '~/lib/base';

interface EditableCellProps {
  record: RecordModel;
  field: CollectionField;
  active: boolean;
  editing: boolean;
  onActivate: () => void;
  onEdit: (seed?: string) => void;
  onEditEnd: () => void;
  onCommit: (value: unknown) => void;
}

export function EditableCell(props: EditableCellProps) {
  const { record, field, active, editing, onActivate, onEdit, onEditEnd, onCommit } = props;
  const value = record[field.name];
  const kind = editorKind(field);

  // bool toggles in place — no edit mode, immediate optimistic commit.
  if (kind === 'bool') {
    return (
      <CellShell active={ active } onActivate={ onActivate }>
        <Checkbox
          checked={ Boolean(value) }
          onCheckedChange={ (c) => onCommit(Boolean(c)) }
          aria-label={ field.name }
        />
      </CellShell>
    );
  }

  const editable = kind !== 'readonly' && kind !== 'file';
  const display = formatDisplay(value, field);

  return (
    <Popover open={ editing } onOpenChange={ (o) => { if (!o) onEditEnd(); } }>
      <PopoverAnchor asChild>
        <CellShell
          active={ active }
          editable={ editable }
          onActivate={ onActivate }
          onEdit={ editable ? onEdit : undefined }
        >
          <span className={ cn('block truncate', kind === 'json' && 'font-mono text-xs') }>
            { display || <span className="text-muted-foreground/50">—</span> }
          </span>
        </CellShell>
      </PopoverAnchor>
      { editing && editable && (
        <PopoverContent
          align="start"
          sideOffset={ -34 }
          className={ cn('w-72 p-0', kind === 'select' && 'w-56') }
          onOpenAutoFocus={ (e) => { if (kind === 'select') e.preventDefault(); } }
        >
          <CellEditor
            kind={ kind }
            field={ field }
            value={ value }
            onCommit={ (v) => { onCommit(v); onEditEnd(); } }
            onCancel={ onEditEnd }
          />
        </PopoverContent>
      ) }
    </Popover>
  );
}

// The cell frame: active ring, hover affordance, click/enter handling.
function CellShell({
  active,
  editable,
  onActivate,
  onEdit,
  children,
}: {
  active: boolean;
  editable?: boolean;
  onActivate: () => void;
  onEdit?: () => void;
  children: React.ReactNode;
}) {
  return (
    <div
      role="gridcell"
      tabIndex={ -1 }
      onMouseDown={ onActivate }
      onDoubleClick={ onEdit }
      className={ cn(
        'flex h-9 items-center px-3 text-sm outline-none',
        editable && 'cursor-text',
        active && 'ring-1 ring-inset ring-ring',
      ) }
    >
      { children }
    </div>
  );
}

interface EditorProps {
  kind: EditorKind;
  field: CollectionField;
  value: unknown;
  onCommit: (value: unknown) => void;
  onCancel: () => void;
}

function CellEditor({ kind, field, value, onCommit, onCancel }: EditorProps) {
  const [draft, setDraft] = useState(() => toEditorString(value, field));
  const [error, setError] = useState('');
  const firstRef = useRef<HTMLInputElement & HTMLTextAreaElement>(null);

  useEffect(() => { firstRef.current?.focus(); firstRef.current?.select?.(); }, []);

  const commit = (raw: unknown) => {
    try {
      onCommit(coerceForApi(raw, field));
    } catch {
      setError('Invalid JSON');
    }
  };

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') { e.preventDefault(); onCancel(); }
    else if (e.key === 'Enter' && kind !== 'textarea' && kind !== 'json') {
      e.preventDefault();
      commit(draft);
    } else if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
      e.preventDefault();
      commit(draft);
    }
  };

  if (kind === 'select' && !isMultiValue(field)) {
    const opts = selectValues(field);
    return (
      <Select
        defaultValue={ String(value ?? '') }
        onValueChange={ (v) => onCommit(coerceForApi(v, field)) }
        open
        onOpenChange={ (o) => { if (!o) onCancel(); } }
      >
        <SelectTrigger className="border-0 shadow-none focus:ring-0">
          <SelectValue placeholder="Select…" />
        </SelectTrigger>
        <SelectContent>
          { opts.map((o) => (
            <SelectItem key={ o } value={ o }>{ o }</SelectItem>
          )) }
        </SelectContent>
      </Select>
    );
  }

  if (kind === 'textarea' || kind === 'json') {
    return (
      <div className="flex flex-col gap-1 p-2">
        <Textarea
          ref={ firstRef }
          value={ draft }
          onChange={ (e) => { setDraft(e.target.value); setError(''); } }
          onKeyDown={ onKeyDown }
          rows={ 5 }
          className={ cn('resize-none border-0 shadow-none focus-visible:ring-0', kind === 'json' && 'font-mono text-xs') }
          placeholder={ kind === 'json' ? '{ }' : '' }
        />
        { error && <span className="px-1 text-xs text-destructive">{ error }</span> }
        <div className="flex justify-between px-1 text-[11px] text-muted-foreground">
          <span>⌘↵ save · esc cancel</span>
          <button className="hover:text-foreground" onMouseDown={ (e) => { e.preventDefault(); commit(draft); } }>
            Save
          </button>
        </div>
      </div>
    );
  }

  // text / number / date / relation / multi-select → single input line.
  return (
    <div className="flex flex-col gap-1 p-1.5">
      <Input
        ref={ firstRef }
        value={ draft }
        onChange={ (e) => setDraft(e.target.value) }
        onKeyDown={ onKeyDown }
        onBlur={ () => commit(draft) }
        type={ kind === 'number' ? 'number' : kind === 'date' ? 'datetime-local' : 'text' }
        step={ kind === 'number' ? 'any' : undefined }
        placeholder={ kind === 'relation' ? 'record id(s), comma-separated' : '' }
        className="h-8 border-0 shadow-none focus-visible:ring-0"
      />
    </div>
  );
}
