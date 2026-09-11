# Hooks

A hook is JavaScript that runs inside the Base process: code that runs when a
record is written, a route of your own, or a job on a schedule. The `base`
binary built from `examples/base` loads hooks. A Go program that embeds Base
loads them only if it registers `plugins/jsvm`.

## Where hooks live

Base loads every file directly inside the hooks directory whose name ends in
`.base.js` or `.base.ts`.

- The directory is `--hooksDir`. Without it, Base uses `hooks` beside the data
  directory, so the default data directory `base_/data` gives `base_/hooks`.
- Subdirectories are not loaded. Keep shared code there and `require` it.
- A `.base.ts` file runs as plain JavaScript. A type annotation is a syntax
  error.
- At startup Base writes type declarations to `types.d.ts` in the data
  directory. An empty hook file gets a `/// <reference>` line pointing at it, so
  an editor can complete `$app`, `routerAdd` and the `on*` hooks.

## An example

A migration creates the collection, a record hook trims text before it is
saved, and a route counts the records.

`base_/migrations/1_notes.js`:

```js
migrate((app) => {
  app.save(new Collection({
    type: "base",
    name: "notes",
    listRule: "",
    viewRule: "",
    createRule: "",
    fields: [{ name: "text", type: "text", required: true }],
  }))
})
```

`base_/hooks/notes.base.js`:

```js
onRecordCreateRequest((e) => {
  e.record.set("text", e.record.getString("text").trim())
  e.next()
}, "notes")

routerAdd("GET", "/v1/notes/count", (e) => {
  const { label } = require(`${__hooks}/lib/label.js`)
  return e.json(200, { [label()]: $app.countRecords("notes") })
})
```

`base_/hooks/lib/label.js`:

```js
module.exports = { label: () => "notes" }
```

Start Base next to those files and call it:

```sh
./base serve --http 127.0.0.1:8090
```

```sh
curl -s -X POST http://127.0.0.1:8090/v1/collections/notes/records \
  -H 'Content-Type: application/json' -d '{"text":"   buy milk   "}'
{"collectionId":"hbc_3395098727","collectionName":"notes","id":"ot6ovxamu9s9ovi","text":"buy milk"}

curl -s http://127.0.0.1:8090/v1/notes/count
{"notes":1}
```

## How a hook file runs

Each file runs once, at startup, and registers handlers: `routerAdd`,
`routerUse`, `cronAdd`, and the `on*` functions such as `onRecordCreateRequest`
and `onBootstrap`. There is no `onServe`; add routes with `routerAdd`. The
handlers run later, on a pool of interpreters whose size is `--hooksPool`
(default 15).

**A handler cannot see the rest of its file.** Base compiles each handler from
its own source text, so a `const` at the top of the file does not exist inside
the handler. This route fails:

```js
const greeting = "hello"

routerAdd("GET", "/v1/gotcha", (e) => {
  return e.json(200, { greeting: greeting })
})
```

```sh
curl -s http://127.0.0.1:8090/v1/gotcha
{"data":{},"message":"Something went wrong while processing your request.","status":400}
```

Put shared code in a module and `require` it inside the handler, as the example
does. Modules are CommonJS: `module.exports` out, `require(path)` in. A relative
path resolves from the process's working directory, not from the hook file, so
build the path from `__hooks`, which holds the absolute path of the hooks
directory.

Record hooks take collection names after the handler, as
`onRecordCreateRequest(handler, "notes")` does. With no names, the handler runs
for every collection. A handler calls `e.next()` to let the rest of the chain
run, including Base's own work; a handler that returns without calling it stops
the chain there.

Inside a handler, `$app` is the Base the event belongs to. With IAM on, that is
the org's own Base, so a hook reads and writes that org's data. Record and model
hooks run on every org's Base. `routerAdd` and `cronAdd` register once, for the
whole process.

The interpreter is goja, a JavaScript engine written in Go. Beyond the language
it has `require`, `console`, `process` and `Buffer`, and no other Node.js
modules. Make HTTP requests with `$http.send`. Everything else Base offers is
declared in `types.d.ts`.

## Changing hooks while Base runs

With `--hooksWatch`, which is on by default, saving any file under the hooks
directory restarts Base in place: the same process, running the new code.
Hidden directories and `node_modules` are not watched. When the hooks directory
is a symlink, Base watches the directory it points to, but it does not follow
symlinks inside it. Windows cannot restart this way; restart by hand there.

A file that fails to load is logged and skipped while watching is on. With
`--hooksWatch=false`, the same failure stops the process.

## Migrations

Put schema in migrations. They are JavaScript files in `--migrationsDir`
(default `migrations` beside the data directory, so `base_/migrations`), and
`serve` applies the pending ones in file name order when it starts. Each file
calls `migrate(up, down)`; `down` is optional.

| Command | What it does |
|---|---|
| `base migrate up` | Applies pending migrations. |
| `base migrate down [n]` | Reverts the last `n`. |
| `base migrate create <name>` | Writes an empty migration file. |
| `base migrate collections` | Writes a migration holding every current collection. |
| `base migrate history-sync` | Forgets applied migrations whose files are gone. |

With `--automigrate`, on by default, a collection change a superuser makes over
`/v1` is also written out as a migration file.

## Before you ship hooks

A hook runs with the full authority of the process. `$os` can run commands and
read and write files, and `routerAdd` can claim any path. Load only hooks you
wrote or read. [threat-model.md](threat-model.md) covers what that means for a
deployment.

A route added with `routerAdd` applies no collection rules. Check them yourself
with `$app.canAccessRecord(record, e.requestInfo(), collection.listRule)`. The
[hybrid search example](../examples/hooks-hybrid-search) does this for every
result it returns.
