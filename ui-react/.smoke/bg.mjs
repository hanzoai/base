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
await new Promise((r) => srv.listen(4601, r))
const b = await chromium.launch({ executablePath: '/home/z/.cache/ms-playwright/chromium-1234/chrome-linux/chrome' })
const pg = await b.newPage()
await pg.goto('http://localhost:4601/_/login', { waitUntil: 'networkidle' })
console.log(JSON.stringify(await pg.evaluate(() => {
  const hits = []
  for (const sheet of document.styleSheets) {
    let rules; try { rules = sheet.cssRules } catch { continue }
    for (const r of rules) {
      if (r.selectorText && /(^|,)\s*(body|html)\s*($|,)/.test(r.selectorText) && /background/.test(r.cssText)) {
        hits.push({ owner: sheet.ownerNode?.tagName + (sheet.href ? ' ' + sheet.href.split('/').pop() : ' <style>'),
                    sel: r.selectorText, val: r.cssText.match(/--background\s*:\s*([^;]+)/)?.[1] })
      }
    }
  }
  return { hits: hits.slice(0, 12), resolved: getComputedStyle(document.documentElement).getPropertyValue('--background') }
}), null, 1))
await b.close(); srv.close()
