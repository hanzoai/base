/**
 * Schema types — collection + field model shapes the admin UI needs.
 *
 * These were previously imported from `pocketbase` as `CollectionModel`
 * / `CollectionField`. Defining them natively in @hanzo/base so the
 * SDK has no upstream dependency.
 */

import type { BaseRecord } from './state.js'

/**
 * A single field in a collection schema.
 *
 * Field types and their option shapes mirror the Base admin API
 * (`/api/collections/{id}` → `fields[]`), so existing admin UIs read
 * the structure unchanged.
 */
export interface CollectionField {
  id: string
  name: string
  type:
    | 'text'
    | 'number'
    | 'bool'
    | 'email'
    | 'url'
    | 'editor'
    | 'date'
    | 'autodate'
    | 'select'
    | 'json'
    | 'file'
    | 'relation'
    | 'password'
  system?: boolean
  required?: boolean
  presentable?: boolean
  hidden?: boolean
  unique?: boolean
  // Per-type options (subset, intentionally permissive — additional fields are passed through)
  min?: number | string | null
  max?: number | string | null
  pattern?: string
  maxSelect?: number
  values?: string[]
  collectionId?: string
  cascadeDelete?: boolean
  minSelect?: number
  options?: Record<string, unknown>
  // Anything else the backend chose to attach (autodate `onCreate`,
  // file `mimeTypes`, etc.) — kept as-is so admin pages round-trip.
  [key: string]: unknown
}

/**
 * A collection definition. Mirrors the Base admin API
 * `/api/collections/{id}` payload.
 *
 * `type` selects the back-end behaviour: `base` is a regular collection,
 * `auth` is an auth collection (users), `view` is a virtual read-only
 * SQL-defined view.
 */
export interface CollectionModel {
  id: string
  name: string
  type: 'base' | 'auth' | 'view'
  system?: boolean
  fields: CollectionField[]
  indexes?: string[]
  listRule?: string | null
  viewRule?: string | null
  createRule?: string | null
  updateRule?: string | null
  deleteRule?: string | null
  // Auth-collection options (login methods, token durations, etc.).
  // Permissive — the admin UI persists whatever the backend returns.
  authRule?: string | null
  manageRule?: string | null
  authAlert?: { enabled?: boolean; emailTemplate?: Record<string, unknown> }
  oauth2?: Record<string, unknown>
  passwordAuth?: { enabled?: boolean; identityFields?: string[] }
  otp?: Record<string, unknown>
  mfa?: Record<string, unknown>
  fileToken?: Record<string, unknown>
  authToken?: Record<string, unknown>
  passwordResetToken?: Record<string, unknown>
  emailChangeToken?: Record<string, unknown>
  verificationToken?: Record<string, unknown>
  // View-collection options
  viewQuery?: string
  // Generic catchall (admin form fields the SDK doesn't model explicitly).
  [key: string]: unknown
}

/**
 * Alias for `BaseRecord` so consumers migrating from the upstream
 * client can keep using `RecordModel`. New code should prefer
 * `BaseRecord` from `./state.js`.
 */
export type RecordModel = BaseRecord
