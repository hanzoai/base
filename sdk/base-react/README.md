# Moved

The React bindings for Hanzo Base are no longer a separate package. They now
ship inside the JavaScript/TypeScript client `@hanzo/base` as a subpath export:

**https://github.com/hanzo-js/base**

```sh
npm install @hanzo/base
```

```ts
import { BaseProvider, useQuery, useAuth } from '@hanzo/base/react'
```

The standalone `@hanzoai/base-react` package is retired — its hooks
(`useQuery`, `useMutation`, `useAuth`, `useRealtime`, `useConnectionState`,
`usePresence`, `useCRDT`, …) are all provided by `@hanzo/base/react`, which
is a superset. There is exactly one React entry point now.
