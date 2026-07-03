// RecordGrid — a Twenty-grade editable data grid. Two-tier keyboard cursor
// (arrows move the active cell; Enter / typing enters edit mode), sortable
// columns, row selection, per-row actions. Cells own the display/edit split
// (EditableCell); the grid owns the cursor and the commit fan-out.
import { ArrowDown, ArrowUp, ChevronsUpDown, MoreHorizontal } from 'lucide-react';
import { useCallback, useRef, useState } from 'react';

import { EditableCell } from '~/components/grid/EditableCell';
import { Checkbox } from '~/components/ui/checkbox';
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '~/components/ui/dropdown-menu';
import { cn } from '~/lib/cn';
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
      className="overflow-auto rounded-lg border border-border outline-none focus-visible:ring-1 focus-visible:ring-ring"
    >
      <table className="w-full border-collapse text-sm">
        <thead className="sticky top-0 z-10 bg-card">
          <tr className="border-b border-border">
            <th className="w-10 px-3 py-2">
              <Checkbox
                checked={ allSelected }
                onCheckedChange={ onToggleAll }
                disabled={ records.length === 0 }
                aria-label="Select all"
              />
            </th>
            { fields.map((f) => (
              <SortHeader key={ f.id } field={ f } sort={ sort } onSort={ onSort } />
            )) }
            <th className="w-10" />
          </tr>
        </thead>
        <tbody>
          { records.map((record, r) => (
            <tr
              key={ record.id }
              className={ cn(
                'border-b border-border/60 last:border-0 hover:bg-accent/40',
                selected.has(record.id) && 'bg-accent/30',
              ) }
            >
              <td className="px-3">
                <Checkbox
                  checked={ selected.has(record.id) }
                  onCheckedChange={ () => onToggleSelect(record.id) }
                  aria-label="Select row"
                />
              </td>
              { fields.map((field, c) => (
                <td key={ field.id } className="max-w-[22rem] p-0">
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
              <td className="px-1">
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
              <td colSpan={ cols + 2 } className="px-3 py-16 text-center text-muted-foreground">
                No records yet.
              </td>
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
  return (
    <th className="whitespace-nowrap px-3 py-2 text-left font-medium text-muted-foreground">
      <button
        type="button"
        onClick={ () => onSort(field.name) }
        className="inline-flex items-center gap-1 hover:text-foreground"
      >
        <span>{ field.name }</span>
        <span className="text-[10px] uppercase text-muted-foreground/60">{ field.type }</span>
        { asc ? <ArrowUp className="size-3" /> : desc ? <ArrowDown className="size-3" /> : <ChevronsUpDown className="size-3 opacity-40" /> }
      </button>
    </th>
  );
}

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
    <DropdownMenu>
      <DropdownMenuTrigger className="flex size-7 items-center justify-center rounded-md text-muted-foreground outline-none hover:bg-accent hover:text-foreground focus-visible:ring-1 focus-visible:ring-ring">
        <MoreHorizontal className="size-4" />
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuItem onSelect={ onEdit }>Open</DropdownMenuItem>
        { !isView && <DropdownMenuItem onSelect={ onDuplicate }>Duplicate</DropdownMenuItem> }
        { !isView && <DropdownMenuSeparator /> }
        { !isView && (
          <DropdownMenuItem destructive onSelect={ onDelete }>Delete</DropdownMenuItem>
        ) }
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
