/**
 * CollectionService -- typed CRUD + auth + realtime for a single collection.
 *
 * API-compatible with PocketBase JS SDK's RecordService, extended with
 * reactive features (subscribe/unsubscribe, optimistic writes).
 */

import type { BaseRecord } from './state.js'
import type { QueryStore } from './store.js'
import type { RealtimeService, RealtimeCallback } from './realtime.js'

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface ListResult<T = BaseRecord> {
  page: number
  perPage: number
  totalItems: number
  totalPages: number
  items: T[]
}

export interface RecordQueryOptions {
  filter?: string
  sort?: string
  expand?: string
  fields?: string
  headers?: Record<string, string>
  /** Extra query params merged into the URL. */
  query?: Record<string, string>
  /** Request-scoped AbortSignal. */
  signal?: AbortSignal
}

export interface RecordFullListOptions extends RecordQueryOptions {
  /** Batch size for pagination (default 200). */
  batch?: number
}

export interface FileOptions {
  thumb?: string
  token?: string
}

export interface AuthResponse<T = BaseRecord> {
  token: string
  record: T
}

export interface OAuth2Options {
  provider: string
  code: string
  codeVerifier: string
  redirectUrl: string
  createData?: Record<string, unknown>
}

// ---------------------------------------------------------------------------
// CollectionService
// ---------------------------------------------------------------------------

export class CollectionService {
  readonly collectionIdOrName: string

  private readonly _baseUrl: string
  private readonly _getToken: () => string
  private readonly _setAuth: (token: string, record: BaseRecord) => void
  private readonly _store: QueryStore
  private readonly _realtime: RealtimeService

  constructor(
    collectionIdOrName: string,
    baseUrl: string,
    getToken: () => string,
    setAuth: (token: string, record: BaseRecord) => void,
    store: QueryStore,
    realtime: RealtimeService,
  ) {
    this.collectionIdOrName = collectionIdOrName
    this._baseUrl = baseUrl.replace(/\/$/, '')
    this._getToken = getToken
    this._setAuth = setAuth
    this._store = store
    this._realtime = realtime
  }

  // ---- CRUD ---------------------------------------------------------------

  async getList<T extends BaseRecord = BaseRecord>(
    page = 1,
    perPage = 30,
    options?: RecordQueryOptions,
  ): Promise<ListResult<T>> {
    const params = new URLSearchParams()
    params.set('page', String(page))
    params.set('perPage', String(perPage))
    this._applyOptions(params, options)

    const result = await this._request<ListResult<T>>(
      'GET',
      `${this._collectionPath()}/records?${params}`,
      undefined,
      options,
    )

    // Cache in store.
    const cacheFilter = options?.filter ?? ''
    this._store.setQuery(
      this.collectionIdOrName,
      cacheFilter,
      result.items as unknown as BaseRecord[],
    )

    return result
  }

  async getFullList<T extends BaseRecord = BaseRecord>(
    options?: RecordFullListOptions,
  ): Promise<T[]> {
    const batch = options?.batch ?? 200
    let page = 1
    let all: T[] = []

    // eslint-disable-next-line no-constant-condition
    while (true) {
      const result = await this.getList<T>(page, batch, options)
      all = all.concat(result.items)
      if (all.length >= result.totalItems || result.items.length < batch) {
        break
      }
      page++
    }

    return all
  }

  async getOne<T extends BaseRecord = BaseRecord>(
    id: string,
    options?: RecordQueryOptions,
  ): Promise<T> {
    const params = new URLSearchParams()
    this._applyOptions(params, options)
    const qs = params.toString()
    const path = `${this._collectionPath()}/records/${encodeURIComponent(id)}${qs ? '?' + qs : ''}`
    return this._request<T>('GET', path, undefined, options)
  }

  async getFirstListItem<T extends BaseRecord = BaseRecord>(
    filter: string,
    options?: RecordQueryOptions,
  ): Promise<T> {
    const opts = { ...options, filter }
    const result = await this.getList<T>(1, 1, opts)
    if (result.items.length === 0) {
      throw new ClientResponseError({
        url: this._baseUrl,
        status: 404,
        data: { message: 'The requested resource wasn\'t found.' },
      })
    }
    return result.items[0]
  }

