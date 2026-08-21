// Typed fetch wrappers for the Base API. Replaces the Hanzo Base SDK.
import { iam, identity } from '~/lib/iam'

const TOKEN_KEY = 'base_auth_token'
const RECORD_KEY = 'base_auth_record'

// ---------------------------------------------------------------------------
// Auth store — localStorage-backed, observable
// ---------------------------------------------------------------------------

type Listener = () => void

const listeners = new Set<Listener>()

function notify() {
  for (const fn of listeners) fn()
}

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ''
}

export function getRecord(): Record<string, unknown> | null {
  const raw = localStorage.getItem(RECORD_KEY)
  if (!raw) return null
  try { return JSON.parse(raw) } catch { return null }
}

export function setAuth(token: string, record: Record<string, unknown>) {
  localStorage.setItem(TOKEN_KEY, token)
  localStorage.setItem(RECORD_KEY, JSON.stringify(record))
  notify()
}

export function clearAuth() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(RECORD_KEY)
  // The issuer's tokens go with it. A refresh token left behind is a session:
  // the next request would renew off it and sign the user straight back in.
  iam.clearTokens()
  notify()
}

export function onAuthChange(fn: Listener): () => void {
  listeners.add(fn)
  return () => { listeners.delete(fn) }
}

// ---------------------------------------------------------------------------
// Session — one question, and the renewal that answers it honestly
// ---------------------------------------------------------------------------

// How close to expiry a bearer stops being worth sending. A request that leaves
// now should still be accepted when it lands.
const MARGIN_MS = 30_000

// When the bearer stops being accepted, in ms, or null when it does not say.
// The signature is the server's to check; this reads only the instant.
function expiry(token: string): number | null {
  try {
    const { exp } = JSON.parse(atob(token.split('.')[1].replace(/-/g, '+').replace(/_/g, '/')))
    return typeof exp === 'number' ? exp * 1000 : null
  } catch {
    return null
  }
}

// Is the stored bearer one the server will still take? A token that states no
// expiry cannot be judged here, so the server judges it.
export function live(): boolean {
  const token = getToken()
  if (!token) return false
  const exp = expiry(token)
  return exp === null || exp - MARGIN_MS > Date.now()
}

let renewing: Promise<void> | null = null

// Renewal is serialized across the whole origin, not merely within this tab.
// IAM rotates the refresh token on every use and revokes the entire family the
// first time it sees one twice, with no grace window — so two tabs renewing at
// once do not race for a token, they sign each other out. A Web Lock is held
// per origin; the re-check inside it is what makes the loser a no-op, because
// by then the winner has written the fresh session to the storage both share.
//
// A renewal that fails leaves the bearer alone. The server refuses it, and the
// page that asked offers a way back — losing the session here would take the
// user's unsaved work with it.
function renew(): Promise<void> {
  renewing ??= navigator.locks
    .request('base:renew', async () => {
      if (live()) return
      const token = await iam.refreshAccessToken()
      setAuth(token.accessToken, identity(token.accessToken))
    })
    .catch(() => { /* the bearer stands as it is */ })
    .finally(() => { renewing = null })
  return renewing
}

// Is there a session to act with? Guards and requests ask this one question, so
// they cannot disagree about the answer. A bearer past its margin is renewed
// before answering; no bearer at all is signed out rather than expired, and
// there is nothing to renew.
export async function session(): Promise<boolean> {
  if (live()) return true
  if (!getToken()) return false
  await renew()
  return live()
}

// ---------------------------------------------------------------------------
// Fetch helpers
// ---------------------------------------------------------------------------

class ApiError extends Error {
  status: number
  data: unknown
  constructor(status: number, message: string, data?: unknown) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    this.data = data
  }
}

export async function request<T>(path: string, init?: RequestInit): Promise<T> {
  // Renew before sending rather than after being refused: the bearer this call
  // carries should still be good when the server reads it.
  await session()
  const token = getToken()
  const headers: Record<string, string> = {
    ...(init?.headers as Record<string, string> ?? {}),
  }
  if (token) headers['Authorization'] = token
  if (init?.body && !(init.body instanceof FormData)) {
    headers['Content-Type'] = 'application/json'
  }

  const res = await fetch(path, { ...init, headers })
  if (!res.ok) {
    let data: unknown
    try { data = await res.json() } catch { /* empty */ }
    const msg = (data as Record<string, string>)?.message ?? res.statusText
    throw new ApiError(res.status, msg, data)
  }
  if (res.status === 204) return undefined as T
  return res.json() as Promise<T>
}

