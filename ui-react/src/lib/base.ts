// Shared Base client instance. `BaseClient` is the ergonomic object API over
// the `/v1` fetch layer (see ./base-client). One instance for the whole admin;
// same bundle works against any Base deploy since the fetch layer is relative.
import { BaseClient } from '~/lib/base-client'

export type { CollectionField, CollectionModel, ListResult, RecordModel } from '~/lib/base-client'

export const base = new BaseClient()

// Commonly-used handles so pages don't reach into the client.
export const superusers = base.collection('_superusers')
export const settings = base.settings
