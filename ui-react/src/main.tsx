import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider, createRouter } from '@tanstack/react-router';
import React from 'react';
import { createRoot } from 'react-dom/client';

import './index.css';
import { routeTree } from './routeTree.gen';

// TanStack Router reads the file-based route tree generated from src/routes/**.
// basepath is bound to the Vite base (BASE_ADMIN_UI_PATH, default '/_/') so the
// router matches under the mount prefix the Go server serves the SPA from.
const router = createRouter({ routeTree, basepath: import.meta.env.BASE_URL });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}

// Admin ships dark-only, true-black. Pin the theme class on <html> once.
document.documentElement.classList.add('dark');

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

const rootEl = document.getElementById('root');
if (!rootEl) throw new Error('root element not found');

createRoot(rootEl).render(
  <React.StrictMode>
    <QueryClientProvider client={ queryClient }>
      <RouterProvider router={ router } />
    </QueryClientProvider>
  </React.StrictMode>,
);
