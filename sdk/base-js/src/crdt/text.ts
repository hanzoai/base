/**
 * CRDTText -- collaborative text based on RGA (Replicated Growable Array).
 *
 * Each character has a unique position id. Insertions reference the position
 * id of the character they follow. Deletions tombstone position ids.
 * The structure resolves conflicts by using HLC comparison on concurrent
 * inserts at the same position.
 */

import type { HLCTimestamp } from './clock.js'
import { HybridLogicalClock, compareHLC } from './clock.js'
import type { Operation, TextInsertPayload, TextDeletePayload } from './operations.js'
import { makeOpId, makePositionId } from './operations.js'

// ---------------------------------------------------------------------------
// Internal node structure
// ---------------------------------------------------------------------------

interface TextNode {
  id: string         // unique position id
  char: string       // single character
  hlc: HLCTimestamp  // creation timestamp
  tombstone: boolean // soft-deleted
  afterId: string | null // the id this node was inserted after
}

// ---------------------------------------------------------------------------
// CRDTText
// ---------------------------------------------------------------------------

export type TextChangeCallback = (text: string) => void

export class CRDTText {
  private _nodes: TextNode[] = []
  private readonly _clock: HybridLogicalClock
  private readonly _documentId: string
  private readonly _field: string
  private readonly _pendingOps: Operation[] = []
  private _listeners = new Set<TextChangeCallback>()

  /** Index from position id to array index for O(1) lookup. */
  private _index = new Map<string, number>()

  constructor(documentId: string, field: string, clock: HybridLogicalClock) {
    this._documentId = documentId
    this._field = field
    this._clock = clock
  }

  // ---- Public API ---------------------------------------------------------

  /** Insert text at a visible character position (0-based). */
  insert(position: number, text: string): Operation {
    if (text.length === 0) {
      throw new Error('CRDTText.insert: empty text')
    }

    const afterId = this._visibleIdAtPosition(position - 1)
    const hlc = this._clock.now()
    const positionIds: string[] = []

    // Create nodes for each character, chaining them.
    let prevId = afterId
    for (let i = 0; i < text.length; i++) {
      const posId = makePositionId(hlc, i)
      positionIds.push(posId)

      const node: TextNode = {
        id: posId,
        char: text[i],
        hlc: { ...hlc, counter: hlc.counter + i },
        tombstone: false,
        afterId: prevId,
      }

      this._insertNode(node)
      prevId = posId
    }

    const op: Operation = {
      id: makeOpId(hlc),
      documentId: this._documentId,
      field: this._field,
      hlc,
      type: 'text.insert',
      payload: { afterId, content: text, positionIds } satisfies TextInsertPayload,
    }

    this._pendingOps.push(op)
    this._notifyChange()
    return op
  }

  /** Delete `length` visible characters starting at `position` (0-based). */
  delete(position: number, length: number): Operation {
    const ids = this._visibleIdsInRange(position, length)
    if (ids.length === 0) {
      throw new Error('CRDTText.delete: nothing to delete')
    }

    for (const id of ids) {
      const idx = this._index.get(id)
      if (idx !== undefined) {
        this._nodes[idx].tombstone = true
      }
    }

    const hlc = this._clock.now()
    const op: Operation = {
      id: makeOpId(hlc),
      documentId: this._documentId,
      field: this._field,
      hlc,
      type: 'text.delete',
      payload: { positionIds: ids } satisfies TextDeletePayload,
    }

    this._pendingOps.push(op)
    this._notifyChange()
    return op
  }

