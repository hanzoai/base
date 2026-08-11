import { Link, Outlet, createRootRoute } from '@tanstack/react-router';
import { Boxes, Database, LayoutDashboard, ScrollText, Settings } from '@hanzogui/lucide-icons-2';

import { useAuth } from '~/hooks/useAuth';
import { icon } from '../icon'

const NAV = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard, exact: true },
  { to: '/bases', label: 'Bases', icon: Boxes, exact: false },
  { to: '/collections', label: 'Collections', icon: Database, exact: false },
  { to: '/logs', label: 'Logs', icon: ScrollText, exact: false },
  { to: '/settings', label: 'Settings', icon: Settings, exact: false },
] as const;

// Root layout: sidebar on every page except /login. Auth gate lives here so
// child routes don't repeat it.
function RootLayout() {
  const { isAuthenticated, record, signOut } = useAuth();

  return (
    <div className="shell">
      { isAuthenticated && (
        <aside className="shell__nav">
          <div className="shell__brand">
            <img src={icon} alt="Base" />
            <span>Base</span>
          </div>
          <nav className="stack stack--tight">
            { NAV.map(({ to, label, icon: Icon, exact }) => (
              <Link
                key={ to }
                to={ to }
                className="shell__link"
                activeOptions={ exact ? { exact: true } : undefined }
                activeProps={{ 'data-active': 'true' }}
              >
                <Icon size={ 16 } />
                { label }
              </Link>
            )) }
          </nav>
          <div className="shell__foot">
            <div className="truncate">{ String(record?.email ?? '') }</div>
            <button type="button" onClick={ signOut } className="link">
              Sign out
            </button>
          </div>
        </aside>
      ) }
      <main className="shell__main">
        <Outlet />
      </main>
    </div>
  );
}

export const Route = createRootRoute({ component: RootLayout });
