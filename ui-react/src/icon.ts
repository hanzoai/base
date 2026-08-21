// The admin mounts under a base path (default /_/), so an asset addressed from
// the root reaches the server's single-page fallback instead of the file: the
// browser is handed HTML where it asked for an image, at 200, and the logo just
// renders broken with nothing in the console and nothing in the network log.
//
// Vite rewrites the references it can see — the ones in index.html and anything
// imported — but a path written as a string in JSX is opaque to it and ships
// verbatim. So the base belongs in one value, here, rather than at each usage.
// The mark is the SERVING HOST'S, not a compiled-in one, for the same reason the
// title is: one binary answers for Hanzo, Lux and Zoo. index.html resolves which
// brand this host is before the bundle loads and leaves the answer on
// window.__brand — read here rather than re-derived, so there is exactly one map
// from host to brand and no way for the name and the logo to disagree.
//
// An unknown host falls back to the unbranded mark. That is deliberate: lending
// our logo to a lookalike domain is worse than showing a generic glyph.
const slug = typeof window === 'undefined' ? '' : ((window as { __brand?: string }).__brand ?? '')

// ?v is a CACHE EVICTION, not decoration. The edge cached the SPA fallback under
// /brands/hanzo/logo.svg — it was requested before the release that added the
// file — and that entry answers HTML at 200 for a fortnight. The header that
// allowed it is fixed in apis/serve.go, but a stored answer cannot be fixed from
// the origin, only stepped around. Bump this if it ever happens again.
export const icon = slug
  ? `${import.meta.env.BASE_URL}brands/${slug}/logo.svg?v=2`
  : `${import.meta.env.BASE_URL}icon.svg`
