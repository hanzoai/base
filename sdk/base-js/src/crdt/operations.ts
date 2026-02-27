/**
 * CRDT operation types -- wire format for sync between client and server.
 *
 * All operations are serializable to JSON and designed for
 * eventual compatibility with a Go CRDT server.
 */

import type { HLCTimestamp } from './clock.js'

// ---------------------------------------------------------------------------
// Operation type tags
// ---------------------------------------------------------------------------

export type OperationType =
  | 'text.insert'
  | 'text.delete'
  | 'counter.increment'
  | 'set.add'
  | 'set.remove'
  | 'register.set'

// ---------------------------------------------------------------------------
// Base operation
// ---------------------------------------------------------------------------

export interface Operation {
  /** Unique operation id: `{siteId}:{ts}:{counter}` */
  id: string
  /** The CRDT document id this operation belongs to. */
  documentId: string
  /** Which field within the document. */
  field: string
  /** HLC timestamp for causal ordering. */
  hlc: HLCTimestamp
  /** Operation type tag. */
  type: OperationType
  /** Type-specific payload. */
  payload: OperationPayload
}

// ---------------------------------------------------------------------------
// Payloads
// ---------------------------------------------------------------------------

export interface TextInsertPayload {
  /** Position id of the character after which to insert (null = head). */
  afterId: string | null
  /** The text content to insert. Each character gets its own positional id. */
  content: string
  /** Assigned position ids for each character (same length as content). */
  positionIds: string[]
}

export interface TextDeletePayload {
  /** Position ids of the characters to tombstone. */
  positionIds: string[]
}

export interface CounterIncrementPayload {
  /** Delta (positive for increment, negative for decrement). */
  delta: number
}

export interface SetAddPayload {
  /** The value to add (JSON-serializable). */
  value: unknown
  /** Unique tag for this add operation (for OR-Set semantics). */
  tag: string
}

export interface SetRemovePayload {
  /** The value to remove. */
  value: unknown
  /** Tags being causally removed (observed tags at removal time). */
  tags: string[]
}

export interface RegisterSetPayload {
  /** The new value. */
  value: unknown
}

export type OperationPayload =
  | TextInsertPayload
  | TextDeletePayload
  | CounterIncrementPayload
  | SetAddPayload
  | SetRemovePayload
  | RegisterSetPayload

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

export function makeOpId(hlc: HLCTimestamp): string {
  return `${hlc.siteId}:${hlc.ts}:${hlc.counter}`
}

export function makePositionId(hlc: HLCTimestamp, charIndex: number): string {
  return `${hlc.siteId}:${hlc.ts}:${hlc.counter}:${charIndex}`
}
