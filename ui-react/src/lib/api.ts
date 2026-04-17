// Typed fetch wrappers for the Base API. Replaces the PocketBase JS SDK.

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
  notify()
}

export function onAuthChange(fn: Listener): () => void {
  listeners.add(fn)
  return () => { listeners.delete(fn) }
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

async function request<T>(path: string, init?: RequestInit): Promise<T> {
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
  level: string
  message: string
  data: Record<string, unknown>
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
// Auth
// ---------------------------------------------------------------------------

export async function authWithPassword(identity: string, password: string) {
  const res = await request<{ token: string; record: Record<string, unknown> }>(
    '/api/collections/_superusers/auth-with-password',
    {
      method: 'POST',
      body: JSON.stringify({ identity, password }),
    },
  )
  setAuth(res.token, res.record)
  return res
}

// ---------------------------------------------------------------------------
// Collections
// ---------------------------------------------------------------------------

export async function listCollections(params?: { sort?: string; filter?: string; batch?: number }): Promise<CollectionModel[]> {
  // Base returns paginated; use perPage=500 to get all in one shot
  const q = qs({ sort: params?.sort, filter: params?.filter, perPage: params?.batch ?? 500 })
  const res = await request<ListResult<CollectionModel>>(`/api/collections${q}`)
  return res.items
}

export async function getCollection(id: string): Promise<CollectionModel> {
  return request<CollectionModel>(`/api/collections/${encodeURIComponent(id)}`)
}

export async function updateCollection(id: string, data: Record<string, unknown>): Promise<CollectionModel> {
  return request<CollectionModel>(`/api/collections/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body: JSON.stringify(data),
  })
}

export async function deleteCollection(id: string): Promise<void> {
  return request<void>(`/api/collections/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export async function importCollections(collections: CollectionModel[]): Promise<void> {
  return request<void>('/api/collections/import', {
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
  return request<ListResult<RecordModel>>(`/api/collections/${encodeURIComponent(collection)}/records${q}`)
}

export async function getRecord(collection: string, id: string): Promise<RecordModel> {
  return request<RecordModel>(`/api/collections/${encodeURIComponent(collection)}/records/${encodeURIComponent(id)}`)
}

export async function createRecord(collection: string, data: FormData | Record<string, unknown>): Promise<RecordModel> {
  const body = data instanceof FormData ? data : JSON.stringify(data)
  return request<RecordModel>(`/api/collections/${encodeURIComponent(collection)}/records`, {
    method: 'POST',
    body,
  })
}

export async function updateRecord(collection: string, id: string, data: FormData | Record<string, unknown>): Promise<RecordModel> {
  const body = data instanceof FormData ? data : JSON.stringify(data)
  return request<RecordModel>(`/api/collections/${encodeURIComponent(collection)}/records/${encodeURIComponent(id)}`, {
    method: 'PATCH',
    body,
  })
}

export async function deleteRecord(collection: string, id: string): Promise<void> {
  return request<void>(`/api/collections/${encodeURIComponent(collection)}/records/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

// ---------------------------------------------------------------------------
// Logs
// ---------------------------------------------------------------------------

export async function listLogs(page: number, perPage: number, params?: { sort?: string; filter?: string }): Promise<ListResult<LogModel>> {
  const q = qs({ page, perPage, sort: params?.sort, filter: params?.filter })
  return request<ListResult<LogModel>>(`/api/logs${q}`)
}

// ---------------------------------------------------------------------------
// Settings
// ---------------------------------------------------------------------------

export async function getSettings(): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>('/api/settings')
}

export async function updateSettings(data: Record<string, unknown>): Promise<Record<string, unknown>> {
  return request<Record<string, unknown>>('/api/settings', {
    method: 'PATCH',
    body: JSON.stringify(data),
  })
}

export async function testEmail(collection: string, toEmail: string, template: string): Promise<void> {
  return request<void>('/api/settings/test/email', {
    method: 'POST',
    body: JSON.stringify({ email: toEmail, template, collection }),
  })
}

// ---------------------------------------------------------------------------
// Backups
// ---------------------------------------------------------------------------

export async function listBackups(): Promise<BackupModel[]> {
  return request<BackupModel[]>('/api/backups')
}

export async function createBackup(name: string): Promise<void> {
  return request<void>('/api/backups', {
    method: 'POST',
    body: JSON.stringify({ name }),
  })
}

export async function deleteBackup(key: string): Promise<void> {
  return request<void>(`/api/backups/${encodeURIComponent(key)}`, { method: 'DELETE' })
}

export async function restoreBackup(key: string): Promise<void> {
  return request<void>(`/api/backups/${encodeURIComponent(key)}/restore`, { method: 'POST' })
}

export function getBackupDownloadURL(key: string, token: string): string {
  return `/api/backups/${encodeURIComponent(key)}?token=${encodeURIComponent(token)}`
}

export async function getFileToken(): Promise<string> {
  const res = await request<{ token: string }>('/api/files/token', { method: 'POST' })
  return res.token
}

// ---------------------------------------------------------------------------
// Crons
// ---------------------------------------------------------------------------

export async function listCrons(): Promise<CronModel[]> {
  const res = await request<CronModel[]>('/api/crons')
  return res
}

export async function runCron(jobId: string): Promise<void> {
  return request<void>(`/api/crons/${encodeURIComponent(jobId)}`, { method: 'POST' })
}

// ---------------------------------------------------------------------------
// Superusers (convenience)
// ---------------------------------------------------------------------------

export async function listSuperusers(params?: { sort?: string }): Promise<RecordModel[]> {
  const q = qs({ sort: params?.sort, perPage: 200 })
  const res = await request<ListResult<RecordModel>>(`/api/collections/_superusers/records${q}`)
  return res.items
}
