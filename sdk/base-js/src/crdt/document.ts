/**
 * CRDTDocument -- container for collaborative fields.
 *
 * A document holds named CRDT fields (text, counter, set, register) that
 * share a single HLC clock and sync channel. Operations from all fields
 * are collected and sent to the server together.
 */

import { HybridLogicalClock } from './clock.js'
import { CRDTText } from './text.js'
import { CRDTCounter } from './counter.js'
import { CRDTSet } from './set.js'
import { CRDTRegister } from './register.js'
import type { Operation } from './operations.js'

export type DocumentChangeCallback = (ops: Operation[]) => void

export class CRDTDocument {
  readonly id: string
  readonly clock: HybridLogicalClock

  private _texts = new Map<string, CRDTText>()
  private _counters = new Map<string, CRDTCounter>()
  private _sets = new Map<string, CRDTSet>()
  private _registers = new Map<string, CRDTRegister>()
  private _listeners = new Set<DocumentChangeCallback>()

  /** Buffer for ops not yet synced. */
  private _unsyncedOps: Operation[] = []

  constructor(id: string, siteId?: string) {
    this.id = id
    this.clock = new HybridLogicalClock(siteId)
  }

  // ---- Field accessors ----------------------------------------------------

  getText(field: string): CRDTText {
    let t = this._texts.get(field)
    if (!t) {
      t = new CRDTText(this.id, field, this.clock)
      this._texts.set(field, t)
    }
    return t
  }

  getCounter(field: string): CRDTCounter {
    let c = this._counters.get(field)
    if (!c) {
      c = new CRDTCounter(this.id, field, this.clock)
      this._counters.set(field, c)
    }
    return c
  }

  getSet<T = unknown>(field: string): CRDTSet<T> {
    let s = this._sets.get(field)
    if (!s) {
      s = new CRDTSet(this.id, field, this.clock)
      this._sets.set(field, s)
    }
    return s as CRDTSet<T>
  }

  getRegister<T = unknown>(field: string): CRDTRegister<T> {
    let r = this._registers.get(field)
    if (!r) {
      r = new CRDTRegister(this.id, field, this.clock)
      this._registers.set(field, r)
    }
    return r as CRDTRegister<T>
  }

  // ---- Operation collection -----------------------------------------------

  /**
   * Collect all pending local operations from all fields.
   * Drains the per-field op buffers and returns them.
   */
  collectOps(): Operation[] {
    const ops: Operation[] = []
    for (const t of this._texts.values()) ops.push(...t.drainOps())
    for (const c of this._counters.values()) ops.push(...c.drainOps())
    for (const s of this._sets.values()) ops.push(...s.drainOps())
    for (const r of this._registers.values()) ops.push(...r.drainOps())
    this._unsyncedOps.push(...ops)
    return ops
  }

  /**
   * Acknowledge that operations up to and including `opId` have been
   * persisted by the server. Removes them from the unsynced buffer.
   */
  acknowledge(opId: string): void {
    const idx = this._unsyncedOps.findIndex((op) => op.id === opId)
    if (idx >= 0) {
      this._unsyncedOps.splice(0, idx + 1)
    }
  }

  /** Get operations that have not been acknowledged by the server. */
  getUnsyncedOps(): readonly Operation[] {
    return this._unsyncedOps
  }

  // ---- Remote operation application ---------------------------------------

  /**
   * Apply a batch of remote operations to the appropriate CRDT fields.
   */
  applyRemoteOps(ops: Operation[]): void {
    for (const op of ops) {
      if (op.documentId !== this.id) continue
      this._applyRemoteOp(op)
    }
  }

  private _applyRemoteOp(op: Operation): void {
    switch (op.type) {
      case 'text.insert':
      case 'text.delete': {
        const t = this.getText(op.field)
        t.applyRemote(op)
        break
      }
      case 'counter.increment': {
        const c = this.getCounter(op.field)
        c.applyRemote(op)
        break
      }
      case 'set.add':
      case 'set.remove': {
        const s = this.getSet(op.field)
        s.applyRemote(op)
        break
      }
      case 'register.set': {
        const r = this.getRegister(op.field)
        r.applyRemote(op)
        break
      }
    }
  }

  // ---- Serialization ------------------------------------------------------

  /**
   * Encode all pending operations as a Uint8Array (JSON wire format).
   * Used for WebSocket transmission.
   */
  encode(): Uint8Array {
    const ops = this.collectOps()
    const json = JSON.stringify({
      documentId: this.id,
      siteId: this.clock.siteId,
      ops,
    })
    return new TextEncoder().encode(json)
  }

  /**
   * Decode and apply a remote message (Uint8Array or string).
   */
  decode(data: Uint8Array | string): void {
    const text = typeof data === 'string' ? data : new TextDecoder().decode(data)
    const msg = JSON.parse(text) as { documentId: string; siteId: string; ops: Operation[] }

    if (msg.documentId !== this.id) return
    // Do not apply our own operations.
    if (msg.siteId === this.clock.siteId) return

    this.applyRemoteOps(msg.ops)
    this._notifyChange(msg.ops)
  }

  // ---- Change notifications -----------------------------------------------

  onChange(callback: DocumentChangeCallback): () => void {
    this._listeners.add(callback)
    return () => {
      this._listeners.delete(callback)
    }
  }

  private _notifyChange(ops: Operation[]): void {
    for (const cb of this._listeners) {
      try {
        cb(ops)
      } catch {
        // listener errors must not break notification
      }
    }
  }
}
