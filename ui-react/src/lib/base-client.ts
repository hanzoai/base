// BaseClient — the ergonomic object API the admin routes are written against
// (`base.collection(x).getList(...)`, `base.settings.getAll()`, …). It is a
// thin facade over the typed `/v1` fetch layer in `./api`; no fetch logic is
// duplicated here. One shared instance is exported from `./base`.
import * as api from '~/lib/api'
import type { CollectionModel, ListResult, RecordModel } from '~/lib/api'

export type { CollectionField, CollectionModel, ListResult, RecordModel } from '~/lib/api'

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

  async authWithPassword(
    identity: string,
    password: string,
  ): Promise<{ token: string; record: Record<string, unknown> }> {
    const res = await api.request<{ token: string; record: Record<string, unknown> }>(
      `/v1/collections/${encodeURIComponent(this.name)}/auth-with-password`,
      { method: 'POST', body: JSON.stringify({ identity, password }) },
    )
    api.setAuth(res.token, res.record)
    return res
  }
}

class AuthStore {
  get token(): string {
    return api.getToken()
  }

  get record(): Record<string, unknown> | null {
    return api.getRecord()
  }

  get isValid(): boolean {
    return Boolean(api.getToken())
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
  }

  readonly settings = {
    getAll: (): Promise<Record<string, unknown>> => api.getSettings(),
    update: (data: Record<string, unknown>): Promise<Record<string, unknown>> =>
      api.updateSettings(data),
    testEmail: (collection: string, email: string, template: string): Promise<void> =>
      api.testEmail(collection, email, template),
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

  readonly crons = {
    getFullList: (): Promise<api.CronModel[]> => api.listCrons(),
    run: (jobId: string): Promise<void> => api.runCron(jobId),
  }

  readonly files = {
    getToken: (): Promise<string> => api.getFileToken(),
  }
}
