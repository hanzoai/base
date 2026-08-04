// EditableCell — the display/edit split. A cell renders its value; when the grid
// puts it in edit mode the cell BECOMES its editor, in place, with one
// commit/cancel protocol. bool toggles without entering edit mode at all.
//
// In place, not floating: the editor used to be a Radix Popover anchored to the
// cell and nudged back over it with `sideOffset={-34}`, which is a floating layer
// doing an impression of an inline one — it re-implemented the cell's geometry,
// fought the grid for focus, and needed `onOpenAutoFocus` escapes per field kind.
// Swapping the cell's own contents costs no positioning, no portal and no
// focus negotiation, and it is what the grid already looked like it was doing.
import { useEffect, useRef, useState } from 'react';

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
        <input
          type="checkbox"
          checked={ Boolean(value) }
          onChange={ (e) => onCommit(e.target.checked) }
          aria-label={ field.name }
        />
      </CellShell>
    );
  }

  const editable = kind !== 'readonly' && kind !== 'file';

  if (editing && editable) {
    return (
      <CellEditor
        kind={ kind }
        field={ field }
        value={ value }
        onCommit={ (v) => { onCommit(v); onEditEnd(); } }
        onCancel={ onEditEnd }
      />
    );
  }

  const display = formatDisplay(value, field);
  // Numeric columns right-align; numeric + date use tabular figures so digits
  // line up column-wise.
  const numeric = kind === 'number';
  const classes = [
    'truncate',
    numeric || kind === 'date' ? 'num' : '',
    kind === 'json' ? 'mono' : '',
  ].filter(Boolean).join(' ');

  return (
    <CellShell
      active={ active }
      editable={ editable }
      numeric={ numeric }
      onActivate={ onActivate }
      onEdit={ editable ? onEdit : undefined }
    >
      <span className={ classes }>
        { display || <span className="muted">—</span> }
      </span>
    </CellShell>
  );
}

// The cell frame: active ring, hover affordance, click/enter handling.
function CellShell({
  active,
  editable,
  numeric,
  onActivate,
  onEdit,
  children,
}: {
  active: boolean;
  editable?: boolean;
  numeric?: boolean;
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
      className="cell__inner"
      data-active={ active ? 'true' : undefined }
      data-editable={ editable ? 'true' : undefined }
      data-numeric={ numeric ? 'true' : undefined }
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

// The `type` a single-line editor needs, per field kind.
const INPUT_TYPE: Partial<Record<EditorKind, string>> = {
  number: 'number',
  date: 'datetime-local',
};

function CellEditor({ kind, field, value, onCommit, onCancel }: EditorProps) {
  const [draft, setDraft] = useState(() => toEditorString(value, field));
  const [error, setError] = useState('');
  const ref = useRef<HTMLInputElement | HTMLTextAreaElement | HTMLSelectElement>(null);

  useEffect(() => {
    ref.current?.focus();
    if (ref.current && 'select' in ref.current) ref.current.select();
  }, []);

  const commit = (raw: unknown) => {
    try {
      onCommit(coerceForApi(raw, field));
    } catch {
      setError('Invalid JSON');
    }
  };

  // One key protocol for every editor kind: Escape cancels, Enter commits —
  // except in the multiline editors, where Enter is a newline and ⌘/Ctrl↵ commits.
  const multiline = kind === 'textarea' || kind === 'json';
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') { e.preventDefault(); onCancel(); return; }
    if (e.key !== 'Enter') return;
    if (multiline && !(e.metaKey || e.ctrlKey)) return;
    e.preventDefault();
    commit(draft);
  };

  if (kind === 'select' && !isMultiValue(field)) {
    return (
      <select
        ref={ ref as React.Ref<HTMLSelectElement> }
        className="cell__edit"
        defaultValue={ String(value ?? '') }
        onChange={ (e) => commit(e.target.value) }
        onKeyDown={ onKeyDown }
        onBlur={ onCancel }
      >
        <option value="">—</option>
        { selectValues(field).map((o) => (
          <option key={ o } value={ o }>{ o }</option>
        )) }
      </select>
    );
  }

  if (multiline) {
    return (
      <div>
        <textarea
          ref={ ref as React.Ref<HTMLTextAreaElement> }
          className={ kind === 'json' ? 'cell__edit cell__edit--mono' : 'cell__edit' }
          value={ draft }
          onChange={ (e) => { setDraft(e.target.value); setError(''); } }
          onKeyDown={ onKeyDown }
          rows={ 5 }
          placeholder={ kind === 'json' ? '{ }' : '' }
        />
        <div className="row small muted" style={{ padding: 'var(--space-1) var(--space-3)' }}>
          <span>⌘↵ save · esc cancel</span>
          <button type="button" className="link push" onMouseDown={ (e) => { e.preventDefault(); commit(draft); } }>
            Save
          </button>
        </div>
        { error && <span className="danger small">{ error }</span> }
      </div>
    );
  }

  // text / number / date / relation / multi-select → one input line.
  return (
    <input
      ref={ ref as React.Ref<HTMLInputElement> }
      className="cell__edit"
      value={ draft }
      onChange={ (e) => setDraft(e.target.value) }
      onKeyDown={ onKeyDown }
      onBlur={ () => commit(draft) }
      type={ INPUT_TYPE[kind] ?? 'text' }
      step={ kind === 'number' ? 'any' : undefined }
      placeholder={ kind === 'relation' ? 'record id(s), comma-separated' : '' }
    />
  );
}