function qs(params: Record<string, string | number | boolean | undefined>): string {
  const parts: string[] = []
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') parts.push(`${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
  }
  return parts.length ? '?' + parts.join('&') : ''
}

// ---------------------------------------------------------------------------
// List response type
// ---------------------------------------------------------------------------

export interface ListResult<T> {
  page: number
  perPage: number
  totalItems: number
  totalPages: number
  items: T[]
}

// ---------------------------------------------------------------------------
// Collection types
// ---------------------------------------------------------------------------

export interface CollectionField {
  id: string
  name: string
  type: string
  system: boolean
  hidden: boolean
  presentable: boolean
  [key: string]: unknown
}

export interface CollectionModel {
  id: string
  name: string
  type: string
  system: boolean
  fields: CollectionField[]
  indexes: string[]
  listRule: string | null
  viewRule: string | null
  createRule: string | null
  updateRule: string | null
  deleteRule: string | null
  [key: string]: unknown
}

export interface RecordModel {
  id: string
  collectionId: string
  collectionName: string
  created: string
  updated: string
  [key: string]: unknown
}

export interface LogModel {
  id: string
  created: string
  // slog's numeric level, which is what `core.Log.Level` is and what the server
  // sends. Naming it a string here made every comparison against one silently
  // false.
  level: number
  message: string
  data: Record<string, unknown>
  [key: string]: unknown
}

export interface FunctionModel {
  // The name is the id: one row is one function, addressed at
  // /v1/functions/{name}. The collection's own pattern is what a name must
  // match, restated in FUNCTION_NAME below so a bad one is answered before the
  // round trip rather than after it.
  id: string
  // What the function runs. It is a hidden field, so it comes back for a
  // superuser and not for anyone else — a caller may learn that a function
  // exists without learning what it says.
  source: string
  createdAt: string
  updatedAt: string
  [key: string]: unknown
}

export interface BackupModel {
  key: string
  size: number
  modified: string
}

export interface CronModel {
  id: string
  expression: string
  [key: string]: unknown
}

// ---------------------------------------------------------------------------
// Collections
// ---------------------------------------------------------------------------

export async function listCollections(params?: { sort?: string; filter?: string; batch?: number }): Promise<CollectionModel[]> {
  // Base returns paginated; use perPage=500 to get all in one shot
  const q = qs({ sort: params?.sort, filter: params?.filter, perPage: params?.batch ?? 500 })
  const res = await request<ListResult<CollectionModel>>(`/v1/collections${q}`)
  return res.items
}

export async function getCollection(id: string): Promise<CollectionModel> {
  return request<CollectionModel>(`/v1/collections/${encodeURIComponent(id)}`)
}

