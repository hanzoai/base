// Four sample documents with three-number embeddings, so the example runs
// without an embedding model. Real embeddings come from your model and are
// written to the embedding field like any other value.
migrate((app) => {
  const docs = app.findCollectionByNameOrId("docs")
  const samples = [
    ["SQLite full-text search", "FTS5 ranks matching rows with BM25.", [0.9, 0.1, 0.0]],
    ["Vector similarity", "Cosine similarity compares embeddings by angle.", [0.1, 0.9, 0.0]],
    ["Reciprocal rank fusion", "RRF merges ranked lists without tuning weights.", [0.5, 0.5, 0.1]],
    ["Access rules", "A list rule decides which records a caller may see.", [0.0, 0.2, 0.9]],
  ]
  for (const [title, body, embedding] of samples) {
    app.save(new Record(docs, { title, body, embedding }))
  }
})
