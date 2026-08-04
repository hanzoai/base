// RecordGrid — an editable data grid. Two-tier keyboard cursor (arrows move the
// active cell; Enter / typing enters edit mode), sortable columns, row selection,
// per-row actions. Cells own the display/edit split (EditableCell); the grid owns
// the cursor and the commit fan-out.
import { ArrowDown, ArrowUp, ChevronsUpDown, MoreHorizontal } from '@hanzogui/lucide-icons-2';
import { DropdownMenu } from '@hanzo/ui';
import { useCallback, useRef, useState } from 'react';

import { EditableCell } from '~/components/grid/EditableCell';
import { isInlineEditable } from '~/lib/fields';
import type { CollectionField, RecordModel } from '~/lib/base';

interface RecordGridProps {
  fields: CollectionField[];
  records: RecordModel[];
  sort: string;
  onSort: (fieldName: string) => void;
  selected: Set<string>;
  onToggleSelect: (id: string) => void;
  onToggleAll: () => void;
  onCommitCell: (record: RecordModel, field: CollectionField, value: unknown) => void;
  onEditRecord: (record: RecordModel) => void;
  onDuplicate: (record: RecordModel) => void;
  onDelete: (record: RecordModel) => void;
  isView: boolean;
}

export function RecordGrid(props: RecordGridProps) {
  const { fields, records, sort, onSort, selected, onToggleSelect, onToggleAll, onCommitCell } = props;

  // Two-tier cursor: `cursor` is the soft focus; `editing` is the hard focus.
  const [cursor, setCursor] = useState<{ r: number; c: number } | null>(null);
  const [editing, setEditing] = useState(false);
  const gridRef = useRef<HTMLDivElement>(null);

  const rows = records.length;
  const cols = fields.length;

  const move = useCallback(
    (dr: number, dc: number) => {
      setCursor((cur) => {
        const r = Math.min(Math.max((cur?.r ?? 0) + dr, 0), Math.max(rows - 1, 0));
        const c = Math.min(Math.max((cur?.c ?? 0) + dc, 0), Math.max(cols - 1, 0));
        return { r, c };
      });
    },
    [rows, cols],
  );

  const onKeyDown = (e: React.KeyboardEvent) => {
    if (editing) return; // editor owns the keys while open
    switch (e.key) {
      case 'ArrowDown': e.preventDefault(); move(1, 0); break;
      case 'ArrowUp': e.preventDefault(); move(-1, 0); break;
      case 'ArrowLeft': e.preventDefault(); move(0, -1); break;
      case 'ArrowRight':
      case 'Tab': e.preventDefault(); move(0, 1); break;
      case 'Enter':
      case 'F2':
        if (cursor && isInlineEditable(fields[cursor.c])) { e.preventDefault(); setEditing(true); }
        break;
      default:
        // type-to-edit: a lone printable character opens the editor.
        if (e.key.length === 1 && !e.metaKey && !e.ctrlKey && !e.altKey && cursor && isInlineEditable(fields[cursor.c])) {
          setEditing(true);
        }
    }
  };

  const allSelected = records.length > 0 && records.every((r) => selected.has(r.id));

  return (
    <div
      ref={ gridRef }
      role="grid"
      tabIndex={ 0 }
      onKeyDown={ onKeyDown }
      className="table-wrap"
    >
      <table className="table">
        <thead>
          <tr>
            <th className="gutter">
              <input
                type="checkbox"
                checked={ allSelected }
                onChange={ onToggleAll }
                disabled={ records.length === 0 }
                aria-label="Select all"
              />
            </th>
            { fields.map((f) => (
              <SortHeader key={ f.id } field={ f } sort={ sort } onSort={ onSort } />
            )) }
            <th className="gutter" />
          </tr>
        </thead>
        <tbody>
          { records.map((record, r) => (
            <tr key={ record.id } data-selected={ selected.has(record.id) ? 'true' : undefined }>
              <td className="gutter">
                <input
                  type="checkbox"
                  checked={ selected.has(record.id) }
                  onChange={ () => onToggleSelect(record.id) }
                  aria-label="Select row"
                />
              </td>
              { fields.map((field, c) => (
                <td key={ field.id } className="cell">
                  <EditableCell
                    record={ record }
                    field={ field }
                    active={ cursor?.r === r && cursor?.c === c }
                    editing={ editing && cursor?.r === r && cursor?.c === c }
                    onActivate={ () => { setCursor({ r, c }); setEditing(false); gridRef.current?.focus(); } }
                    onEdit={ () => { setCursor({ r, c }); setEditing(true); } }
                    onEditEnd={ () => { setEditing(false); gridRef.current?.focus(); } }
                    onCommit={ (v) => onCommitCell(record, field, v) }
                  />
                </td>
              )) }
              <td className="gutter">
                <RowActions
                  isView={ props.isView }
                  onEdit={ () => props.onEditRecord(record) }
                  onDuplicate={ () => props.onDuplicate(record) }
                  onDelete={ () => props.onDelete(record) }
                />
              </td>
            </tr>
          )) }
          { records.length === 0 && (
            <tr>
              <td colSpan={ cols + 2 } className="empty">No records yet.</td>
            </tr>
          ) }
        </tbody>
      </table>
    </div>
  );
}

function SortHeader({
  field,
  sort,
  onSort,
}: {
  field: CollectionField;
  sort: string;
  onSort: (name: string) => void;
}) {
  const asc = sort === field.name;
  const desc = sort === `-${field.name}`;
  // Numeric headers right-align to sit over their right-aligned tabular cells.
  const numeric = field.type === 'number' ? 'true' : undefined;
  return (
    <th data-numeric={ numeric }>
      <button type="button" onClick={ () => onSort(field.name) } className="th-sort" data-numeric={ numeric }>
        <span>{ field.name }</span>
        <span className="type-tag">{ field.type }</span>
        { asc ? <ArrowUp size={ 12 } /> : desc ? <ArrowDown size={ 12 } /> : <ChevronsUpDown size={ 12 } opacity={ 0.4 } /> }
      </button>
    </th>
  );
}

// `items` is DropdownMenu's declarative form — the menu builds itself out of the
// same compound parts a hand-written tree would use, so the row's actions are
// stated as data rather than as markup.
function RowActions({
  isView,
  onEdit,
  onDuplicate,
  onDelete,
}: {
  isView: boolean;
  onEdit: () => void;
  onDuplicate: () => void;
  onDelete: () => void;
}) {
  return (
    <DropdownMenu
      trigger={
        <button type="button" className="icon-btn" aria-label="Row actions">
          <MoreHorizontal size={ 16 } />
        </button>
      }
      items={ [
        { key: 'open', label: 'Open', onSelect: onEdit },
        ...(isView ? [] : [
          { key: 'duplicate', label: 'Duplicate', onSelect: onDuplicate },
          { type: 'separator' as const, key: 'sep' },
          { key: 'delete', label: 'Delete', destructive: true, onSelect: onDelete },
        ]),
      ] }
    />
  );
}
