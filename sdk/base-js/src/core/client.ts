/**
 * BaseClient -- main entry point for @hanzoai/base.
 *
 * Two API surfaces:
 *
 * 1. Base-compatible: client.collection('posts').getList(...)
 * 2. Direct (convenience):  client.list('posts', { filter: '...' })
 *
 * Both share the same QueryStore, RealtimeService, and AuthStore.
 */

import type { BaseRecord } from './state.js'
import { VersionTracker } from './state.js'
import { QueryStore } from './store.js'
import { RealtimeService, type RealtimeEvent } from './realtime.js'
import { CollectionService, type FileOptions } from './collection.js'

// ---------------------------------------------------------------------------
// AuthStore
// ---------------------------------------------------------------------------

export interface AuthStore {
  token: string
  record: BaseRecord | null
  onChange(callback: (token: string, record: BaseRecord | null) => void): () => void
  save(token: string, record: BaseRecord | null): void
  clear(): void
  readonly isValid: boolean
}

export type AuthChangeCallback = (token: string, record: BaseRecord | null) => void

/**
 * Default in-memory auth store.
 * Validates JWT exp claim without external dependencies.
 */
export class MemoryAuthStore implements AuthStore {
  private _token = ''
  private _record: BaseRecord | null = null
  private _listeners = new Set<AuthChangeCallback>()

  get token(): string {
    return this._token
  }

  get record(): BaseRecord | null {
    return this._record
  }

  get isValid(): boolean {
    if (!this._token) return false
    try {
      const parts = this._token.split('.')
      if (parts.length !== 3) return false
      const payload = JSON.parse(atob(parts[1].replace(/-/g, '+').replace(/_/g, '/')))
      if (typeof payload.exp === 'number') {
        return payload.exp > Date.now() / 1000
      }
      return true
    } catch {
      return false
    }
  }

  save(token: string, record: BaseRecord | null): void {
    this._token = token
    this._record = record
    this._notify()
  }

  clear(): void {
    this._token = ''
    this._record = null
    this._notify()
  }

  onChange(callback: AuthChangeCallback): () => void {
    this._listeners.add(callback)
    return () => {
      this._listeners.delete(callback)
    }
  }

  private _notify(): void {
    for (const cb of this._listeners) {
      try {
        cb(this._token, this._record)
      } catch {
        // listener errors must not break notification
      }
    }
  }
}

// ---------------------------------------------------------------------------
// FileService
// ---------------------------------------------------------------------------

export class FileService {
  private readonly _baseUrl: string

  constructor(baseUrl: string) {
    this._baseUrl = baseUrl.replace(/\/$/, '')
  }

  /**
   * Build a full URL to a record file.
   * Compatible with Base's files.getURL().
   */
  getURL(record: BaseRecord, filename: string, options?: FileOptions): string {
    if (!filename || !record.id) return ''

    const collectionId = (record.collectionId ?? record.collectionName ?? '') as string
    const parts = [
      this._baseUrl,
      'api',
      'files',
      encodeURIComponent(collectionId),
      encodeURIComponent(record.id),
      encodeURIComponent(filename),
    ]

    let url = parts.join('/')

    const params = new URLSearchParams()
    if (options?.thumb) params.set('thumb', options.thumb)
    if (options?.token) params.set('token', options.token)
    const qs = params.toString()
    if (qs) url += '?' + qs

    return url
  }
}

// ---------------------------------------------------------------------------
// ClientConfig
// ---------------------------------------------------------------------------

export interface ClientConfig {
  /** Base URL of the Hanzo Base instance (e.g. "https://myapp.hanzo.ai"). */
  url: string
  /** Optional external auth store. Defaults to in-memory store. */
  authStore?: AuthStore
}

export interface ListOptions {
  filter?: string
  sort?: string
  expand?: string
  fields?: string
  page?: number
  perPage?: number
}

export interface ListResult<T = BaseRecord> {
  page: number
  perPage: number
  totalItems: number
  totalPages: number
  items: T[]
}

