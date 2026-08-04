import { GuiProvider } from '@hanzo/gui';
import guiConfig from '@hanzo/ui/gui-config';
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

// Admin ships dark-only, true-black. Pin the theme class on <html> once —
// @hanzo/design keys its palette off it, and it is the same signal GuiProvider's
// `defaultTheme` gives the component layer.
document.documentElement.classList.add('dark');

const queryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
});

const rootEl = document.getElementById('root');
if (!rootEl) throw new Error('root element not found');

// `guiConfig` is @hanzo/ui's own scale — the same one the console and hanzo.ai
// render against, so this admin cannot drift from them.
createRoot(rootEl).render(
  <React.StrictMode>
    <GuiProvider config={ guiConfig } defaultTheme="dark">
      <QueryClientProvider client={ queryClient }>
        <RouterProvider router={ router } />
      </QueryClientProvider>
    </GuiProvider>
  </React.StrictMode>,
);