export async function updateCollection(id: string, data: Record<string, unknown>): Promise<CollectionModel> {
  return request<CollectionModel>(`/v1/collections/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  })
}

export async function deleteCollection(id: string): Promise<void> {
  return request<void>(`/v1/collections/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export async function importCollections(collections: CollectionModel[]): Promise<void> {
  return request<void>('/v1/collections/import', {
    method: 'PUT',
    body: JSON.stringify({ collections }),
  })
}

// ---------------------------------------------------------------------------
// Records
// ---------------------------------------------------------------------------

export async function listRecords(
  collection: string,
  page: number,
  perPage: number,
  params?: { sort?: string; filter?: string },
): Promise<ListResult<RecordModel>> {
  const q = qs({ page, perPage, sort: params?.sort, filter: params?.filter })
  return request<ListResult<RecordModel>>(`/v1/collections/${encodeURIComponent(collection)}/records${q}`)
}

export async function getRecordById(collection: string, id: string): Promise<RecordModel> {
  return request<RecordModel>(`/v1/collections/${encodeURIComponent(collection)}/records/${encodeURIComponent(id)}`)
}

// Fetch every record across all pages (bounded loop, 500/page).
export async function getFullRecords(
  collection: string,
  params?: { sort?: string; filter?: string },
): Promise<RecordModel[]> {
  const out: RecordModel[] = []
  for (let page = 1; ; page++) {
    const res = await listRecords(collection, page, 500, params)
    out.push(...res.items)
    if (page >= res.totalPages || res.items.length === 0) break
  }
  return out
}

export async function createRecord(collection: string, data: FormData | Record<string, unknown>): Promise<RecordModel> {
  const body = data instanceof FormData ? data : JSON.stringify(data)
  return request<RecordModel>(`/v1/collections/${encodeURIComponent(collection)}/records`, {
    method: 'POST',
    body,
  })
}

export async function updateRecord(collection: string, id: string, data: FormData | Record<string, unknown>): Promise<RecordModel> {
  const body = data instanceof FormData ? data : JSON.stringify(data)
  return request<RecordModel>(`/v1/collections/${encodeURIComponent(collection)}/records/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body,
  })
}

export async function deleteRecord(collection: string, id: string): Promise<void> {
  return request<void>(`/v1/collections/${encodeURIComponent(collection)}/records/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

// ---------------------------------------------------------------------------
// Logs
// ---------------------------------------------------------------------------

export async function listLogs(page: number, perPage: number, params?: { sort?: string; filter?: string }): Promise<ListResult<LogModel>> {
  const q = qs({ page, perPage, sort: params?.sort, filter: params?.filter })
  return request<ListResult<LogModel>>(`/v1/logs${q}`)
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

export async function getSettings(): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>('/v1/settings')
}

export async function updateSettings(data: Record<string, unknown>): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>('/v1/settings', {
    method: 'PATCH',
    body: JSON.stringify(data),
  })
}

// `forms.TestEmailSend` takes an address and nothing else — it sends a plain
// message, because the templated flows it used to render were removed with
// local auth. What this proves is that SMTP works.
export async function testEmail(toEmail: string): Promise<void> {
  return request<void>('/v1/settings/test/email', {
    method: 'POST',
    body: JSON.stringify({ email: toEmail }),
  })
}

// ---------------------------------------------------------------------------
// Backups
// ---------------------------------------------------------------------------

export async function listBackups(): Promise<BackupModel[]> {
  return request<BackupModel[]>('/v1/backups')
}

export async function createBackup(name: string): Promise<void> {
  return request<void>('/v1/backups', {
    method: 'POST',
    body: JSON.stringify({ name }),
  })
}

export async function deleteBackup(key: string): Promise<void> {
  return request<void>(`/v1/backups/${encodeURIComponent(key)}`, { method: 'DELETE' })
}

export async function restoreBackup(key: string): Promise<void> {
  return request<void>(`/v1/backups/${encodeURIComponent(key)}/restore`, { method: 'POST' })
}

export function getBackupDownloadURL(key: string, token: string): string {
  return `/v1/backups/${encodeURIComponent(key)}?token=${encodeURIComponent(token)}`
}

export async function getFileToken(): Promise<string> {
  const res = await request<{ token: string }>('/v1/files/token', { method: 'POST' })
  return res.token
}

// ---------------------------------------------------------------------------
// Functions
//
// A function is a record in `_functions` and /v1/functions is the record path
// with a nicer address — the same handlers, so the same rules decide who may
// write one and who may call one. The five management calls below are that
// address; `invokeFunction` is the one verb that is genuinely different, and it
// is a POST to the function itself.
// ---------------------------------------------------------------------------

// What a function may be called, as the collection's id field spells it.
export const FUNCTION_NAME = /^[a-z0-9][a-z0-9_-]*$/

export async function listFunctions(): Promise<FunctionModel[]> {
  const res = await request<ListResult<FunctionModel>>(`/v1/functions${qs({ perPage: 200, sort: 'id' })}`)
  return res.items
}

export async function createFunction(id: string, source: string): Promise<FunctionModel> {
  return request<FunctionModel>('/v1/functions', {
    method: 'POST',
    body: JSON.stringify({ id, source }),
  })
}

// Only the source is sent. The name is the record's primary key, so renaming is
// writing a different function, and PATCHing one here would quietly leave the
// old name behind.
export async function updateFunction(id: string, source: string): Promise<FunctionModel> {
  return request<FunctionModel>(`/v1/functions/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify({ source }),
  })
}

export async function deleteFunction(id: string): Promise<void> {
  return request<void>(`/v1/functions/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

// Run one function and answer with whatever it answered. The result is the
// function's own JSON — any shape — so it is returned unread.
export async function invokeFunction(id: string, payload: string): Promise<unknown> {
  return request<unknown>(`/v1/functions/${encodeURIComponent(id)}`, {
    method: 'POST',
    body: payload,
  })
}

// ---------------------------------------------------------------------------
// Crons
// ---------------------------------------------------------------------------

export async function listCrons(): Promise<CronModel[]> {
  const res = await request<CronModel[]>('/v1/crons')
  return res
}

export async function runCron(jobId: string): Promise<void> {
  return request<void>(`/v1/crons/${encodeURIComponent(jobId)}`, { method: 'POST' })
}

// ---------------------------------------------------------------------------
// Superusers (convenience)
// ---------------------------------------------------------------------------

export async function listSuperusers(params?: { sort?: string }): Promise<RecordModel[]> {
  const q = qs({ sort: params?.sort, perPage: 200 })
  const res = await request<ListResult<RecordModel>>(`/v1/collections/_superusers/records${q}`)
  return res.items
}

// ---------------------------------------------------------------------------
// Realtime — Base SSE protocol at /v1/realtime.
//
// One shared EventSource per page, reference-counted per topic. On CONNECT the
// server hands back a clientId; we POST the active topic set to bind
// subscriptions. Events arrive as named SSE messages, the name being the topic.
//
// EventSource sends no headers, so the stream cannot carry the bearer every
// other call carries. It is opened with a grant instead: POST /v1/realtime/token
// mints one on an ordinary authenticated request and the stream spends it, once.
// Because it is spent, a dropped stream is reopened here with a fresh grant
// rather than by the browser's own retry, which would replay one that is gone.
// ---------------------------------------------------------------------------

export interface RealtimeEvent {
  action: 'create' | 'update' | 'delete'
  record: RecordModel
}
type RealtimeCallback = (e: RealtimeEvent) => void

let es: EventSource | null = null
let clientId = ''
let opening = false
let retry: ReturnType<typeof setTimeout> | undefined
let backoff = 1000
const topics = new Map<string, Set<RealtimeCallback>>()
const listening = new Set<string>()

function deliver(topic: string, ev: Event): void {
  const bucket = topics.get(topic)
  if (!bucket) return
  try {
    const evt = JSON.parse((ev as MessageEvent).data) as RealtimeEvent
    for (const fn of bucket) fn(evt)
  } catch { /* ignore malformed frame */ }
}

// One listener per topic per stream. The set is emptied with the stream, so a
// reopened one is listened to afresh and a live one is never doubled up.
function listen(source: EventSource, topic: string): void {
  if (listening.has(topic)) return
  listening.add(topic)
  source.addEventListener(topic, (ev) => deliver(topic, ev))
}

async function submitSubscriptions(): Promise<void> {
  if (!clientId) return
  await request<void>('/v1/realtime', {
    method: 'POST',
    body: JSON.stringify({ clientId, subscriptions: [...topics.keys()] }),
  }).catch(() => { /* transient; resent on next change */ })
}

function drop(): void {
  es?.close()
  es = null
  clientId = ''
  listening.clear()
}

function reopen(): void {
  if (retry !== undefined || topics.size === 0) return
  retry = setTimeout(() => { retry = undefined; void connect() }, backoff)
  backoff = Math.min(backoff * 2, 30_000)
}

async function connect(): Promise<void> {
  if (es || opening || topics.size === 0) return
  opening = true
  try {
    const { token } = await request<{ token: string }>('/v1/realtime/token', { method: 'POST' })
    if (topics.size === 0) return
    const source = new EventSource(`/v1/realtime?token=${encodeURIComponent(token)}`)
    source.addEventListener('CONNECT', (ev) => {
      try {
        clientId = JSON.parse((ev as MessageEvent).data).clientId as string
        backoff = 1000
        void submitSubscriptions()
      } catch { /* malformed handshake */ }
    })
    source.onerror = () => { drop(); reopen() }
    for (const topic of topics.keys()) listen(source, topic)
    es = source
  } catch {
    reopen()
  } finally {
    opening = false
  }
}

export function subscribeRecords(topic: string, cb: RealtimeCallback): () => void {
  let subs = topics.get(topic)
  if (!subs) {
    subs = new Set()
    topics.set(topic, subs)
    if (es) {
      listen(es, topic)
      void submitSubscriptions()
    }
  }
  subs.add(cb)
  void connect()

  return () => {
    const bucket = topics.get(topic)
    if (!bucket) return
    bucket.delete(cb)
    if (bucket.size === 0) {
      topics.delete(topic)
      void submitSubscriptions()
      if (topics.size === 0) drop()
    }
  }
}
