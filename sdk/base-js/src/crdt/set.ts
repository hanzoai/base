/**
 * CRDTSet<T> -- Observed-Remove Set (OR-Set).
 *
 * Each add() generates a unique tag. A remove() records the set of tags
 * observed for that value at removal time. The value is present iff it has
 * any tag not covered by a remove.
 *
 * This gives add-wins semantics: a concurrent add and remove of the same
 * value results in the value being present (the new add's tag survives).
 */

import { HybridLogicalClock } from './clock.js'
import type { Operation, SetAddPayload, SetRemovePayload } from './operations.js'
import { makeOpId } from './operations.js'

export type SetChangeCallback<T> = (values: T[]) => void

interface TaggedEntry<T> {
  value: T
  tags: Set<string>
}

export class CRDTSet<T = unknown> {
  /** Map from serialized value key to tagged entry. */
  private _entries = new Map<string, TaggedEntry<T>>()

  private readonly _clock: HybridLogicalClock
  private readonly _documentId: string
  private readonly _field: string
  private readonly _pendingOps: Operation[] = []
  private _listeners = new Set<SetChangeCallback<T>>()

  constructor(documentId: string, field: string, clock: HybridLogicalClock) {
    this._documentId = documentId
    this._field = field
    this._clock = clock
  }

  // ---- Public API ---------------------------------------------------------

  get values(): T[] {
    const result: T[] = []
    for (const entry of this._entries.values()) {
      if (entry.tags.size > 0) {
        result.push(entry.value)
      }
    }
    return result
  }

  get size(): number {
    let count = 0
    for (const entry of this._entries.values()) {
      if (entry.tags.size > 0) count++
    }
    return count
  }

  has(item: T): boolean {
    const key = this._keyOf(item)
    const entry = this._entries.get(key)
    return entry !== undefined && entry.tags.size > 0
  }

  add(item: T): Operation {
    const hlc = this._clock.now()
    const tag = makeOpId(hlc)
    const key = this._keyOf(item)

    let entry = this._entries.get(key)
    if (!entry) {
      entry = { value: item, tags: new Set() }
      this._entries.set(key, entry)
    }
    entry.tags.add(tag)

    const op: Operation = {
      id: tag,
      documentId: this._documentId,
      field: this._field,
      hlc,
      type: 'set.add',
      payload: { value: item, tag } satisfies SetAddPayload,
    }

    this._pendingOps.push(op)
    this._notifyChange()
    return op
  }

  remove(item: T): Operation {
    const key = this._keyOf(item)
    const entry = this._entries.get(key)
    const observedTags = entry ? Array.from(entry.tags) : []

    // Remove all observed tags.
    if (entry) {
      entry.tags.clear()
    }

    const hlc = this._clock.now()
    const op: Operation = {
      id: makeOpId(hlc),
      documentId: this._documentId,
      field: this._field,
      hlc,
      type: 'set.remove',
      payload: { value: item, tags: observedTags } satisfies SetRemovePayload,
    }

    this._pendingOps.push(op)
    this._notifyChange()
    return op
  }

  /** Apply a remote operation. */
  applyRemote(op: Operation): void {
    this._clock.receive(op.hlc)

    if (op.type === 'set.add') {
      const payload = op.payload as SetAddPayload
      const key = this._keyOf(payload.value as T)
      let entry = this._entries.get(key)
      if (!entry) {
        entry = { value: payload.value as T, tags: new Set() }
        this._entries.set(key, entry)
      }
      entry.tags.add(payload.tag)
    } else if (op.type === 'set.remove') {
      const payload = op.payload as SetRemovePayload
      const key = this._keyOf(payload.value as T)
      const entry = this._entries.get(key)
      if (entry) {
        for (const tag of payload.tags) {
          entry.tags.delete(tag)
        }
      }
    }

    this._notifyChange()
  }

  drainOps(): Operation[] {
    return this._pendingOps.splice(0, this._pendingOps.length)
  }

  onChange(callback: SetChangeCallback<T>): () => void {
    this._listeners.add(callback)
    return () => {
      this._listeners.delete(callback)
    }
  }

  // ---- Internal -----------------------------------------------------------

  private _keyOf(value: T): string {
    // Use JSON serialization as the key for value identity.
    return JSON.stringify(value)
  }

  private _notifyChange(): void {
    if (this._listeners.size === 0) return
    const vals = this.values
    for (const cb of this._listeners) {
      try {
        cb(vals)
      } catch {
        // listener errors must not break notification
      }
    }
  }
}
