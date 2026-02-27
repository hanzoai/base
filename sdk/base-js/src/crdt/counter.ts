/**
 * CRDTCounter -- PN-Counter (positive-negative counter).
 *
 * Each site tracks its own positive and negative totals.
 * The value is sum(positives) - sum(negatives) across all sites.
 * Merge is max() per-site per-direction.
 */

import { HybridLogicalClock } from './clock.js'
import type { Operation, CounterIncrementPayload } from './operations.js'
import { makeOpId } from './operations.js'

export type CounterChangeCallback = (value: number) => void

export class CRDTCounter {
  /** Per-site positive accumulator. */
  private _positive = new Map<string, number>()
  /** Per-site negative accumulator. */
  private _negative = new Map<string, number>()

  private readonly _clock: HybridLogicalClock
  private readonly _documentId: string
  private readonly _field: string
  private readonly _pendingOps: Operation[] = []
  private _listeners = new Set<CounterChangeCallback>()

  constructor(documentId: string, field: string, clock: HybridLogicalClock) {
    this._documentId = documentId
    this._field = field
    this._clock = clock
  }

  // ---- Public API ---------------------------------------------------------

  get value(): number {
    let pos = 0
    let neg = 0
    for (const v of this._positive.values()) pos += v
    for (const v of this._negative.values()) neg += v
    return pos - neg
  }

  increment(amount = 1): Operation {
    if (amount === 0) throw new Error('CRDTCounter: amount must be nonzero')

    const siteId = this._clock.siteId
    if (amount > 0) {
      this._positive.set(siteId, (this._positive.get(siteId) ?? 0) + amount)
    } else {
      this._negative.set(siteId, (this._negative.get(siteId) ?? 0) + Math.abs(amount))
    }

    const hlc = this._clock.now()
    const op: Operation = {
      id: makeOpId(hlc),
      documentId: this._documentId,
      field: this._field,
      hlc,
      type: 'counter.increment',
      payload: { delta: amount } satisfies CounterIncrementPayload,
    }

    this._pendingOps.push(op)
    this._notifyChange()
    return op
  }

  decrement(amount = 1): Operation {
    return this.increment(-amount)
  }

  /** Apply a remote increment operation. */
  applyRemote(op: Operation): void {
    if (op.type !== 'counter.increment') return
    this._clock.receive(op.hlc)

    const payload = op.payload as CounterIncrementPayload
    const siteId = op.hlc.siteId

    if (payload.delta > 0) {
      this._positive.set(siteId, (this._positive.get(siteId) ?? 0) + payload.delta)
    } else {
      this._negative.set(siteId, (this._negative.get(siteId) ?? 0) + Math.abs(payload.delta))
    }

    this._notifyChange()
  }

  /** Merge with a remote counter state (for full-state sync). */
  mergeState(positives: Record<string, number>, negatives: Record<string, number>): void {
    for (const [site, val] of Object.entries(positives)) {
      this._positive.set(site, Math.max(this._positive.get(site) ?? 0, val))
    }
    for (const [site, val] of Object.entries(negatives)) {
      this._negative.set(site, Math.max(this._negative.get(site) ?? 0, val))
    }
    this._notifyChange()
  }

  /** Export state for full-state sync. */
  exportState(): { positive: Record<string, number>; negative: Record<string, number> } {
    return {
      positive: Object.fromEntries(this._positive),
      negative: Object.fromEntries(this._negative),
    }
  }

  drainOps(): Operation[] {
    return this._pendingOps.splice(0, this._pendingOps.length)
  }

  onChange(callback: CounterChangeCallback): () => void {
    this._listeners.add(callback)
    return () => {
      this._listeners.delete(callback)
    }
  }

  private _notifyChange(): void {
    if (this._listeners.size === 0) return
    const val = this.value
    for (const cb of this._listeners) {
      try {
        cb(val)
      } catch {
        // listener errors must not break notification
      }
    }
  }
}
