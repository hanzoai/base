# Hybrid search with hooks

Search a collection by words and by meaning in one request. SQLite FTS5 ranks
the words with BM25, cosine similarity ranks stored embeddings against a query
vector, and reciprocal rank fusion merges the two rankings. Every result then
passes the collection's list rule.

| File | What it does |
|---|---|
| `migrations/1_docs.js` | creates the `docs` collection, an FTS5 table over its title and body, and the triggers that keep that table current |
| `migrations/2_sample_docs.js` | adds four documents with three-number embeddings |
| `hooks/search.base.js` | registers `POST /v1/docs/search` |
| `hooks/search.js` | ranks, fuses, and checks each result against the list rule |

## Run it

Build the binary as the [README](../../README.md#run-it) shows. Then, from
`examples/base`:

```sh
./base serve --http 127.0.0.1:8090 --dir "$(mktemp -d)" \
  --hooksDir ../hooks-hybrid-search/hooks \
  --migrationsDir ../hooks-hybrid-search/migrations
```

In another terminal:

```sh
curl -s -X POST http://127.0.0.1:8090/v1/docs/search \
  -H 'Content-Type: application/json' \
  -d '{"q": "rank", "vector": [0.1, 0.9, 0]}'
```

```json
{"items":[{"id":"5iy49tdy89q0knu","score":0.03252247488101534,"title":"Reciprocal rank fusion"},{"id":"ahov6ixwj26jsd8","score":0.01639344262295082,"title":"Vector similarity"},{"id":"mehis4xhns53hh6","score":0.015873015873015872,"title":"SQLite full-text search"},{"id":"zl12ppvcbbtofhv","score":0.015625,"title":"Access rules"}]}
```

"Reciprocal rank fusion" comes first because it is in both rankings: it is the
only document containing the word `rank`, and its embedding is the second
closest to the vector. Send only `q` to search by words, only `vector` to search
by meaning, and `limit` to cap the results (default 10, at most 50).

## How it works

- The triggers write to `docs_fts` in the same transaction as the write to
  `docs`, whether the write came through the API, a hook, a migration or plain
  SQL.
- The query is split into words and each word is quoted, so FTS5 reads
  punctuation as text rather than as query syntax. Any word may match.
- Each ranking gives its top 100 documents `1 / (60 + rank)`, and a document's
  scores add up.
- A route added in a hook applies no rules, so `search.js` calls
  `canAccessRecord` with the collection's `listRule` for each result. Set that
  rule to `@request.auth.id != ""` and an anonymous search returns no items.

## Limits

- Cosine similarity reads every stored embedding on every query. That suits
  thousands of documents, not millions.
- `docs_fts` is matched to `docs` by `id`, so updating or deleting a document
  scans `docs_fts`. The same scale applies.
- This example computes no embeddings. Store each document's vector from your
  model in `embedding`, and send the query's vector, of the same length, as
  `vector`.
- If you change the fields of `docs`, change the triggers in a new migration.
