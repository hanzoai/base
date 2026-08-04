import { chromium } from '/home/z/src/hanzo/desktop/node_modules/playwright/index.mjs'
import http from 'node:http'; import fs from 'node:fs'; import path from 'node:path'
const root = new URL('../dist/', import.meta.url).pathname
const srv = http.createServer((req, res) => {
  const url = req.url.split('?')[0]
  if (url.startsWith('/v1/')) { res.writeHead(200, {'content-type':'application/json'}); return res.end('[]') }
  let rel = url.startsWith('/_/') ? url.slice(3) : url.slice(1)
  if (!rel || !path.extname(rel)) rel = 'index.html'
  fs.readFile(path.join(root, rel), (e, b) => { if (e) { res.writeHead(404); return res.end('nf') }
    res.writeHead(200); res.end(b) })
})
await new Promise((r) => srv.listen(4602, r))
const b = await chromium.launch({ executablePath: '/home/z/.cache/ms-playwright/chromium-1234/chrome-linux/chrome' })
const pg = await b.newPage()
await pg.goto('http://localhost:4602/_/login', { waitUntil: 'networkidle' })
const r = await pg.evaluate(() => ({
  htmlClass: document.documentElement.className,
  htmlStyleAttr: document.documentElement.getAttribute('style'),
  bodyInline: document.body.getAttribute('style'),
  bodyComputed: getComputedStyle(document.body).backgroundColor,
  htmlComputed: getComputedStyle(document.documentElement).backgroundColor,
  varOnBody: getComputedStyle(document.body).getPropertyValue('--background'),
  rootChildClass: document.getElementById('root')?.firstElementChild?.className?.slice(0, 120),
  rootChildBg: document.getElementById('root')?.firstElementChild
    ? getComputedStyle(document.getElementById('root').firstElementChild).backgroundColor : null,
}));
console.log(JSON.stringify(r, null, 1))
await b.close(); srv.close()
