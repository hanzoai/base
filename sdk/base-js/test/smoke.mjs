/**
 * Smoke test for @hanzoai/base core SDK.
 * Verifies: client construction, store operations, optimistic updates,
 * version tracking, realtime service instantiation, collection service,
 * file URL generation, CRDT primitives.
 */

import {
  BaseClient,
  MemoryAuthStore,
  FileService,
  QueryStore,
  VersionTracker,
  RealtimeService,
  CollectionService,
} from '../dist/core/index.js'

import {
  HybridLogicalClock,
  compareHLC,
  CRDTCounter,
  CRDTRegister,
  CRDTSet,
  CRDTText,
  CRDTDocument,
} from '../dist/crdt/index.js'

let passed = 0
let failed = 0

function assert(condition, msg) {
  if (condition) {
    passed++
  } else {
    failed++
    console.error(`  FAIL: ${msg}`)
  }
}

function assertEq(actual, expected, msg) {
  if (actual === expected) {
    passed++
  } else {
    failed++
    console.error(`  FAIL: ${msg} -- expected ${JSON.stringify(expected)}, got ${JSON.stringify(actual)}`)
  }
}

// ---- BaseClient -----------------------------------------------------------

console.log('=== BaseClient ===')

const client = new BaseClient('https://example.hanzo.ai/')
assertEq(client.url, 'https://example.hanzo.ai', 'trailing slash stripped')
assert(client.authStore !== null, 'authStore exists')
assert(client.store instanceof QueryStore, 'store is QueryStore')
assert(client.realtime instanceof RealtimeService, 'realtime is RealtimeService')
assert(client.files instanceof FileService, 'files is FileService')
assertEq(client.authStore.token, '', 'initial token is empty')
assertEq(client.authStore.isValid, false, 'initial auth is invalid')

// Config object form
const client2 = new BaseClient({ url: 'https://test.hanzo.ai' })
assertEq(client2.url, 'https://test.hanzo.ai', 'config object works')

// ---- AuthStore ------------------------------------------------------------

console.log('=== AuthStore ===')

const auth = new MemoryAuthStore()
assertEq(auth.token, '', 'empty token')
assertEq(auth.isValid, false, 'invalid when empty')

// Create a fake JWT with exp in the future
const header = btoa(JSON.stringify({ alg: 'HS256' }))
const payload = btoa(JSON.stringify({ exp: Math.floor(Date.now() / 1000) + 3600, sub: 'user1' }))
const fakeJwt = `${header}.${payload}.signature`

let authChanged = false
const unsub = auth.onChange(() => { authChanged = true })
auth.save(fakeJwt, { id: 'user1', name: 'Test User' })

assertEq(auth.token, fakeJwt, 'token saved')
assertEq(auth.isValid, true, 'valid JWT accepted')
assertEq(auth.record?.id, 'user1', 'record saved')
assertEq(authChanged, true, 'onChange fired')

unsub()
authChanged = false
auth.clear()
assertEq(auth.token, '', 'cleared token')
assertEq(authChanged, false, 'unsubscribed listener not called')

// ---- FileService ----------------------------------------------------------

console.log('=== FileService ===')

const files = new FileService('https://example.hanzo.ai')
const record = { id: 'rec123', collectionId: 'posts', collectionName: 'posts' }
const url = files.getURL(record, 'photo.jpg')
assertEq(url, 'https://example.hanzo.ai/api/files/posts/rec123/photo.jpg', 'basic file URL')

const urlThumb = files.getURL(record, 'photo.jpg', { thumb: '100x100' })
assert(urlThumb.includes('thumb=100x100'), 'thumb param in URL')

assertEq(files.getURL({ id: '' }, 'test.jpg'), '', 'empty id returns empty string')
assertEq(files.getURL(record, ''), '', 'empty filename returns empty string')

// ---- QueryStore -----------------------------------------------------------

console.log('=== QueryStore ===')

const store = new QueryStore()
assertEq(store.getQuery('posts', ''), undefined, 'empty store returns undefined')

store.setQuery('posts', '', [
  { id: '1', title: 'Hello' },
  { id: '2', title: 'World' },
])

const cached = store.getQuery('posts', '')
assertEq(cached?.length, 2, 'cached 2 records')
assertEq(cached?.[0].title, 'Hello', 'first record correct')

