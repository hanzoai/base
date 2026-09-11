# Access rules and identity

A collection has five rules: `listRule`, `viewRule`, `createRule`, `updateRule`
and `deleteRule`. They are all of Base's authorization. Base checks them in Go
on every `/v1` request. A list rule becomes part of the query, so a row the
caller may not see is neither returned nor counted.

## Rule values

| Value | Who passes | A caller who fails gets |
|---|---|---|
| `null`, the default | superusers | `403 Only superusers can perform this action.` |
| `""` | everyone, signed in or not | |
| an expression | callers for whom it is true | list: a page with no items; view, update, delete: `404`; create: `400 Failed to create record.` |

An expression is a filter over the record and the request:

```
@request.auth.id != ""
owner = @request.auth.id
status = "published" || owner = @request.auth.id
@request.body.role:isset = false
```

| Part | Forms |
|---|---|
| Compare | `=` `!=` `>` `>=` `<` `<=` `~` (contains) `!~` |
| Compare any of several values | `?=` `?!=` `?>` `?>=` `?<` `?<=` `?~` `?!~` |
| Combine | `&&` `\|\|` and parentheses |
| The request | `@request.auth.*` `@request.body.*` `@request.query.*` `@request.headers.*` `@request.method` `@request.context` |
| Another collection | `@collection.<name>.<field>` |
| Modifiers | `:isset` `:changed` `:length` `:each` `:lower` |

`?filter=` on list requests uses the same syntax.

Rules also decide what `?expand=` returns and what the `/v1/rest/{table}` reads
return. A route you add in a hook applies no rule on its own. Call
`$app.canAccessRecord(record, e.requestInfo(), collection.listRule)` for each
record, as the [hybrid search example](../examples/hooks-hybrid-search/hooks/search.js)
does.

## Where identity comes from

Base signs nobody in. It has no passwords, no sign-up and no sessions of its
own. A caller is whoever a Hanzo IAM access token says, sent as
`Authorization: Bearer <token>`.

Point Base at IAM to turn this on:

```sh
IAM_ENDPOINT=https://hanzo.id ./base serve --http 127.0.0.1:8090
```

With `IAM_ENDPOINT` set, Base:

- verifies every token against IAM's keys at
  `$IAM_ENDPOINT/v1/iam/.well-known/jwks`, and treats a token that fails as no
  token;
- ignores tokens signed any other way, including those an older Base issued;
- passes `/v1/iam/*` through to IAM;
- lists IAM as the one sign-in provider at
  `GET /v1/collections/users/auth-methods`, with IAM's authorize URL and a PKCE
  challenge;
- deletes `X-Org-Id`, `X-User-Id` and `X-User-Email` from every request as it
  arrives. `X-Org-Id` is read first, as the org the caller asks for.

For the length of one request, the token becomes an auth record that is never
saved:

| Field | From the token |
|---|---|
| `id` | `sub` (else `preferred_username`): kept when it is 15 characters of `a-z`, `0-9` and `_`, padded with `_` when shorter, otherwise the first 24 hex characters of its SHA-256 |
| `email` | `email` |
| `name` | `name`, else `displayName` |
| `org_id` | the org the request acts in, when the auth collection has an `org_id` field |

Rules read these as `@request.auth.id`, `@request.auth.email` and so on.

**Superusers are the members of IAM's `admin` org**, and nobody else. Their
record is mirrored into `_superusers`; everyone else's into `users`. A superuser
passes every rule, manages collections and settings, and can act in any org. An
`admin` role inside an ordinary org is a different thing and grants none of
this.

**Each org has its own database.** Base takes the org from the token's
memberships and opens `<data dir>/orgs/<org>/data.db` for the request. Two orgs
never share a file, so a query in one cannot return another's rows. A token
that belongs to several orgs picks one with `X-Org-Id: <org>`. Naming an org
the token does not carry gets `403`, and so does a token with no org.

**Keys** stand in for tokens when no person is signing in. IAM issues them.
`sk-` keys are for servers. `pk-` keys are for web pages and reach only routes
marked public, such as record lists and views.

## Without IAM

Leave `IAM_ENDPOINT` unset and the `base` binary serves one Base with no orgs.
There is no sign-in route, so a caller without a token cannot get one: every
such caller is anonymous and none is a superuser. That is enough to build
against collections whose rules admit anonymous callers. Define the collections
in [migrations](hooks.md#migrations), because `/v1/collections` needs a
superuser.

Tokens Base signed itself still count, though, and `auth-refresh` renews them.
A superuser token left from an older version is one; [versions.md](versions.md)
shows how to revoke it.

Such a Base prints this when it starts:

```
(!) No superuser exists yet. Create the first one with:
    ./base superuser upsert EMAIL PASS
```

That command was removed. Superusers come from IAM.

## Close the open users collection

A new Base has a `users` collection whose `createRule` is `""`, so anyone can
add rows to it. Nobody can sign in as those rows, but it is still a public
write. Close it with a migration:

```js
migrate((app) => {
  const users = app.findCollectionByNameOrId("users")
  users.createRule = null
  app.save(users)
})
```

After it runs, an anonymous `POST /v1/collections/users/records` gets `403`.

Rate limits and CORS have defaults to change too. See
[threat-model.md](threat-model.md).
