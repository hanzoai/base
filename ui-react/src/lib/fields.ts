// Field-type helpers shared by the record grid and the detail panel. Maps
// Base/PocketBase field types to an editor kind and provides display
// formatting + value coercion so cell rendering stays orthogonal to the grid.
import type { CollectionField } from '~/lib/base';

export type EditorKind =
  | 'text'
  | 'number'
  | 'bool'
  | 'select'
  | 'date'
  | 'textarea'
  | 'json'
  | 'relation'
  | 'file'
  | 'readonly';

// Which editor a field type opens for inline editing.
export function editorKind(field: CollectionField): EditorKind {
  switch (field.type) {
    case 'text':
    case 'email':
    case 'url':
      return 'text';
    case 'editor':
      return 'textarea';
    case 'number':
      return 'number';
    case 'bool':
      return 'bool';
    case 'select':
      return 'select';
    case 'date':
      return 'date';
    case 'json':
    case 'geoPoint':
      return 'json';
    case 'relation':
      return 'relation';
    case 'file':
      return 'file';
    default:
      // autodate, password and anything unknown are not inline-editable.
      return 'readonly';
  }
}

export function isInlineEditable(field: CollectionField): boolean {
  const k = editorKind(field);
  return k !== 'readonly' && k !== 'file';
}

export function isMultiValue(field: CollectionField): boolean {
  const max = (field as Record<string, unknown>).maxSelect;
  return typeof max === 'number' ? max !== 1 : field.type === 'relation';
}

export function selectValues(field: CollectionField): string[] {
  const v = (field as Record<string, unknown>).values;
  return Array.isArray(v) ? (v as string[]) : [];
}

// A compact, human-readable string for a cell in display mode.
export function formatDisplay(value: unknown, field: CollectionField): string {
  if (value === null || value === undefined || value === '') return '';
  switch (editorKind(field)) {
    case 'bool':
      return value ? 'true' : 'false';
    case 'json':
      return typeof value === 'string' ? value : JSON.stringify(value);
    case 'file':
      return Array.isArray(value) ? `${value.length} file(s)` : String(value);
    case 'relation':
      return Array.isArray(value) ? value.join(', ') : String(value);
    case 'date':
      return String(value).replace('T', ' ').replace('Z', '').slice(0, 19);
    default:
      return String(value);
  }
}

// Convert an editor's raw string/checkbox value into the payload the API wants.
export function coerceForApi(raw: unknown, field: CollectionField): unknown {
  switch (editorKind(field)) {
    case 'number': {
      if (raw === '' || raw === null || raw === undefined) return null;
      const n = Number(raw);
      return Number.isNaN(n) ? null : n;
    }
    case 'bool':
      return Boolean(raw);
    case 'json': {
      if (typeof raw !== 'string' || raw.trim() === '') return null;
      return JSON.parse(raw); // throws on invalid — surfaced to the caller
    }
    case 'select':
    case 'relation': {
      if (!isMultiValue(field)) return raw === '' ? null : raw;
      if (Array.isArray(raw)) return raw;
      return String(raw)
        .split(',')
        .map((s) => s.trim())
        .filter(Boolean);
    }
    default:
      return raw;
  }
}

// The initial editor string for a value entering edit mode.
export function toEditorString(value: unknown, field: CollectionField): string {
  if (value === null || value === undefined) return '';
  switch (editorKind(field)) {
    case 'json':
      return typeof value === 'string' ? value : JSON.stringify(value, null, 2);
    case 'relation':
      return Array.isArray(value) ? value.join(', ') : String(value);
    case 'date':
      // API returns "2026-01-02 15:04:05Z"; datetime-local wants "2026-01-02T15:04".
      return String(value).replace(' ', 'T').slice(0, 16);
    default:
      return String(value);
  }
}
