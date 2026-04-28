# Base Admin UI (embedded)

This package embeds the Base admin SPA via `//go:embed all:dist`. The
binary serves `dist/` at `/_/` when `BASE_ENABLE_ADMIN_UI=1`.

The SPA itself is **not built here**. Source lives in
`~/work/hanzo/gui/apps/admin-base` (Hanzo GUI v7 + Vite). Build there
and sync the result with the script in this repo.

## Build & sync

```sh
# 1. Build the SPA in the gui workspace
cd ../gui/apps/admin-base
bun run build

# 2. Sync into ./dist (this directory) for go:embed
cd -
scripts/sync-admin-ui.sh
```

The sync writes a `.sync-stamp` so binaries can be traced back to the
source commit.

## Stack

| Layer | Stack |
|---|---|
| Framework | React 19 |
| Bundler | Vite 8 |
| Router | react-router-dom 7 |
| Data | `useFetch` from `@hanzogui/admin/data` |
| UI kit | Hanzo GUI v7 (`hanzogui` umbrella + `@hanzogui/admin` chrome) |
| Auth | Base `_superusers` token in localStorage; `useAuth` hook |

## Pages shipped

| Page | Path | File |
|---|---|---|
| Login | `/_/login` | `gui/apps/admin-base/src/pages/Login.tsx` |
| Collections | `/_/collections` | `Collections.tsx` |
| Records | `/_/collections/:id/records` | `Records.tsx` |
| Logs | `/_/logs` | `Logs.tsx` |
| Settings · SMTP | `/_/settings/smtp` | `SettingsSmtp.tsx` |
| Settings · Rate limits | `/_/settings/rate-limits` | `SettingsRateLimits.tsx` |
| Settings · Tokens | `/_/settings/tokens` | `SettingsTokens.tsx` |

Auth, OAuth2, Backups, Mail templates, Superusers, Export/Import, SQL
runner, and Realtime inspector are not yet ported.

## Why this layout

- The SPA shares the `@hanzogui/admin` chrome with `apps/admin-tasks`
  (and any other admin surface in `apps/admin-*`). One way to build
  admin UI across tasks/kms/commerce/console/base.
- `dist/` is committed-tracked at sync time so `go build` is hermetic
  — CI doesn't need bun installed to compile the binary.
- The static extractor must run; without resolved theme CSS the runtime
  `getThemeProxied()` throws "Missing theme" and renders blank.