// ---------------------------------------------------------------------------
// BaseClient
// ---------------------------------------------------------------------------

export class BaseClient {
  readonly url: string
  readonly authStore: AuthStore
  readonly store: QueryStore
  readonly realtime: RealtimeService
  readonly files: FileService

  private readonly _versionTracker: VersionTracker
  private readonly _collections = new Map<string, CollectionService>()

  /**
   * Create a BaseClient.
   *
   * Accepts either a config object or a plain URL string for convenience:
   *   new BaseClient('https://myapp.hanzo.ai')
   *   new BaseClient({ url: 'https://myapp.hanzo.ai' })
   */
  constructor(configOrUrl: ClientConfig | string) {
    const config: ClientConfig =
      typeof configOrUrl === 'string' ? { url: configOrUrl } : configOrUrl

    this.url = config.url.replace(/\/$/, '')
    this.authStore = config.authStore ?? new MemoryAuthStore()
    this.store = new QueryStore()
    this.realtime = new RealtimeService(this.url, () => this.authStore.token)
    this.files = new FileService(this.url)
    this._versionTracker = new VersionTracker()

    // Sync identity hash when auth changes.
    this.authStore.onChange((token) => {
      this._versionTracker.setIdentity(
        token ? VersionTracker.hashIdentity(token) : 0,
      )
    })
  }

  // ---- Base-compatible collection() API ------------------------------------

  /** Get or create a CollectionService for the given name/id. */
  collection(nameOrId: string): CollectionService {
    let svc = this._collections.get(nameOrId)
    if (!svc) {
      svc = new CollectionService(
        nameOrId,
        this.url,
        () => this.authStore.token,
        (token, record) => this.authStore.save(token, record),
        this.store,
        this.realtime,
      )
      this._collections.set(nameOrId, svc)
    }
    return svc
  }

  // ---- State version ------------------------------------------------------

  /** Current state version from the QueryStore's internal tracker. */
  get version() {
    return this.store.version
  }

  // ---- Direct convenience API (kept for backwards compatibility) ----------

  private _headers(): Record<string, string> {
    const h: Record<string, string> = { 'Content-Type': 'application/json' }
    if (this.authStore.token) {
      h['Authorization'] = this.authStore.token
    }
    return h
  }

  private async _request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await fetch(`${this.url}${path}`, {
      method,
      headers: this._headers(),
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })

    if (!res.ok) {
      const text = await res.text()
      let detail: unknown
      try {
        detail = JSON.parse(text)
      } catch {
        detail = text
      }
      throw new BaseClientError(res.status, detail)
    }

    if (res.status === 204) return undefined as T

