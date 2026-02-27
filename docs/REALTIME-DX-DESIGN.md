# Hanzo Base: Realtime DX Design
## Inspired by Convex, Built on Go

### Vision
Take the best developer experience patterns from Convex (reactive queries, type-safe
mutations, optimistic updates, automatic cache invalidation) and implement them natively
in Hanzo Base's Go backend with SSE/WebSocket transport.

---

## 1. Reactive Queries (Convex-Inspired)

### Current: PocketBase SSE Subscriptions
```typescript
// Subscribe to all changes in a collection
base.collection('tasks').subscribe('*', callback)
```

### Proposed: Reactive Query Hooks
```typescript
import { useQuery, useMutation } from '@hanzoai/base-react'

// Reactive query — auto-updates when data changes
const tasks = useQuery('tasks', {
  filter: 'status = "active" && assignee = @request.auth.id',
  sort: '-created',
  expand: 'assignee',
})

// Returns: { data, isLoading, error, refetch }
```

**Implementation:** Client SDK wraps SSE subscription + initial fetch into a single
reactive primitive. Query deduplication ensures multiple components subscribing to the
same query share one SSE connection.

---

## 2. Type-Safe Mutations with Optimistic Updates

### Proposed API
```typescript
const createTask = useMutation('tasks', 'create')
const updateTask = useMutation('tasks', 'update')

// With optimistic update
await createTask(
  { title: 'Ship it', status: 'active' },
  {
    optimistic: (store) => {
      const current = store.getQuery('tasks', currentFilter)
      store.setQuery('tasks', currentFilter, [
        ...current,
        { id: 'temp_' + Date.now(), title: 'Ship it', status: 'active' }
      ])
    }
  }
)
```

**Implementation:** Optimistic update layer sits between SSE subscription and React
state. On mutation, apply optimistic update immediately. When server confirms via SSE
transition, merge or rollback.

---

## 3. Query Deduplication & Connection Management

```
Component A: useQuery('tasks', filter1)  ─┐
                                           ├─> Single SSE subscription
Component B: useQuery('tasks', filter1)  ─┘

Component C: useQuery('tasks', filter2)  ─── Separate subscription
```

**Implementation:**
- Hash query + args to create subscription key
- Reference count subscriptions
- Auto-unsubscribe when last component unmounts
- Reconnect with max observed timestamp to catch missed updates

---

## 4. Type Generation from Schema

### Proposed CLI
```bash
base typegen --output types.ts
```

### Generated Output
```typescript
// types.ts (auto-generated from Base schema)
export interface Tasks {
  id: string
  title: string
  status: 'active' | 'done'
  assignee: string
  created: string
  updated: string
}

export interface Users {
  id: string
  email: string
  name: string
  avatar: string
  created: string
  updated: string
}

export type Collections = {
  tasks: Tasks
  users: Users
}
```

**Implementation:** Go tool reads Base schema (collection definitions) and generates
TypeScript interfaces. Runs as `base typegen` CLI command or Go plugin.

---

## 5. Scheduled Functions (Post-Commit)

### Proposed API (JSVM)
```javascript
// Schedule after record creation
onRecordAfterCreateSuccess((e) => {
  // This runs AFTER the record is committed to DB
  // ensuring the user sees the record before notifications fire
  scheduleAfter(0, 'notify_user', { recordId: e.record.id })
}, 'tasks')

// Delayed execution
scheduleAfter(5000, 'send_reminder', { taskId: '...' })

// At specific time
scheduleAt(new Date('2025-03-01'), 'generate_report', {})
```

**Implementation:** Go-side scheduler with exactly-once semantics for mutations,
at-most-once for actions. Stored in Base's internal `_scheduled_functions` collection.

---

## 6. Enhanced WebSocket Protocol

### Current: SSE (Server-Sent Events)
One-way server→client. Simple but limited.

### Proposed: Hybrid SSE + WebSocket
- **SSE** for standard subscriptions (simple, HTTP/2 compatible)
- **WebSocket** for bidirectional real-time features:
  - Presence (who's online)
  - Cursor tracking
  - Live collaboration

### State Version Tracking (from Convex)
```typescript
interface StateVersion {
  querySet: number    // Which queries are subscribed
  ts: bigint          // Server timestamp
  identity: number    // Auth version (detect stale tokens)
}

interface Transition {
  startVersion: StateVersion
  endVersion: StateVersion
  modifications: Modification[]  // Atomic batch of changes
}
```

---

## 7. React SDK Package

### Package: `@hanzoai/base-react`

```typescript
// Provider
import { BaseProvider } from '@hanzoai/base-react'

function App() {
  return (
    <BaseProvider url="http://localhost:8090">
      <TaskList />
    </BaseProvider>
  )
}

// Hooks
import { useQuery, useMutation, useAuth, useRealtime } from '@hanzoai/base-react'

function TaskList() {
  const { data: tasks, isLoading } = useQuery('tasks', {
    filter: 'status = "active"',
    sort: '-created',
  })

  const createTask = useMutation('tasks', 'create')
  const { user, signIn, signOut } = useAuth()

  // Low-level realtime access
  useRealtime('tasks', '*', (event) => {
    // Custom handler for special cases
  })

  if (isLoading) return <Loading />

  return (
    <ul>
      {tasks.map(task => <TaskItem key={task.id} task={task} />)}
    </ul>
  )
}
```

---

## Implementation Priority

1. **Phase 1: React hooks wrapper** — `useQuery`, `useMutation` over existing SSE
2. **Phase 2: Query deduplication** — Share SSE connections, reference counting
3. **Phase 3: Optimistic updates** — Client-side state layer
4. **Phase 4: Type generation** — `base typegen` CLI command
5. **Phase 5: Scheduled functions** — Go-side scheduler with JSVM integration
6. **Phase 6: WebSocket upgrade** — Presence, cursors, collaboration
7. **Phase 7: State version tracking** — Convex-style transitions for consistency

---

## Architecture Comparison

| Feature | Convex | Supabase | **Hanzo Base** |
|---------|--------|----------|---------------|
| Reactive queries | WebSocket | Postgres LISTEN/NOTIFY | SSE → WebSocket |
| Optimistic updates | Built-in | Manual | Planned (Phase 3) |
| Type generation | Auto from schema | `supabase gen types` | `base typegen` |
| Cloud functions | JS in V8 isolates | Edge Functions (Deno) | JSVM (Go embedded) |
| Auth | Custom | GoTrue | Built-in + IAM |
| File storage | Built-in | S3 + imgproxy | Built-in + S3 |
| Local dev | Single binary | Docker Compose | **Single binary** |
| Production | Hosted only | Self-host or cloud | Self-host + K8s |