// Subscribe to changes
let notified = false
const unsubStore = store.subscribe('posts', '', () => { notified = true })

store.setQuery('posts', '', [{ id: '1', title: 'Updated' }])
assertEq(notified, true, 'subscriber notified on setQuery')
unsubStore()

// ---- Optimistic updates ---------------------------------------------------

console.log('=== Optimistic Updates ===')

store.setQuery('tasks', '', [
  { id: 't1', title: 'Task 1' },
  { id: 't2', title: 'Task 2' },
])

// Optimistic create
const mutId = store.optimisticSet('tasks', { id: 't3', title: 'Optimistic Task' })
const tasksWithOpt = store.getQuery('tasks', '')
assertEq(tasksWithOpt?.length, 3, 'optimistic add shows 3 records')
assert(tasksWithOpt?.some(r => r.id === 't3'), 'optimistic record present')

// Rollback
store.rollbackOptimistic(mutId)
const tasksAfterRollback = store.getQuery('tasks', '')
assertEq(tasksAfterRollback?.length, 2, 'rollback removes optimistic')

// Optimistic delete
const delId = store.optimisticDelete('tasks', 't1')
const tasksAfterDel = store.getQuery('tasks', '')
assertEq(tasksAfterDel?.length, 1, 'optimistic delete hides record')
assertEq(tasksAfterDel?.[0].id, 't2', 'correct record remains')

store.rollbackOptimistic(delId)
const tasksRestored = store.getQuery('tasks', '')
assertEq(tasksRestored?.length, 2, 'rollback restores deleted record')

// ---- Server update ingestion ----------------------------------------------

console.log('=== Server Update Ingestion ===')

store.applyServerUpdate('tasks', 'create', { id: 't4', title: 'Server Task' })
const tasksWithServer = store.getQuery('tasks', '')
assertEq(tasksWithServer?.length, 3, 'server create added record')

store.applyServerUpdate('tasks', 'update', { id: 't4', title: 'Updated Server Task' })
const updated = store.getQuery('tasks', '')?.find(r => r.id === 't4')
assertEq(updated?.title, 'Updated Server Task', 'server update applied')

store.applyServerUpdate('tasks', 'delete', { id: 't4' })
const tasksAfterServerDel = store.getQuery('tasks', '')
assertEq(tasksAfterServerDel?.length, 2, 'server delete removed record')

// ---- VersionTracker -------------------------------------------------------

console.log('=== VersionTracker ===')

const vt = new VersionTracker()
assertEq(vt.current.querySet, 0, 'initial querySet is 0')
assertEq(vt.current.ts, 0n, 'initial ts is 0n')

vt.advance([{ type: 'QueryUpdated', collection: 'posts', record: { id: '1' } }], 1000n)
assertEq(vt.current.querySet, 1, 'querySet incremented')
assertEq(vt.current.ts, 1000n, 'ts updated')

vt.setIdentity(42)
assertEq(vt.current.identity, 42, 'identity set')

const hash = VersionTracker.hashIdentity('test-token')
assert(typeof hash === 'number', 'hashIdentity returns number')
assert(hash > 0, 'hashIdentity non-zero')

// ---- CollectionService (construction check) --------------------------------

console.log('=== CollectionService ===')

const collSvc = client.collection('posts')
assert(collSvc instanceof CollectionService, 'collection() returns CollectionService')
assertEq(collSvc.collectionIdOrName, 'posts', 'collection name correct')

// Same instance returned
const collSvc2 = client.collection('posts')
assert(collSvc === collSvc2, 'collection() returns cached instance')

// Different collection
const collSvc3 = client.collection('users')
assert(collSvc3 !== collSvc, 'different collection returns different instance')

// ---- RealtimeService (construction check) ----------------------------------

console.log('=== RealtimeService ===')

const rt = new RealtimeService('https://example.hanzo.ai', () => '')
assertEq(rt.state, 'disconnected', 'initial state is disconnected')
assertEq(rt.maxTimestamp, 0n, 'initial maxTimestamp is 0n')

let connState = null
const unsubConn = rt.onConnectionChange((s) => { connState = s })
assert(typeof unsubConn === 'function', 'onConnectionChange returns unsub fn')
unsubConn()

// ---- CRDT: HybridLogicalClock -------------------------------------------

console.log('=== CRDT: HybridLogicalClock ===')