  async create<T extends BaseRecord = BaseRecord>(
    data: Record<string, unknown> | FormData,
    options?: RecordQueryOptions,
  ): Promise<T> {
    const params = new URLSearchParams()
    this._applyOptions(params, options)
    const qs = params.toString()
    const path = `${this._collectionPath()}/records${qs ? '?' + qs : ''}`

    // Optimistic: generate temp id.
    let mutationId: string | undefined
    if (!(data instanceof FormData) && typeof data === 'object') {
      const tempId = `__temp_${Date.now()}_${Math.random().toString(36).slice(2, 6)}`
      const optimistic: BaseRecord = {
        id: tempId,
        collectionName: this.collectionIdOrName,
        ...(data as Record<string, unknown>),
      }
      mutationId = this._store.optimisticSet(this.collectionIdOrName, optimistic)
    }

    try {
      const body = data instanceof FormData ? data : JSON.stringify(data)
      const contentType = data instanceof FormData ? undefined : 'application/json'
      const record = await this._request<T>('POST', path, body, options, contentType)

      // Replace optimistic entry with real server record.
      if (mutationId) {
        this._store.rollbackOptimistic(mutationId)
      }
      this._store.applyServerUpdate(this.collectionIdOrName, 'create', record as unknown as BaseRecord)
      return record
    } catch (err) {
      if (mutationId) {
        this._store.rollbackOptimistic(mutationId)
      }
      throw err
    }
  }

  async update<T extends BaseRecord = BaseRecord>(
    id: string,
    data: Record<string, unknown> | FormData,
    options?: RecordQueryOptions,
  ): Promise<T> {
    const params = new URLSearchParams()
    this._applyOptions(params, options)
    const qs = params.toString()
    const path = `${this._collectionPath()}/records/${encodeURIComponent(id)}${qs ? '?' + qs : ''}`

    // Optimistic update.
    let mutationId: string | undefined
    if (!(data instanceof FormData) && typeof data === 'object') {
      const optimistic: BaseRecord = {
        id,
        collectionName: this.collectionIdOrName,
        ...(data as Record<string, unknown>),
      }
      mutationId = this._store.optimisticSet(this.collectionIdOrName, optimistic)
    }

    try {
      const body = data instanceof FormData ? data : JSON.stringify(data)
      const contentType = data instanceof FormData ? undefined : 'application/json'
      const record = await this._request<T>('PATCH', path, body, options, contentType)

      if (mutationId) {
        this._store.rollbackOptimistic(mutationId)
      }
      this._store.applyServerUpdate(this.collectionIdOrName, 'update', record as unknown as BaseRecord)
      return record
    } catch (err) {
      if (mutationId) {
        this._store.rollbackOptimistic(mutationId)
      }
      throw err
    }
  }

  async delete(id: string, options?: RecordQueryOptions): Promise<boolean> {
    const params = new URLSearchParams()
    this._applyOptions(params, options)
    const qs = params.toString()
    const path = `${this._collectionPath()}/records/${encodeURIComponent(id)}${qs ? '?' + qs : ''}`

    // Optimistic delete.
    const mutationId = this._store.optimisticDelete(this.collectionIdOrName, id)

    try {
      await this._request<void>('DELETE', path, undefined, options)
      this._store.rollbackOptimistic(mutationId)
      this._store.applyServerUpdate(this.collectionIdOrName, 'delete', { id } as BaseRecord)
      return true
    } catch (err) {
      this._store.rollbackOptimistic(mutationId)
      throw err
    }
  }

  // ---- Realtime -----------------------------------------------------------

  /**
   * Subscribe to realtime events for this collection.
   * `topic` is "*" for all changes or a specific record id.
   */
  subscribe(topic: string, callback: RealtimeCallback): () => void {
    return this._realtime.subscribe(this.collectionIdOrName, topic, callback)
  }

  /** Unsubscribe from a specific topic or all topics for this collection. */
  unsubscribe(_topic?: string): void {
    // The primary unsubscribe mechanism is the function returned by subscribe().
    // This method exists for API compat; callers should prefer the returned fn.
  }

  // ---- Auth methods (for auth collections) --------------------------------

  async authWithPassword<T extends BaseRecord = BaseRecord>(
    identity: string,
    password: string,
    options?: RecordQueryOptions,
  ): Promise<AuthResponse<T>> {
    const params = new URLSearchParams()
    this._applyOptions(params, options)
    const qs = params.toString()
    const path = `${this._collectionPath()}/auth-with-password${qs ? '?' + qs : ''}`

    const result = await this._request<AuthResponse<T>>(
      'POST',
      path,
      JSON.stringify({ identity, password }),
      options,
      'application/json',
    )

    this._setAuth(result.token, result.record as unknown as BaseRecord)
    return result
  }

