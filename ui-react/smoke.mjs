/*
 * Renders the built admin in a real browser and reports what it finds.
 *
 * A green `pnpm build` does not prove this UI works. The stack is @hanzo/gui +
 * plain CSS custom properties, and BOTH layers fail silently: gui drops a prop
 * it does not recognise (no error, no type failure, just an unstyled element),
 * and CSS resolves an undefined `var(--x)` to nothing. So the checks here are
 * the two that catch silence:
 *
 *   1. every `var(--token)` src/index.css references resolves to a real value
 *      in the document — an empty one means the sheet lost its token source;
 *   2. every route paints: text in #root, no page error, and the control
 *      surfaces (.btn/.panel/.input) compute to real colours and radii.
 *
 * Serves dist/ at the root exactly as the Go server mounts it and stubs `/v1`,
 * so it runs against the committed bundle with no Base server and no network.
 *
 * Usage: node smoke.mjs   (needs playwright; point PLAYWRIGHT at a checkout
 * that has it if this package does not, e.g. PLAYWRIGHT=/path/to/playwright).
 */
import fs from 'node:fs'
import http from 'node:http'
import path from 'node:path'

const { chromium } = await import(process.env.PLAYWRIGHT ?? 'playwright')

const root = new URL('./dist/', import.meta.url).pathname
const css = fs.readFileSync(new URL('./src/index.css', import.meta.url), 'utf8')
// The tokens this sheet leans on. `--gap`/`--cols` are its own locals, set on
// the element, so they are not expected on :root.
const tokens = [...new Set([...css.matchAll(/var\((--[a-z0-9-]+)/g)].map((m) => m[1]))]
  .filter((t) => t !== '--gap' && t !== '--cols')

// Read the width the confirm dialog asks for out of the route that asks for it,
// so this check cannot drift from the source it is checking.
const DIALOG_MAX_W = Number(
  fs.readFileSync(new URL('./src/routes/collections_.$id_.records.tsx', import.meta.url), 'utf8')
    .match(/<DialogContent maxW=\{\s*(\d+)\s*\}/)?.[1],
)
if (!DIALOG_MAX_W) throw new Error('smoke: could not read DialogContent maxW from the records route')

// --- the stub server -------------------------------------------------------
const collection = {
  id: 'c1', name: 'posts', type: 'base', system: false,
  fields: [
    { id: 'f1', name: 'title', type: 'text', system: false, hidden: false, presentable: true },
    { id: 'f2', name: 'published', type: 'bool', system: false, hidden: false, presentable: false },
  ],
  indexes: [], listRule: null, viewRule: null, createRule: null, updateRule: null, deleteRule: null,
}
const record = { id: 'r1', collectionId: 'c1', collectionName: 'posts', created: '2026-01-01 00:00:00Z', updated: '2026-01-01 00:00:00Z', title: 'Hello', published: true }
const page1 = (items) => ({ page: 1, perPage: 30, totalItems: items.length, totalPages: 1, items })

function stub(url) {
  if (url === '/v1/collections') return page1([collection])
  if (url.startsWith('/v1/collections/') && url.endsWith('/records')) return page1([record])
  if (/^\/v1\/collections\/[^/]+\/records\/[^/]+$/.test(url)) return record
  if (url.startsWith('/v1/collections/')) return collection
  if (url === '/v1/logs') return page1([{ id: 'l1', created: '2026-01-01 00:00:00Z', level: 0, message: 'served', data: { type: 'request', status: 200 } }])
  if (url === '/v1/backups') return [{ key: 'base_data.zip', size: 1024, modified: '2026-01-01 00:00:00Z' }]
  if (url === '/v1/crons') return [{ id: '__pbDBOptimize__', expression: '0 0 * * *' }]
  if (url === '/v1/settings') {
    return {
      meta: { appName: 'Base', appURL: 'http://localhost:8090', senderName: 'Support', senderAddress: 'support@example.com' },
      smtp: { enabled: false, host: 'smtp.example.com', port: 587, username: '', authMethod: '', tls: true, localName: '' },
      backups: { cron: '', cronMaxKeep: 3, s3: { enabled: false, bucket: '', region: '', endpoint: '', accessKey: '', forcePathStyle: false } },
      s3: { enabled: false, bucket: '', region: '', endpoint: '', accessKey: '', forcePathStyle: false },
      logs: { maxDays: 7, minLevel: 0, logIP: true, logAuthId: false },
      rateLimits: { enabled: false, rules: [{ label: '*:auth', maxRequests: 2, duration: 3, audience: '' }] },
      trustedProxy: { headers: [], useLeftmostIP: false },
      batch: { enabled: false, maxRequests: 50, timeout: 3, maxBodySize: 0 },
    }
  }
  return page1([])
}

const server = http.createServer((req, res) => {
  const url = req.url.split('?')[0]
  // Realtime is SSE, not JSON — the browser aborts the EventSource on any
  // other MIME type, and the record grid subscribes on mount.
  if (url === '/v1/realtime' && req.method === 'GET') {
    res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
    return res.write('event: PB_CONNECT\ndata: {"clientId":"smoke"}\n\n')
  }
  if (url.startsWith('/v1/')) {
    res.writeHead(200, { 'content-type': 'application/json' })
    return res.end(JSON.stringify(stub(url)))
  }
  // SPA fallback: anything without a file extension is a client route.
  // This mirrors the Go handler, which serves the bundle at the root. A stale
  // mount here would pass the check against a layout nothing runs.
  let rel = url.slice(1)
  if (!rel || !path.extname(rel)) rel = 'index.html'
  fs.readFile(path.join(root, rel), (err, body) => {
    if (err) { res.writeHead(404); return res.end('not found') }
    const ext = path.extname(rel)
    const type = { '.js': 'text/javascript', '.css': 'text/css', '.woff2': 'font/woff2', '.svg': 'image/svg+xml' }[ext] ?? 'text/html'
    res.writeHead(200, { 'content-type': type })
    res.end(body)
  })
})
await new Promise((r) => server.listen(4600, r))
const origin = 'http://localhost:4600'

// --- the run ---------------------------------------------------------------
const routes = [
  '/login', '/', '/collections', '/collections/c1', '/collections/c1/records',
  '/collections/c1/records/r1', '/logs', '/settings', '/settings/application',
  '/settings/auth', '/settings/mail', '/settings/smtp', '/settings/backups',
  '/settings/tokens', '/settings/superusers', '/settings/crons',
  '/settings/logs', '/settings/rate-limits', '/settings/data',
]

const browser = await chromium.launch(process.env.CHROME ? { executablePath: process.env.CHROME } : {})
const page = await browser.newPage()
const errors = []
page.on('pageerror', (e) => errors.push(`${page.url().split('/_')[1]}: ${e.message}`))
page.on('console', (m) => { if (m.type() === 'error') errors.push(`${page.url().split('/_')[1]}: ${m.text()}`) })

const painted = {}
const styles = { unresolved: [], utilityClasses: [], computed: {} }

// Signed OUT first: /login is the only route that renders `.panel` (with
// /auth/callback), and every later navigation carries the token, so the sign-in
// card is the one surface a signed-in pass can never see. Look at it here.
await page.goto(`${origin}/login`, { waitUntil: 'domcontentloaded' })
await page.waitForFunction(() => (document.getElementById('root')?.innerText ?? '').trim().length > 0, { timeout: 15000 })
painted['/login (signed out)'] = (await page.locator('#root').innerText()).replace(/\s+/g, ' ').trim().slice(0, 72) || 'BLANK'
styles.computed.panel = await page.evaluate(() => {
  const el = document.querySelector('.panel')
  if (!el) return null
  const cs = getComputedStyle(el)
  return { backgroundColor: cs.backgroundColor, borderColor: cs.borderColor, borderRadius: cs.borderRadius, padding: cs.padding }
})

// Now signed in as a superuser, so the guarded routes render instead of
// bouncing. `collectionName` is load-bearing: the /settings guard keys the
// superuser off it (AuthStore.isSuperuser), exactly as the IAM callback sets it.
await page.evaluate(() => {
  localStorage.setItem('base_auth_token', 'smoke')
  localStorage.setItem('base_auth_record', JSON.stringify({ id: 's1', email: 'z@hanzo.ai', collectionName: '_superusers' }))
})

for (const route of routes) {
  // Not `networkidle`: the record grid holds an SSE stream open, so the network
  // is never idle. Wait for paint, then let the stubbed queries settle.
  await page.goto(origin + route, { waitUntil: 'domcontentloaded' })
  await page.waitForFunction(() => (document.getElementById('root')?.innerText ?? '').trim().length > 0, { timeout: 15000 })
  await page.waitForTimeout(300)
  const text = (await page.locator('#root').innerText()).replace(/\s+/g, ' ').trim()
  painted[route] = text ? text.slice(0, 72) : 'BLANK'

  // Probed on every route, not once: a control only proves it is styled on a
  // page that actually renders it.
  const seen = await page.evaluate((names) => {
    const rootStyle = getComputedStyle(document.documentElement)
    const of = (sel, ...props) => {
      const el = document.querySelector(sel)
      if (!el) return null
      const cs = getComputedStyle(el)
      return Object.fromEntries(props.map((p) => [p, cs[p]]))
    }
    return {
      unresolved: names.filter((n) => !rootStyle.getPropertyValue(n).trim()),
      computed: {
        body: of('body', 'backgroundColor', 'color', 'fontFamily'),
        btn: of('.btn', 'backgroundColor', 'borderRadius', 'padding'),
        input: of('.input', 'backgroundColor', 'borderColor', 'borderRadius'),
        chip: of('.chip:not([data-active])', 'borderColor', 'borderRadius', 'padding', 'fontSize'),
        chipOn: of('.chip[data-active="true"]', 'borderColor', 'backgroundColor'),
        code: of('code', 'backgroundColor', 'fontFamily', 'padding'),
        panel: of('.panel', 'backgroundColor', 'borderColor'),
        table: of('.table', 'borderColor', 'fontSize'),
      },
      // Utility-class residue: proof the Tailwind vocabulary is really gone.
      utilityClasses: [...document.querySelectorAll('[class]')]
        .flatMap((e) => String(e.className).split(/\s+/))
        .filter((c) => /^(flex|grid-cols|text-(xs|sm|base|lg)|bg-|border-[a-z]|[pm][xytblr]?-\d|gap-\d|rounded-|hover:|dark:)/.test(c)),
    }
  }, tokens)
  styles.unresolved.push(...seen.unresolved)
  styles.utilityClasses.push(...seen.utilityClasses)
  // Record the key even when the control was absent here, so a control absent
  // from EVERY route surfaces as an unprobed null rather than vanishing.
  for (const [k, v] of Object.entries(seen.computed)) if (!styles.computed[k]) styles.computed[k] = v
}
styles.unresolved = [...new Set(styles.unresolved)]

/*
 * The overlays, driven for real. They are the one part of this admin that is
 * @hanzo/gui components rather than CSS, and a gui overlay that fails does not
 * throw — it just never appears. So: open the row menu, pick Delete, and check
 * the confirm dialog is on screen at the width `DialogContent maxW` asks for.
 */
const overlays = {}
await page.goto(`${origin}/collections/c1/records`, { waitUntil: 'domcontentloaded' })
await page.getByLabel('Row actions').first().click()
overlays.menuItems = await page.getByText('Duplicate', { exact: true }).count()
await page.getByText('Delete', { exact: true }).first().click()
const panel = page.getByRole('dialog').first()
overlays.dialogVisible = await panel.waitFor({ state: 'visible', timeout: 5000 }).then(() => true, () => false)
// The panel itself, not an inner box: `DialogContent maxW={384}` is a gui
// shorthand, and a shorthand gui does not recognise is dropped in silence —
// the dialog still opens, just at the full-width default. So measure it, and
// measure it AFTER the enter animation: gui applies the content's style classes
// a frame late, and reading the box before they land reports the pre-animation
// width (~viewport), which looks exactly like the dropped-prop failure.
overlays.dialogMaxW = DIALOG_MAX_W
overlays.dialogWidth = overlays.dialogVisible
  ? await page.evaluate(async () => {
      const el = document.querySelector('[role="dialog"]')
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)))
      return Math.round(el.getBoundingClientRect().width)
    })
  : null

await browser.close()
server.close()

const blank = Object.entries(painted).filter(([, v]) => v === 'BLANK').map(([k]) => k)
// A control probed as `null` was never on screen — the check for it did not
// pass, it did not run. Report that as loudly as a wrong value.
const unprobed = Object.entries(styles.computed).filter(([, v]) => !v).map(([k]) => k)
console.log(JSON.stringify({
  routes: painted,
  blank,
  unresolvedTokens: styles.unresolved,
  utilityClasses: [...new Set(styles.utilityClasses)],
  unprobed,
  computed: styles.computed,
  overlays,
  errors: [...new Set(errors)],
}, null, 1))

const ok = !blank.length && !styles.unresolved.length && !styles.utilityClasses.length && !errors.length
  && !unprobed.length && overlays.menuItems > 0 && overlays.dialogVisible
  && overlays.dialogWidth === overlays.dialogMaxW
console.log(ok ? 'SMOKE OK' : 'SMOKE FAILED')
process.exit(ok ? 0 : 1)