    return res.json() as Promise<T>
  }

  async list(collection: string, options?: ListOptions): Promise<ListResult> {
    const params = new URLSearchParams()
    if (options?.filter) params.set('filter', options.filter)
    if (options?.sort) params.set('sort', options.sort)
    if (options?.expand) params.set('expand', options.expand)
    if (options?.fields) params.set('fields', options.fields)
    if (options?.page) params.set('page', String(options.page))
    if (options?.perPage) params.set('perPage', String(options.perPage))

    const qs = params.toString()
    const path = `/api/collections/${encodeURIComponent(collection)}/records${qs ? '?' + qs : ''}`
    const result = await this._request<ListResult>('GET', path)

    this.store.setQuery(collection, options?.filter ?? '', result.items)
    return result
  }

  async getOne(collection: string, id: string, options?: Pick<ListOptions, 'expand' | 'fields'>): Promise<BaseRecord> {
    const params = new URLSearchParams()
    if (options?.expand) params.set('expand', options.expand)
    if (options?.fields) params.set('fields', options.fields)
    const qs = params.toString()
    const path = `/api/collections/${encodeURIComponent(collection)}/records/${encodeURIComponent(id)}${qs ? '?' + qs : ''}`
    return this._request<BaseRecord>('GET', path)
  }

  async create(collection: string, data: Record<string, unknown>): Promise<BaseRecord> {
    const path = `/api/collections/${encodeURIComponent(collection)}/records`
    const record = await this._request<BaseRecord>('POST', path, data)
    this.store.applyServerUpdate(collection, 'create', record)
    return record
  }

  async update(collection: string, id: string, data: Record<string, unknown>): Promise<BaseRecord> {
    const path = `/api/collections/${encodeURIComponent(collection)}/records/${encodeURIComponent(id)}`
    const record = await this._request<BaseRecord>('PATCH', path, data)
    this.store.applyServerUpdate(collection, 'update', record)
    return record
  }

  async delete(collection: string, id: string): Promise<void> {
    const path = `/api/collections/${encodeURIComponent(collection)}/records/${encodeURIComponent(id)}`
    await this._request<void>('DELETE', path)
    this.store.applyServerUpdate(collection, 'delete', { id } as BaseRecord)
  }

  // ---- Auth (direct convenience) ------------------------------------------

  async signInWithPassword(
    collection: string,
    identity: string,
    password: string,
  ): Promise<{ token: string; record: BaseRecord }> {
    const path = `/api/collections/${encodeURIComponent(collection)}/auth-with-password`
    const result = await this._request<{ token: string; record: BaseRecord }>('POST', path, {
      identity,
      password,
    })
    this.authStore.save(result.token, result.record)
    return result
  }

  async signUp(
    collection: string,
    data: Record<string, unknown>,
  ): Promise<BaseRecord> {
    return this.create(collection, data)
  }

  async refreshAuth(collection: string): Promise<{ token: string; record: BaseRecord }> {
    const path = `/api/collections/${encodeURIComponent(collection)}/auth-refresh`
    const result = await this._request<{ token: string; record: BaseRecord }>('POST', path)
    this.authStore.save(result.token, result.record)
    return result
  }

  signOut(): void {
    this.authStore.clear()
  }

  // ---- Raw request --------------------------------------------------------

  /**
   * Send a raw request to the Base API.
   * Convenience for endpoints not covered by CollectionService.
   */
  async send<T = unknown>(
    path: string,
    options: {
      method?: string
      headers?: Record<string, string>
      body?: string | FormData
      query?: Record<string, string>
      signal?: AbortSignal
    } = {},
  ): Promise<T> {
    const method = options.method ?? 'GET'
    let url = `${this.url}${path}`

    if (options.query) {
      const params = new URLSearchParams(options.query)
      url += '?' + params.toString()
    }

    const headers: Record<string, string> = { ...options.headers }
    if (this.authStore.token) {
      headers['Authorization'] = this.authStore.token
    }

    const response = await fetch(url, {
      method,
      headers,
      body: options.body,
      signal: options.signal,
    })

    if (!response.ok) {
      const data = await response.json().catch(() => ({}))
      throw new BaseClientError(
        response.status,
        data,
      )
    }

    if (response.status === 204) return undefined as T

    return response.json() as Promise<T>
  }

  // ---- Health check -------------------------------------------------------

  async health(): Promise<{ code: number; message: string }> {
    return this.send('/api/health')
  }

  // ---- Realtime convenience -----------------------------------------------

  /**
   * Subscribe to realtime events for a collection topic.
   * Also wires events into the QueryStore automatically.
   */
  subscribeAndSync(collection: string, topic = '*', callback?: (e: RealtimeEvent) => void): () => void {
    return this.realtime.subscribe(collection, topic, (event) => {
      this.store.applyServerUpdate(collection, event.action, event.record)
      callback?.(event)
    })
  }

  // ---- Cleanup ------------------------------------------------------------

  /** Disconnect realtime and clear caches. */
  disconnect(): void {
    this.realtime.disconnect()
  }
}

// ---------------------------------------------------------------------------
// Error
// ---------------------------------------------------------------------------

export class BaseClientError extends Error {
  readonly status: number
  readonly detail: unknown

  constructor(status: number, detail: unknown) {
    super(`BaseClient error ${status}`)
    this.name = 'BaseClientError'
    this.status = status
    this.detail = detail
  }
}
