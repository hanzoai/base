import { chromium } from '/home/z/src/hanzo/desktop/node_modules/playwright/index.mjs'
import http from 'node:http'; import fs from 'node:fs'; import path from 'node:path'
const root = new URL('../dist/', import.meta.url).pathname
// Serve the SPA under /_/ exactly as the Go server mounts it, and stub /v1 so
// the admin's queries resolve instead of hanging.
const srv = http.createServer((req, res) => {
  const url = req.url.split('?')[0]
  if (url.startsWith('/v1/')) {
    res.writeHead(200, { 'content-type': 'application/json' })
    if (url.includes('collections')) return res.end(JSON.stringify([{ id: 'c1', name: 'posts', type: 'base', system: false, fields: [] }]))
    return res.end(JSON.stringify({ items: [], page: 1, totalItems: 0, totalPages: 0 }))
  }
  let rel = url.startsWith('/_/') ? url.slice(3) : url.slice(1)
  if (!rel || !path.extname(rel)) rel = 'index.html'
  const p = path.join(root, rel)
  fs.readFile(p, (e, b) => {
    if (e) { res.writeHead(404); res.end('nf'); return }
    const ct = p.endsWith('.js') ? 'text/javascript' : p.endsWith('.css') ? 'text/css'
      : p.endsWith('.woff2') ? 'font/woff2' : p.endsWith('.svg') ? 'image/svg+xml' : 'text/html'
    res.writeHead(200, { 'content-type': ct }); res.end(b)
  })
})
await new Promise((r) => srv.listen(4600, r))
const b = await chromium.launch({ executablePath: '/home/z/.cache/ms-playwright/chromium-1234/chrome-linux/chrome' })
const pg = await b.newPage(); const errs = []
pg.on('pageerror', (e) => errs.push('pageerror: ' + e.message))
pg.on('console', (m) => { if (m.type() === 'error' && !m.text().includes('404')) errs.push('console: ' + m.text()) })

const out = {}
await pg.goto('http://localhost:4600/_/login', { waitUntil: 'networkidle' })
out.loginText = (await pg.locator('#root').innerText()).replace(/\s+/g, ' ').slice(0, 160)
out.loginBtn = await pg.getByText('Sign in with Hanzo').count()
// Computed styles prove the token layer actually resolved (not unstyled HTML).
out.styles = await pg.evaluate(() => {
  const b = getComputedStyle(document.body)
  const btn = document.querySelector('.btn')
  const cs = btn && getComputedStyle(btn)
  return {
    bodyBg: b.backgroundColor, bodyColor: b.color, font: b.fontFamily.slice(0, 40),
    btnBg: cs?.backgroundColor, btnRadius: cs?.borderRadius, btnPad: cs?.padding,
    panelBorder: getComputedStyle(document.querySelector('.panel')).borderColor,
  }
})
out.tailwindLeftOver = await pg.evaluate(() =>
  [...document.querySelectorAll('[class]')].map(e => e.className).join(' ')
    .split(/\s+/).filter(c => /^(flex|grid|text-|bg-|border-|px-|py-|gap-|rounded|hover:)/.test(c)).slice(0, 10))
await b.close(); srv.close()
console.log(JSON.stringify({ ...out, errs: errs.slice(0, 6) }, null, 1))
