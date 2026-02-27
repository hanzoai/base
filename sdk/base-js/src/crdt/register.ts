/**
 * CRDTRegister<T> -- Last-Writer-Wins Register (LWW-Register).
 *
 * The value with the highest HLC timestamp wins.
 * Ties are broken by site id comparison.
 */

import type { HLCTimestamp } from './clock.js'
import { HybridLogicalClock, compareHLC } from './clock.js'
import type { Operation, RegisterSetPayload } from './operations.js'
import { makeOpId } from './operations.js'

export type RegisterChangeCallback<T> = (value: T | undefined) => void

export class CRDTRegister<T = unknown> {
  private _value: T | undefined = undefined
  private _hlc: HLCTimestamp | null = null

  private readonly _clock: HybridLogicalClock
  private readonly _documentId: string
  private readonly _field: string
  private readonly _pendingOps: Operation[] = []
  private _listeners = new Set<RegisterChangeCallback<T>>()

  constructor(documentId: string, field: string, clock: HybridLogicalClock) {
    this._documentId = documentId
    this._field = field
    this._clock = clock
  }

  // ---- Public API ---------------------------------------------------------

  get value(): T | undefined {
    return this._value
  }

  set(value: T): Operation {
    const hlc = this._clock.now()
    this._value = value
    this._hlc = hlc

    const op: Operation = {
      id: makeOpId(hlc),
      documentId: this._documentId,
      field: this._field,
      hlc,
      type: 'register.set',
      payload: { value } satisfies RegisterSetPayload,
    }

    this._pendingOps.push(op)
    this._notifyChange()
    return op
  }

  /** Apply a remote set operation. LWW: only apply if remote HLC > current. */
  applyRemote(op: Operation): void {
    if (op.type !== 'register.set') return
    this._clock.receive(op.hlc)

    const payload = op.payload as RegisterSetPayload

    if (this._hlc === null || compareHLC(op.hlc, this._hlc) > 0) {
      this._value = payload.value as T
      this._hlc = op.hlc
      this._notifyChange()
    }
  }

  /** Export current state for full-state sync. */
  exportState(): { value: T | undefined; hlc: HLCTimestamp | null } {
    return { value: this._value, hlc: this._hlc }
  }

  /** Import state from full-state sync. LWW merge. */
  importState(value: T, hlc: HLCTimestamp): void {
    if (this._hlc === null || compareHLC(hlc, this._hlc) > 0) {
      this._value = value
      this._hlc = hlc
      this._notifyChange()
    }
  }

  drainOps(): Operation[] {
    return this._pendingOps.splice(0, this._pendingOps.length)
  }

  onChange(callback: RegisterChangeCallback<T>): () => void {
    this._listeners.add(callback)
    return () => {
      this._listeners.delete(callback)
    }
  }

  private _notifyChange(): void {
    if (this._listeners.size === 0) return
    const val = this._value
    for (const cb of this._listeners) {
      try {
        cb(val)
      } catch {
        // listener errors must not break notification
      }
    }
  }
}
