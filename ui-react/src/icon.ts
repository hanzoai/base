// The admin mounts under a base path (default /_/), so an asset addressed from
// the root reaches the server's single-page fallback instead of the file: the
// browser is handed HTML where it asked for an image, at 200, and the logo just
// renders broken with nothing in the console and nothing in the network log.
//
// Vite rewrites the references it can see — the ones in index.html and anything
// imported — but a path written as a string in JSX is opaque to it and ships
// verbatim. So the base belongs in one value, here, rather than at each usage.
export const icon = `${import.meta.env.BASE_URL}icon.svg`
