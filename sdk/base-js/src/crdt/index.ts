/**
 * @hanzoai/base/crdt -- CRDT primitives for offline-first sync.
 *
 * Provides:
 * - HybridLogicalClock for causal ordering
 * - Operation types for wire format
 * - CRDTText (RGA-based collaborative text)
 * - CRDTCounter (PN-Counter)
 * - CRDTSet (OR-Set with add-wins semantics)
 * - CRDTRegister (LWW-Register)
 * - CRDTDocument (container for collaborative fields)
 */

// Clock
export { HybridLogicalClock, compareHLC } from './clock.js'
export type { HLCTimestamp } from './clock.js'

// Operations
export { makeOpId, makePositionId } from './operations.js'
export type {
  OperationType,
  Operation,
  OperationPayload,
  TextInsertPayload,
  TextDeletePayload,
  CounterIncrementPayload,
  SetAddPayload,
  SetRemovePayload,
  RegisterSetPayload,
} from './operations.js'

// CRDT types
export { CRDTText } from './text.js'
export type { TextChangeCallback } from './text.js'

export { CRDTCounter } from './counter.js'
export type { CounterChangeCallback } from './counter.js'

export { CRDTSet } from './set.js'
export type { SetChangeCallback } from './set.js'

export { CRDTRegister } from './register.js'
export type { RegisterChangeCallback } from './register.js'

export { CRDTDocument } from './document.js'
export type { DocumentChangeCallback } from './document.js'

// Sync
export { CRDTSync } from './sync.js'
export type {
  SyncState,
  PeerState,
  SyncStateCallback,
  PeerCallback,
  RemoteChangeCallback,
} from './sync.js'
