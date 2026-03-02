import { createRootRoute, Link, Outlet } from '@tanstack/react-router';

import { useAuth } from '~/hooks/useAuth';

// Root layout: sidebar on every page except /login. Auth gate is
// implemented here so we don't repeat it in every child route.
function RootLayout() {
  const { isAuthenticated, record, signOut } = useAuth();

  return (
    <div className="flex h-screen bg-neutral-950 text-neutral-100">
      { isAuthenticated && (
        <aside className="w-56 border-r border-neutral-800 p-4">
          <div className="mb-6 flex items-center gap-2">
            <img src="/icon.svg" alt="Base" className="h-6 w-6" />
            <span className="font-semibold">Base Admin</span>
          </div>
          <nav className="flex flex-col gap-1 text-sm">
            <Link
              to="/"
              className="rounded px-2 py-1 hover:bg-neutral-800"
              activeProps={{ className: 'rounded px-2 py-1 bg-neutral-800 text-neutral-100' }}
              activeOptions={{ exact: true }}
            >
              Dashboard
            </Link>
            <Link
              to="/collections"
              className="rounded px-2 py-1 hover:bg-neutral-800"
              activeProps={{ className: 'rounded px-2 py-1 bg-neutral-800 text-neutral-100' }}
            >
              Collections
            </Link>
            <Link
              to="/logs"
              className="rounded px-2 py-1 hover:bg-neutral-800"
              activeProps={{ className: 'rounded px-2 py-1 bg-neutral-800 text-neutral-100' }}
            >
              Logs
            </Link>
            <Link
              to="/settings"
              className="rounded px-2 py-1 hover:bg-neutral-800"
              activeProps={{ className: 'rounded px-2 py-1 bg-neutral-800 text-neutral-100' }}
            >
              Settings
            </Link>
          </nav>
          <div className="absolute bottom-4 left-4 right-4 text-xs text-neutral-400">
            <div className="truncate">{ record?.email ?? '' }</div>
            <button onClick={ signOut } className="mt-1 text-neutral-500 hover:text-neutral-200">
              Sign out
            </button>
          </div>
        </aside>
      ) }
      <main className="flex-1 overflow-auto p-6">
        <Outlet />
      </main>
    </div>
  );
}

export const Route = createRootRoute({ component: RootLayout });
