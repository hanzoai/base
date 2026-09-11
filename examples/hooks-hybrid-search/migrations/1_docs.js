// The docs collection, and an FTS5 index over its title and body.
//
// Three triggers keep the index in step with docs. They run inside the write's
// own transaction, whichever way the write arrived: the API, a hook, a
// migration or plain SQL.
migrate((app) => {
  app.save(new Collection({
    type: "base",
    name: "docs",
    listRule: "",
    viewRule: "",
    fields: [
      { name: "title", type: "text", required: true },
      { name: "body", type: "text" },
      { name: "embedding", type: "json" },
    ],
  }))

  const statements = [
    "CREATE VIRTUAL TABLE docs_fts USING fts5(id UNINDEXED, title, body)",
    "CREATE TRIGGER docs_fts_insert AFTER INSERT ON docs BEGIN INSERT INTO docs_fts (id, title, body) VALUES (new.id, new.title, new.body); END",
    "CREATE TRIGGER docs_fts_update AFTER UPDATE ON docs BEGIN UPDATE docs_fts SET title = new.title, body = new.body WHERE id = old.id; END",
    "CREATE TRIGGER docs_fts_delete AFTER DELETE ON docs BEGIN DELETE FROM docs_fts WHERE id = old.id; END",
  ]
  for (const sql of statements) {
    app.db().newQuery(sql).execute()
  }
}, (app) => {
  app.db().newQuery("DROP TABLE IF EXISTS docs_fts").execute()
  app.delete(app.findCollectionByNameOrId("docs"))
})