  async authWithOAuth2<T extends BaseRecord = BaseRecord>(
    oauthOptions: OAuth2Options,
    options?: RecordQueryOptions,
  ): Promise<AuthResponse<T>> {
    const params = new URLSearchParams()
    this._applyOptions(params, options)
    const qs = params.toString()
    const path = `${this._collectionPath()}/auth-with-oauth2${qs ? '?' + qs : ''}`

    const result = await this._request<AuthResponse<T>>(
      'POST',
      path,
      JSON.stringify(oauthOptions),
      options,
      'application/json',
    )

    this._setAuth(result.token, result.record as unknown as BaseRecord)
    return result
  }

  async requestVerification(email: string, options?: RecordQueryOptions): Promise<boolean> {
    const path = `${this._collectionPath()}/request-verification`
    await this._request<void>(
      'POST',
      path,
      JSON.stringify({ email }),
      options,
      'application/json',
    )
    return true
  }

  async confirmVerification(token: string, options?: RecordQueryOptions): Promise<boolean> {
    const path = `${this._collectionPath()}/confirm-verification`
    await this._request<void>(
      'POST',
      path,
      JSON.stringify({ token }),
      options,
      'application/json',
    )
    return true
  }

  async requestPasswordReset(email: string, options?: RecordQueryOptions): Promise<boolean> {
    const path = `${this._collectionPath()}/request-password-reset`
    await this._request<void>(
      'POST',
      path,
      JSON.stringify({ email }),
      options,
      'application/json',
    )
    return true
  }

  async confirmPasswordReset(
    token: string,
    password: string,
    passwordConfirm: string,
    options?: RecordQueryOptions,
  ): Promise<boolean> {
    const path = `${this._collectionPath()}/confirm-password-reset`
    await this._request<void>(
      'POST',
      path,
      JSON.stringify({ token, password, passwordConfirm }),
      options,
      'application/json',
    )
    return true
  }

  // ---- Internal -----------------------------------------------------------

  private _collectionPath(): string {
    return `/api/collections/${encodeURIComponent(this.collectionIdOrName)}`
  }

  private _applyOptions(params: URLSearchParams, options?: RecordQueryOptions): void {
    if (!options) return
    if (options.filter) params.set('filter', options.filter)
    if (options.sort) params.set('sort', options.sort)
    if (options.expand) params.set('expand', options.expand)
    if (options.fields) params.set('fields', options.fields)
    if (options.query) {
      for (const [k, v] of Object.entries(options.query)) {
        params.set(k, v)
      }
    }
  }

  private async _request<T>(
    method: string,
    path: string,
    body?: string | FormData,
    options?: RecordQueryOptions,
    contentType?: string,
  ): Promise<T> {
    const url = `${this._baseUrl}${path}`
    const token = this._getToken()

    const headers: Record<string, string> = {
      ...(options?.headers ?? {}),
    }
    if (token) {
      headers['Authorization'] = token
    }
    if (contentType) {
      headers['Content-Type'] = contentType
    }

    const response = await fetch(url, {
      method,
      headers,
      body: body ?? undefined,
      signal: options?.signal,
    })

    if (!response.ok) {
      const data = await response.json().catch(() => ({}))
      throw new ClientResponseError({
        url,
        status: response.status,
        data,
      })
    }

    // DELETE returns 204 with no body.
    if (response.status === 204) {
      return undefined as T
    }

    return response.json() as Promise<T>
  }
}

// ---------------------------------------------------------------------------
// ClientResponseError
// ---------------------------------------------------------------------------

export interface ClientResponseErrorData {
  url: string
  status: number
  data: Record<string, unknown>
}

export class ClientResponseError extends Error {
  url: string
  status: number
  data: Record<string, unknown>
  isAbort: boolean

  constructor(errorData: ClientResponseErrorData) {
    const message =
      (errorData.data?.message as string) ??
      `ClientResponseError ${errorData.status}`
    super(message)
    this.name = 'ClientResponseError'
    this.url = errorData.url
    this.status = errorData.status
    this.data = errorData.data
    this.isAbort = errorData.status === 0
  }

  toJSON(): ClientResponseErrorData {
    return { url: this.url, status: this.status, data: this.data }
  }
}
