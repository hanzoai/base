# Cron → Tasks migration

`tools/cron` is now a thin shim over `tools/tasks`. Every scheduled job
registered through `app.Cron()` runs via a single `*tasks.Client` — durable
Temporal workflows when `TASKS_URL` is set, local goroutine tickers otherwise.

## What didn't change

- `app.Cron().Add(id, expr, fn)` keeps its signature. Existing callers compile
  and run without edits. Services like BD, ATS, TA, etc. need no code changes.
- `cron.NewSchedule(expr)` is still the cron-expression validator used by
  `core/settings_model.go` for `Backups.Cron` validation.
- `GET /crons` and `POST /crons/{id}` admin endpoints keep the same JSON shape
  (`[{"id": "...", "expression": "..."}]`) and run-now semantics.

## What changed

- `app.Cron()` and `app.Tasks()` share one underlying `*tasks.Client`. Jobs
  registered through either surface are visible to both.
- There is no global 1-minute ticker any more — every schedule has its own
  goroutine (local mode) or Temporal schedule (durable mode). Sub-minute
  expressions work natively (e.g. `"30s"`, `"*/30 * * * * *"`).
- `cron.Cron.Stop()` now pauses tickers without dropping the registry.
  `cron.Cron.Start()` resumes them. This matches the previous semantics from
  the ticker-based implementation.
- `cron.Cron.SetInterval()` is a no-op. The global tick cadence no longer
  exists — each schedule's duration/expression is authoritative.
- `cron.Cron.SetTimezone()` is accepted but only the expression-evaluation
  timezone is propagated; durable schedules run in the tasks server's timezone
  (UTC by default).

## Call-site rule going forward

Use `app.Tasks()` for new code:

```go
app.Tasks().Add("settlement", "30s", fn)          // recurring, duration
app.Tasks().Add("daily-cleanup", "0 3 * * *", fn) // recurring, cron
app.Tasks().Now("webhook.deliver", payload)        // one-shot
```

`app.Cron()` stays forever as the alias — no "deprecated in v2" churn.

## Environment switches

- `TASKS_URL` unset → pure local goroutine tickers (dev/test default).
- `TASKS_URL=http://tasks.hanzo.ai` → durable schedules on Hanzo Tasks.
- `TASKS_ZAP=host:port` → ZAP binary transport preferred over HTTP.

If the remote server is unreachable, schedules transparently fall back to
local tickers so the app never stops firing jobs.