const clock1 = new HybridLogicalClock('site-a')
const clock2 = new HybridLogicalClock('site-b')

const t1 = clock1.now()
const t2 = clock1.now()
assert(compareHLC(t2, t1) > 0, 'sequential timestamps are ordered')

const t3 = clock2.receive(t2)
assert(compareHLC(t3, t2) > 0, 'received timestamp advances')

// ---- CRDT: CRDTCounter ---------------------------------------------------

console.log('=== CRDT: CRDTCounter ===')

const counter = new CRDTCounter('doc1', 'likes', clock1)
assertEq(counter.value, 0, 'initial counter is 0')

counter.increment(5)
assertEq(counter.value, 5, 'increment by 5')

counter.decrement(2)
assertEq(counter.value, 3, 'decrement by 2')

let counterVal = null
counter.onChange((v) => { counterVal = v })
counter.increment(1)
assertEq(counterVal, 4, 'onChange fires with new value')

const ops = counter.drainOps()
assertEq(ops.length, 3, '3 ops drained')

// ---- CRDT: CRDTRegister --------------------------------------------------

console.log('=== CRDT: CRDTRegister ===')

const reg = new CRDTRegister('doc1', 'title', clock1)
assertEq(reg.value, undefined, 'initial register is undefined')

reg.set('Hello World')
assertEq(reg.value, 'Hello World', 'register set')

// Remote wins with higher HLC
const remoteClock = new HybridLogicalClock('remote-site')
// Advance remote clock
for (let i = 0; i < 10; i++) remoteClock.now()
const remoteHlc = remoteClock.now()
const remoteOp = {
  id: 'remote:1:0',
  documentId: 'doc1',
  field: 'title',
  hlc: remoteHlc,
  type: 'register.set',
  payload: { value: 'Remote Wins' },
}
reg.applyRemote(remoteOp)
assertEq(reg.value, 'Remote Wins', 'LWW: remote with higher HLC wins')

// ---- CRDT: CRDTSet -------------------------------------------------------

console.log('=== CRDT: CRDTSet ===')

const set = new CRDTSet('doc1', 'tags', clock1)
assertEq(set.size, 0, 'initial set is empty')

set.add('typescript')
set.add('sdk')
assertEq(set.size, 2, 'set has 2 items')
assertEq(set.has('typescript'), true, 'set contains typescript')

set.remove('typescript')
assertEq(set.has('typescript'), false, 'typescript removed')
assertEq(set.size, 1, 'set has 1 item')

// ---- CRDT: CRDTText ------------------------------------------------------

console.log('=== CRDT: CRDTText ===')

const text = new CRDTText('doc1', 'body', clock1)
text.insert(0, 'Hello')
assertEq(text.toString(), 'Hello', 'insert at 0')
assertEq(text.length, 5, 'length is 5')

text.insert(5, ' World')
assertEq(text.toString(), 'Hello World', 'insert at end')

text.delete(5, 1)
assertEq(text.toString(), 'HelloWorld', 'delete space')

// ---- CRDT: CRDTDocument ---------------------------------------------------

console.log('=== CRDT: CRDTDocument ===')

const doc = new CRDTDocument('doc-abc')
assert(doc.clock instanceof HybridLogicalClock, 'document has clock')

const docText = doc.getText('content')
docText.insert(0, 'Hello from doc')
assertEq(docText.toString(), 'Hello from doc', 'doc text works')

const docCounter = doc.getCounter('views')
docCounter.increment(10)
assertEq(docCounter.value, 10, 'doc counter works')

const allOps = doc.collectOps()
assert(allOps.length > 0, 'collectOps returns operations')
assertEq(doc.getUnsyncedOps().length, allOps.length, 'unsynced ops tracked')

doc.acknowledge(allOps[allOps.length - 1].id)
assertEq(doc.getUnsyncedOps().length, 0, 'acknowledge clears unsynced')

// Encode/decode roundtrip
const doc2 = new CRDTDocument('doc-abc', 'other-site')
doc2.getText('content') // ensure field exists
const encoded = doc.encode()
doc2.decode(encoded)
// doc2 should have the same text from doc's new ops (which were collected again in encode)

// ---- Summary --------------------------------------------------------------

console.log('')
console.log(`=== Results: ${passed} passed, ${failed} failed ===`)
process.exit(failed > 0 ? 1 : 0)
