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
  if (url === '/v1/realtime/token') return { token: 'a-stream-grant' }
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

// Every stream the page opened, with its query. A stream is authenticated by
// the grant it carries and by nothing else, so a stream opened without one is
// a stream that will be served from the wrong Base — and it fails the same
// silent way an unstyled control does: connected, subscribed to nothing.
const streamed = []

// Every rotation the page asked for, with what it sent. A signing secret is
// the server's to mint, so the request that replaces one has to arrive empty —
// a body here would mean the browser chose the material.
const rotations = []

const server = http.createServer((req, res) => {
  const url = req.url.split('?')[0]
  if (req.method === 'POST' && url.endsWith('/rotate')) {
    let body = ''
    req.on('data', (chunk) => { body += chunk })
    return req.on('end', () => {
      rotations.push({ url, body })
      res.writeHead(200, { 'content-type': 'application/json' })
      res.end(JSON.stringify(collection))
    })
  }
  // A record save is refused for want of a session, so the recovery path below
  // has something to recover from.
  if (req.method === 'PATCH' && /^\/v1\/collections\/[^/]+\/records\/[^/]+$/.test(url)) {
    res.writeHead(401, { 'content-type': 'application/json' })
    return res.end(JSON.stringify({ status: 401, message: 'The request requires a valid record authorization token.', data: {} }))
  }
  // Realtime is SSE, not JSON — the browser aborts the EventSource on any
  // other MIME type, and the record grid subscribes on mount. The stream is
  // opened with a grant in the query, the way the server reads one.
  if (url === '/v1/realtime' && req.method === 'GET') {
    streamed.push(req.url)
    res.writeHead(200, { 'content-type': 'text/event-stream', 'cache-control': 'no-cache' })
    return res.write('id:smoke\nevent:CONNECT\ndata:{"clientId":"smoke"}\n\n')
  }
  if (url.startsWith('/v1/')) {
    res.writeHead(200, { 'content-type': 'application/json' })
    return res.end(JSON.stringify(stub(url)))
  }
  // A host page, standing in for the Hanzo surface that offers Base as one of
  // its sections. Served from this origin so the frame shares the seeded
  // session — what is under test here is the layout the frame renders, and
  // `embedded` reads the frame itself, not who serves it.
  if (url === '/host') {
    res.writeHead(200, { 'content-type': 'text/html' })
    return res.end('<!doctype html><style>html,body{margin:0;height:100%}iframe{border:0;width:100%;height:100%}</style><iframe src="/collections"></iframe>')
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
  '/settings/auth', '/settings/smtp', '/settings/backups',
  '/settings/tokens', '/settings/crons',
  '/settings/logs', '/settings/rate-limits', '/settings/data',
]

const browser = await chromium.launch(process.env.CHROME ? { executablePath: process.env.CHROME } : {})
const page = await browser.newPage()
const errors = []
// The recovery block below asks the server to refuse a save, so the browser's
// own line about that refusal is the expected outcome rather than a finding.
// Nothing else in the run provokes one.
let refusalExpected = false
const where = () => new URL(page.url()).pathname
page.on('pageerror', (e) => errors.push(`${where()}: ${e.message}`))
page.on('console', (m) => {
  if (m.type() !== 'error') return
  if (refusalExpected && m.text().includes('status of 401')) return
  errors.push(`${where()}: ${m.text()}`)
})

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

/*
 * What the admin does when something goes wrong, which no amount of painting
 * shows. Three behaviours, each of which fails silently in its own way: a
 * document rendered as a React child throws and takes the whole app down, a
 * refused save looks like a dead end, and a rotation that carried its own
 * secret would look exactly like one that asked for it.
 */
const recovery = {}

// A pasted document is refused with something to read, and the page survives.
await page.goto(`${origin}/settings/data`, { waitUntil: 'domcontentloaded' })
await page.waitForTimeout(300)
await page.locator('textarea').fill('[{"id":"c1","name":{}}]')
await page.waitForTimeout(200)
recovery.importRefused = await page.getByText('is not a collection schema').count()

// A save the server refuses for want of a session keeps every value on screen
// and offers a way back that leaves this tab where it is.
refusalExpected = true
await page.goto(`${origin}/collections/c1/records/r1`, { waitUntil: 'domcontentloaded' })
await page.waitForTimeout(400)
const title = page.locator('input.input').first()
await title.fill('edited over a long sitting')
await page.getByRole('button', { name: 'Save', exact: true }).click()
await page.waitForTimeout(400)
recovery.editKept = await title.inputValue()
recovery.signInOffered = await page.getByText('Sign in again').count()
refusalExpected = false

// Rotation asks; it does not choose.
page.on('dialog', (d) => d.accept())
await page.goto(`${origin}/settings/tokens`, { waitUntil: 'domcontentloaded' })
await page.waitForTimeout(400)
await page.getByRole('button', { name: 'Rotate secrets' }).click()
await page.waitForTimeout(400)
recovery.rotations = rotations

// --- does it fit where it is put -------------------------------------------
//
// The admin has one browser and three places to render it: its own page on a
// desktop, its own page on a phone, and a section inside a Hanzo surface. The
// nav is a column in the first and a strip in the other two.
//
// The nav's WIDTH is the check that bites. A 14rem column on a 390px viewport
// leaves 166px and the table runs off the side of it — but `.shell__main` owns
// that overflow and scrolls it, so the document never grows and `scrollWidth`
// reports nothing wrong. Measuring the document alone would have passed the
// defect through; measuring the column is what fails on it. `overflow` stays
// because it is cheap and it is the thing that must not regress.
const fit = {}
const measure = () => page.evaluate(() => {
  const nav = document.querySelector('.shell__nav')
  const shell = document.querySelector('.shell')
  return {
    nav: nav ? Math.round(nav.getBoundingClientRect().width) : null,
    flow: shell ? getComputedStyle(shell).flexDirection : null,
    overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
    brand: !!document.querySelector('.shell__brand'),
    account: !!document.querySelector('.shell__foot'),
  }
})

await page.setViewportSize({ width: 1280, height: 900 })
await page.goto(`${origin}/collections`, { waitUntil: 'domcontentloaded' })
await page.waitForTimeout(300)
fit.desktop = await measure()

await page.setViewportSize({ width: 390, height: 844 })
await page.goto(`${origin}/collections`, { waitUntil: 'domcontentloaded' })
await page.waitForTimeout(300)
fit.phone = await measure()

// In the frame, at the width a host panel actually gives it.
await page.setViewportSize({ width: 1280, height: 900 })
await page.goto(`${origin}/host`, { waitUntil: 'domcontentloaded' })
await page.waitForTimeout(600)
const frame = page.frames().find((f) => f.url().includes('/collections'))
fit.embedded = frame
  ? await frame.evaluate(() => {
    const nav = document.querySelector('.shell__nav')
    const shell = document.querySelector('.shell')
    return {
      nav: nav ? Math.round(nav.getBoundingClientRect().width) : null,
      flow: shell ? getComputedStyle(shell).flexDirection : null,
      overflow: document.documentElement.scrollWidth - document.documentElement.clientWidth,
      brand: !!document.querySelector('.shell__brand'),
      account: !!document.querySelector('.shell__foot'),
      painted: (document.getElementById('root')?.innerText ?? '').replace(/\s+/g, ' ').trim().slice(0, 72),
    }
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
  streamed,
  recovery,
  fit,
  errors: [...new Set(errors)],
}, null, 1))

const granted = streamed.some((u) => u.includes('token=a-stream-grant'))
const recovered = recovery.importRefused > 0
  && recovery.editKept === 'edited over a long sitting'
  && recovery.signInOffered > 0
  && recovery.rotations.length === 1 && recovery.rotations[0].body === ''
// A column on the desktop; a strip that spans the width, and nothing past the
// right edge, on the phone and in the frame. The frame drops the brand and the
// account — the host already says which product you are in and who you are —
// and still paints the browser it was framed for.
const fits = fit.desktop.flow === 'row' && fit.desktop.nav > 200 && fit.desktop.nav < 260
  && fit.desktop.brand && fit.desktop.account && fit.desktop.overflow <= 0
  && fit.phone.flow === 'column' && fit.phone.nav > 320 && fit.phone.overflow <= 0
  && fit.phone.brand && fit.phone.account
  && !!fit.embedded && fit.embedded.flow === 'column' && fit.embedded.overflow <= 0
  && !fit.embedded.brand && !fit.embedded.account && fit.embedded.painted.length > 0
const ok = !blank.length && !styles.unresolved.length && !styles.utilityClasses.length && !errors.length
  && !unprobed.length && overlays.menuItems > 0 && overlays.dialogVisible
  && overlays.dialogWidth === overlays.dialogMaxW && granted && recovered && fits
console.log(ok ? 'SMOKE OK' : 'SMOKE FAILED')
process.exit(ok ? 0 : 1)
