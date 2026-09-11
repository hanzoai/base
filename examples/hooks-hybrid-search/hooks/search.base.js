// POST /v1/docs/search  {"q": "words", "vector": [0.1, 0.9, 0], "limit": 10}
//
// The handler is compiled on its own, so nothing declared at the top of this
// file is in scope inside it. The code lives in search.js and is required
// where it runs.
routerAdd("POST", "/v1/docs/search", (e) => {
  return require(`${__hooks}/search.js`).search(e)
})
