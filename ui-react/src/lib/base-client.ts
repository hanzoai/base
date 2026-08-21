// BaseClient — the ergonomic object API the admin routes are written against
// (`base.collection(x).getList(...)`, `base.settings.getAll()`, …). It is a
// thin facade over the typed `/v1` fetch layer in `./api`; no fetch logic is
// duplicated here. One shared instance is exported from `./base`.
import * as api from '~/lib/api'
import type { CollectionModel, ListResult, RecordModel } from '~/lib/api'

export type { CollectionField, CollectionModel, FunctionModel, ListResult, RecordModel } from '~/lib/api'

type Data = FormData | Record<string, unknown>
type ListOpts = { sort?: string; filter?: string }

class CollectionHandle {
  constructor(private readonly name: string) {}

  getList(page: number, perPage: number, opts?: ListOpts): Promise<ListResult<RecordModel>> {
    return api.listRecords(this.name, page, perPage, opts)
  }

  getFullList(opts?: ListOpts): Promise<RecordModel[]> {
    return api.getFullRecords(this.name, opts)
  }

  getOne(id: string): Promise<RecordModel> {
    return api.getRecordById(this.name, id)
  }

  create(data: Data): Promise<RecordModel> {
    return api.createRecord(this.name, data)
  }

  update(id: string, data: Data): Promise<RecordModel> {
    return api.updateRecord(this.name, id, data)
  }

  delete(id: string): Promise<void> {
    return api.deleteRecord(this.name, id)
  }

  // Realtime: topic '*' means the whole collection; a record id scopes to it.
  subscribe(topic: string, cb: (e: api.RealtimeEvent) => void): Promise<() => void> {
    const realtimeTopic = !topic || topic === '*' ? this.name : `${this.name}/${topic}`
    return Promise.resolve(api.subscribeRecords(realtimeTopic, cb))
  }
}

class AuthStore {
  get token(): string {
    return api.getToken()
  }

  get record(): Record<string, unknown> | null {
    return api.getRecord()
  }

  // A session, not a string — the same predicate the route guards ask through
  // `session()`, minus the renewal. The sign-in page reads it: someone whose
  // bearer is dead came there to fix exactly that, and renewing under them
  // would be answering a question they did not ask.
  get isValid(): boolean {
    return api.live()
  }

  get isSuperuser(): boolean {
    return api.getRecord()?.collectionName === '_superusers'
  }

  clear(): void {
    api.clearAuth()
  }
}

export class BaseClient {
  readonly authStore = new AuthStore()

  // The admin UI is served same-origin as the API, so relative `/v1` paths in
  // the fetch layer resolve correctly; baseUrl is accepted for call-site parity.
  constructor(_baseUrl?: string) {}

  collection(name: string): CollectionHandle {
    return new CollectionHandle(name)
  }

  readonly collections = {
    getFullList: (opts?: ListOpts & { batch?: number }): Promise<CollectionModel[]> =>
      api.listCollections(opts),
    getOne: (id: string): Promise<CollectionModel> => api.getCollection(id),
    update: (id: string, data: Record<string, unknown>): Promise<CollectionModel> =>
      api.updateCollection(id, data),
    delete: (id: string): Promise<void> => api.deleteCollection(id),
    import: (collections: CollectionModel[]): Promise<void> => api.importCollections(collections),
    // Mints fresh signing secrets for the collection's tokens. Carries no
    // value — a secret is the server's to choose.
    rotate: (id: string): Promise<CollectionModel> =>
      api.request<CollectionModel>(`/v1/collections/${encodeURIComponent(id)}/rotate`, { method: 'POST' }),
  }

  readonly settings = {
    getAll: (): Promise<Record<string, unknown>> => api.getSettings(),
    update: (data: Record<string, unknown>): Promise<Record<string, unknown>> =>
      api.updateSettings(data),
    testEmail: (email: string): Promise<void> => api.testEmail(email),
  }

  readonly logs = {
    getList: (page: number, perPage: number, opts?: ListOpts): Promise<ListResult<api.LogModel>> =>
      api.listLogs(page, perPage, opts),
    subscribe: (cb: (e: api.RealtimeEvent) => void): Promise<() => void> =>
      Promise.resolve(api.subscribeRecords('@log', cb)),
  }

  readonly backups = {
    getFullList: (): Promise<api.BackupModel[]> => api.listBackups(),
    create: (name: string): Promise<void> => api.createBackup(name),
    delete: (key: string): Promise<void> => api.deleteBackup(key),
    restore: (key: string): Promise<void> => api.restoreBackup(key),
    getDownloadURL: (key: string, token: string): string => api.getBackupDownloadURL(key, token),
  }

  readonly functions = {
    getFullList: (): Promise<api.FunctionModel[]> => api.listFunctions(),
    create: (id: string, source: string): Promise<api.FunctionModel> => api.createFunction(id, source),
    update: (id: string, source: string): Promise<api.FunctionModel> => api.updateFunction(id, source),
    delete: (id: string): Promise<void> => api.deleteFunction(id),
    invoke: (id: string, payload: string): Promise<unknown> => api.invokeFunction(id, payload),
  }

  readonly crons = {
    getFullList: (): Promise<api.CronModel[]> => api.listCrons(),
    run: (jobId: string): Promise<void> => api.runCron(jobId),
  }

  readonly database = {
    get: (): Promise<api.DatabaseModel> => api.getDatabase(),
    reclaim: (): Promise<api.Reclaimed> => api.reclaimDatabase(),
  }

  readonly files = {
    getToken: (): Promise<string> => api.getFileToken(),
  }
}
