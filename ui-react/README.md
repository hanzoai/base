# Base Admin UI

The Base admin SPA. Source is `./src`; `pnpm build` writes `./dist`, and
`embed.go` compiles that into the Base binary with `//go:embed all:dist`,
which the server mounts at `/_/` when `BASE_ENABLE_ADMIN_UI=1`.

`dist/` is committed so `go build` is hermetic — CI compiles the binary
without a Node toolchain. Rebuild it in the same commit as any `src/` change.

## Build

```sh
pnpm install
pnpm build      # tsc --noEmit && vite build  ->  ./dist
pnpm dev        # vite, proxying /v1 to a local Base on :8090
```

Two env knobs, both set at build time and both mirrored by the Go server:

| Env | Default | Read by |
|---|---|---|
| `BASE_ADMIN_UI_PATH` | `/_/` | Vite `base` + the Go static mount |
| `VITE_IAM_URL` / `VITE_IAM_CLIENT_ID` | `https://hanzo.id` / `hanzo-base` | `src/lib/iam.ts` |

## Stack

One framework, all platforms. No Tailwind, no Radix, no shadcn.

| Layer | What |
|---|---|
| Framework | React 19 + TanStack Router (file-based, `src/routes/**`) + TanStack Query |
| Bundler | Vite 8 (rolldown) |
| Components | `@hanzo/ui` on `@hanzo/gui` — the cross-platform primitives, same import on web/native/desktop |
| Icons | `@hanzogui/lucide-icons-2` |
| Styling | `src/index.css`: `@hanzo/design` tokens (plain CSS custom properties) + ~30 class names for the things this admin has |
| Data | One `/v1` fetch layer (`src/lib/api.ts`) behind a `base.collection(x).method()` facade (`src/lib/base-client.ts`); realtime is `/v1/realtime` SSE |
| Auth | Hanzo IAM, OAuth2 PKCE via `@hanzo/iam` (`src/lib/iam.ts`). No local password — the fork retired `_superusers` password auth and the server 404s `auth-with-password` for every collection |

Where the line falls: overlays (dialog, dropdown) are `@hanzo/ui` components,
because they carry a11y, focus management and placement that CSS cannot. Every
other surface is a class in `index.css`. A route says what a thing IS.

## Verifying a change

`pnpm build` going green does **not** prove a visual change worked. `@hanzo/gui`
drops a prop it does not recognise in silence — no error, no type failure, just
an unstyled element — and CSS resolves an undefined `var(--x)` to nothing. Both
layers fail quietly, so run the browser check:

```sh
pnpm smoke      # needs playwright; PLAYWRIGHT=/path/to/playwright/index.mjs
```

It serves the committed `dist/` under `/_/` exactly as the Go server does, stubs
`/v1`, and gates on: every route paints, every `var(--token)` resolves, zero
utility-class residue, every control computes real colours and radii, the
overlays open, and the confirm dialog measures the width `DialogContent maxW`
asks for. That last one is the shape of the silent-drop bug, so it is asserted,
not just printed.
