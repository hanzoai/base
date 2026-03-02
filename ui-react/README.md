# Base Admin — React 19 + Vite 8 + @luxfi/ui

Replacement for the legacy Svelte 4 superuser UI in `../ui/`. Same API
contract (PocketBase SDK against the Base server), new stack that
matches every other Hanzo/Lux/Liquidity frontend:

| Layer | Stack |
|---|---|
| Framework | React 19 |
| Bundler | Vite 8 |
| Router | TanStack Router (file-based) |
| Data | TanStack React Query |
| Forms | react-hook-form |
| UI kit | `@luxfi/ui` 7.3 (wraps `@hanzogui/*` 3.x) |
| Realtime | PocketBase SDK 0.26 (EventSource-backed) |

## Develop

```sh
pnpm install
pnpm dev          # Vite on :3000, proxies /api and /realtime to Base on :8090
```

## Build

```sh
pnpm build        # dist/ is picked up by embed.go and served at /_/
```

## Deploy

Swap the admin UI import in `base.go`:

```go
import ui "github.com/hanzoai/base/ui-react"   // React (new default)
// import ui "github.com/hanzoai/base/ui"      // Svelte (legacy, kept during migration)
```

The Go server wires `ui.DistDirFS()` into the `/_/*` route.

## Port status

172 Svelte components in `../ui/src/`. The React scaffold ships the
critical path; the remaining components are mechanical ports.

| Page | Svelte file(s) | React route | Status |
|---|---|---|---|
| Login | `components/SuperusersLogin.svelte` | `routes/login.tsx` | ✅ |
| Dashboard | `components/Dashboard.svelte` | `routes/index.tsx` | ✅ |
| Collections list | `components/collections/CollectionsList.svelte` | `routes/collections.tsx` | ✅ template |
| Collection editor | `components/collections/CollectionUpsertPanel.svelte` (+ fields, rules, indexes) | `routes/collections.$id.tsx` | ⬜ |
| Records list | `components/records/RecordsList.svelte` (+ RecordUpsertPanel) | `routes/collections.$id.records.tsx` | ⬜ |
| Logs | `components/logs/*.svelte` | `routes/logs.tsx` | ✅ basic |
| Log detail | `components/logs/LogViewPanel.svelte` | `routes/logs.$id.tsx` | ⬜ |
| Settings | `components/settings/*.svelte` | `routes/settings.tsx` | ✅ shell |
|  · SMTP | `components/settings/PageSmtp.svelte` | `routes/settings.smtp.tsx` | ⬜ |
|  · S3 | `components/settings/PageBackups.svelte` | `routes/settings.backups.tsx` | ⬜ |
|  · OAuth2 providers | `components/settings/PageApplicationAuth.svelte` | `routes/settings.auth.tsx` | ⬜ |
|  · Mail templates | `components/settings/PageMails.svelte` | `routes/settings.mail.tsx` | ⬜ |
|  · Token options | `components/settings/PageTokenOptions.svelte` | `routes/settings.tokens.tsx` | ⬜ |
|  · Export / Import | `components/settings/PageExportCollections.svelte` | `routes/settings.data.tsx` | ⬜ |
|  · Superusers | `components/settings/PageSuperusers.svelte` | `routes/settings.superusers.tsx` | ⬜ |
| Realtime inspector | `components/collections/RealtimePanel.svelte` | `routes/realtime.tsx` | ⬜ |
| SQL runner | (legacy) | `routes/sql.tsx` | ⬜ |

### Port recipe

Every remaining Svelte page is ~30 minutes to port:

1. **Data fetch** — replace Svelte reactive statements with `useQuery`:
    ```ts
    // Svelte:  $: records = $pb.collection(name).getList(...)
    // React:   const q = useQuery({ queryKey: [name, page], queryFn: ... })
    ```
2. **Mutations** — replace `on:click={save}` with `useMutation`:
    ```ts
    const m = useMutation({ mutationFn: (v) => pb.collection(name).update(id, v), onSuccess: () => qc.invalidateQueries(...) });
    ```
3. **Forms** — swap `bind:value` + `on:submit` for `react-hook-form`:
    ```tsx
    const { register, handleSubmit } = useForm<Values>();
    <form onSubmit={handleSubmit(save)}> <input {...register('email')} /> </form>
    ```
4. **Realtime** — `base.collection(name).subscribe('*', cb)` in a `useEffect`.
5. **Routing** — create `src/routes/<slug>.tsx`, export `Route = createFileRoute('/slug')({ component })`.

All Svelte-specific plugins (`svelte-spa-router`, `svelte-flatpickr`,
`svelte` itself, `sass`) go away. Chart / Leaflet imports are React-compatible
npm packages already (`chart.js`, `react-leaflet`).

## Why replace Svelte

- Match every other Hanzo / Lux / Liquidity frontend (React + @luxfi/ui + TanStack Query).
- Share login / auth / token flows with IAM and Base products (one `useAuth` hook).
- Remove the duplicate toolchain (no Svelte compiler in CI for admin paths).
- Component library parity with the public-facing explorer and exchange.