  /** Apply a remote operation. */
  applyRemote(op: Operation): void {
    this._clock.receive(op.hlc)

    if (op.type === 'text.insert') {
      const payload = op.payload as TextInsertPayload
      for (let i = 0; i < payload.content.length; i++) {
        const node: TextNode = {
          id: payload.positionIds[i],
          char: payload.content[i],
          hlc: { ...op.hlc, counter: op.hlc.counter + i },
          tombstone: false,
          afterId: i === 0 ? payload.afterId : payload.positionIds[i - 1],
        }
        this._insertNode(node)
      }
    } else if (op.type === 'text.delete') {
      const payload = op.payload as TextDeletePayload
      for (const posId of payload.positionIds) {
        const idx = this._index.get(posId)
        if (idx !== undefined) {
          this._nodes[idx].tombstone = true
        }
      }
    }

    this._notifyChange()
  }

  /** Get the current visible text. */
  toString(): string {
    let result = ''
    for (const node of this._nodes) {
      if (!node.tombstone) {
        result += node.char
      }
    }
    return result
  }

  /** Number of visible characters. */
  get length(): number {
    let count = 0
    for (const node of this._nodes) {
      if (!node.tombstone) count++
    }
    return count
  }

  /** Drain pending local operations. */
  drainOps(): Operation[] {
    return this._pendingOps.splice(0, this._pendingOps.length)
  }

  onChange(callback: TextChangeCallback): () => void {
    this._listeners.add(callback)
    return () => {
      this._listeners.delete(callback)
    }
  }

  // ---- Internal -----------------------------------------------------------

  /**
   * Insert a node into the correct position in the array.
   * RGA rule: among siblings sharing the same afterId, order by HLC descending
   * (newer inserts appear first, pushing older ones right).
   */
  private _insertNode(node: TextNode): void {
    // Find the position of the parent (afterId).
    let parentIdx: number
    if (node.afterId === null) {
      parentIdx = -1 // insert at beginning
    } else {
      const idx = this._index.get(node.afterId)
      if (idx === undefined) {
        // Parent not found -- append at end (out-of-order delivery).
        parentIdx = this._nodes.length - 1
      } else {
        parentIdx = idx
      }
    }

    // Scan right from parentIdx+1 to find insertion point.
    // Skip nodes that were also inserted after the same parent but have a
    // higher HLC (they should appear before this node).
    let insertIdx = parentIdx + 1
    while (insertIdx < this._nodes.length) {
      const existing = this._nodes[insertIdx]
      // Only compare with siblings of the same parent.
      if (existing.afterId !== node.afterId) break
      // Higher HLC goes first (left).
      if (compareHLC(existing.hlc, node.hlc) <= 0) break
      insertIdx++
    }

    // Insert and rebuild index for shifted elements.
    this._nodes.splice(insertIdx, 0, node)
    this._rebuildIndex(insertIdx)
  }

  /** Rebuild the id->index map from `startIdx` onward. */
  private _rebuildIndex(startIdx: number): void {
    for (let i = startIdx; i < this._nodes.length; i++) {
      this._index.set(this._nodes[i].id, i)
    }
  }

  /** Get the position id of the visible character at position `pos`, or null for before-head. */
  private _visibleIdAtPosition(pos: number): string | null {
    if (pos < 0) return null
    let visible = -1
    for (const node of this._nodes) {
      if (!node.tombstone) {
        visible++
        if (visible === pos) return node.id
      }
    }
    // pos is past end -- return last visible id.
    for (let i = this._nodes.length - 1; i >= 0; i--) {
      if (!this._nodes[i].tombstone) return this._nodes[i].id
    }
    return null
  }

  /** Get position ids for `length` visible characters starting at `position`. */
  private _visibleIdsInRange(position: number, length: number): string[] {
    const ids: string[] = []
    let visible = -1
    for (const node of this._nodes) {
      if (node.tombstone) continue
      visible++
      if (visible >= position && visible < position + length) {
        ids.push(node.id)
      }
      if (ids.length === length) break
    }
    return ids
  }

  private _notifyChange(): void {
    if (this._listeners.size === 0) return
    const text = this.toString()
    for (const cb of this._listeners) {
      try {
        cb(text)
      } catch {
        // listener errors must not break notification
      }
    }
  }
}
