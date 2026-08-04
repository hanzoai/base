import { chromium } from '/home/z/src/hanzo/desktop/node_modules/playwright/index.mjs'
import http from 'node:http'; import fs from 'node:fs'; import path from 'node:path'
const root = new URL('./out/', import.meta.url).pathname
const srv = http.createServer((req, res) => {
  const p = path.join(root, req.url === '/' ? 'index.html' : req.url.split('?')[0])
  fs.readFile(p, (e, b) => { if (e) { res.writeHead(404); res.end('nf'); return }
    res.writeHead(200, { 'content-type': p.endsWith('.js') ? 'text/javascript' : 'text/html' }); res.end(b) })
})
await new Promise((r) => srv.listen(4599, r))
const b = await chromium.launch({ executablePath: '/home/z/.cache/ms-playwright/chromium-1234/chrome-linux/chrome' })
const pg = await b.newPage(); const errs = []
pg.on('pageerror', (e) => errs.push('pageerror: ' + e.message))
await pg.goto('http://localhost:4599/', { waitUntil: 'networkidle' })
const secType = await pg.locator('#sec').getAttribute('type')
const pwType = await pg.locator('#pwtype').getAttribute('type')
for (const t of ['ClickMe', 'OnClickMe', 'SubmitMe']) { await pg.getByText(t, { exact: true }).first().click(); await pg.waitForTimeout(150) }
await pg.locator('#nativebtn').click(); await pg.waitForTimeout(150)
const hits = await pg.evaluate(() => JSON.stringify(window.__hits ?? []))
const tags = await pg.evaluate(() => ['ClickMe','OnClickMe','SubmitMe'].map(t => {
  const el = [...document.querySelectorAll('*')].find(e => e.textContent === t && e.children.length === 0)
  return t + '=' + (el ? el.tagName + '/' + (el.parentElement?.tagName) : 'missing')
}))
console.log(JSON.stringify({ secType, pwType, hits, tags, errs: errs.slice(0,4) }, null, 1))
await b.close(); srv.close()
