import { Link, Outlet, createRootRoute } from '@tanstack/react-router';
import { Database, LayoutDashboard, ScrollText, Settings } from 'lucide-react';

import { useAuth } from '~/hooks/useAuth';

const NAV = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard, exact: true },
  { to: '/collections', label: 'Collections', icon: Database, exact: false },
  { to: '/logs', label: 'Logs', icon: ScrollText, exact: false },
  { to: '/settings', label: 'Settings', icon: Settings, exact: false },
] as const;

// Root layout: sidebar on every page except /login. Auth gate lives here so
// child routes don't repeat it.
function RootLayout() {
  const { isAuthenticated, record, signOut } = useAuth();

  return (
    <div className="flex h-screen bg-background text-foreground">
      { isAuthenticated && (
        <aside className="flex w-56 flex-col border-r border-border p-3">
          <div className="mb-6 flex items-center gap-2 px-2 pt-1">
            <img src="/icon.svg" alt="Base" className="size-6" />
            <span className="font-semibold">Base</span>
          </div>
          <nav className="flex flex-col gap-0.5 text-sm">
            { NAV.map(({ to, label, icon: Icon, exact }) => (
              <Link
                key={ to }
                to={ to }
                activeOptions={ exact ? { exact: true } : undefined }
                className="flex items-center gap-2 rounded-md px-2 py-1.5 text-muted-foreground transition-colors hover:bg-accent hover:text-foreground"
                activeProps={{ className: 'flex items-center gap-2 rounded-md px-2 py-1.5 bg-accent text-foreground' }}
              >
                <Icon className="size-4" />
                { label }
              </Link>
            )) }
          </nav>
          <div className="mt-auto px-2 text-xs text-muted-foreground">
            <div className="truncate">{ String(record?.email ?? '') }</div>
            <button onClick={ signOut } className="mt-1 hover:text-foreground">
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
