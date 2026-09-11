// Hybrid search over the docs collection.
//
// Two ranked lists: FTS5 matches ordered by BM25, and stored embeddings ordered
// by cosine similarity to the query vector. Reciprocal rank fusion merges them:
// a document scores the sum of 1 / (K + rank) over the lists it appears in.
// Every result then passes the collection's list rule, so the route returns
// what the caller could list from /v1/collections/docs/records.

const K = 60 // the RRF constant from Cormack, Clarke and Buettcher (2009)
const DEPTH = 100 // candidates taken from each list

// match quotes every word, so punctuation in the query is text to find rather
// than FTS5 syntax, and joins the words with OR, so any one can match.
function match(text) {
  return String(text || "")
    .split(/\s+/)
    .filter((word) => word)
    .map((word) => '"' + word.replace(/"/g, '""') + '"')
    .join(" OR ")
}

function lexical(app, text) {
  const query = match(text)
  if (!query) {
    return []
  }
  const rows = arrayOf(new DynamicModel({ id: "" }))
  app.db()
    .newQuery("SELECT id FROM docs_fts WHERE docs_fts MATCH {:query} ORDER BY bm25(docs_fts) LIMIT {:depth}")
    .bind({ query: query, depth: DEPTH })
    .all(rows)
  return rows.map((row) => row.id)
}

function cosine(a, b) {
  let dot = 0, aa = 0, bb = 0
  for (let i = 0; i < a.length; i++) {
    dot += a[i] * b[i]
    aa += a[i] * a[i]
    bb += b[i] * b[i]
  }
  return aa && bb ? dot / Math.sqrt(aa * bb) : 0
}

// semantic scores every stored embedding against the query vector. That is a
// full scan, which is fine for thousands of documents and not for millions.
function semantic(app, vector) {
  if (vector.length === 0) {
    return []
  }
  const rows = arrayOf(new DynamicModel({ id: "", embedding: "" }))
  app.db().newQuery("SELECT id, embedding FROM docs").all(rows)
  const scored = []
  for (const row of rows) {
    let embedding
    try {
      embedding = JSON.parse(row.embedding)
    } catch (_) {
      continue
    }
    if (Array.isArray(embedding) && embedding.length === vector.length) {
      scored.push({ id: row.id, score: cosine(vector, embedding) })
    }
  }
  return scored
    .sort((a, b) => b.score - a.score)
    .slice(0, DEPTH)
    .map((s) => s.id)
}

function search(e) {
  const info = e.requestInfo()
  const body = info.body
  const docs = e.app.findCollectionByNameOrId("docs")

  const vector = []
  for (let i = 0; body.vector && i < body.vector.length; i++) {
    vector.push(Number(body.vector[i]))
  }

  const scores = {}
  for (const list of [lexical(e.app, body.q), semantic(e.app, vector)]) {
    list.forEach((id, rank) => {
      scores[id] = (scores[id] || 0) + 1 / (K + rank + 1)
    })
  }

  const limit = Math.min(Math.max(parseInt(body.limit, 10) || 10, 1), 50)
  const items = []
  for (const id of Object.keys(scores).sort((a, b) => scores[b] - scores[a])) {
    let record
    try {
      record = e.app.findRecordById(docs, id)
    } catch (_) {
      continue // deleted since it was ranked
    }
    if (!e.app.canAccessRecord(record, info, docs.listRule)) {
      continue
    }
    items.push({ id: id, title: record.getString("title"), score: scores[id] })
    if (items.length === limit) {
      break
    }
  }

  return e.json(200, { items: items })
}

module.exports = { search }
