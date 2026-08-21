import { Link, Outlet, createRootRoute } from '@tanstack/react-router';
import { Boxes, Database, LayoutDashboard, ScrollText, Settings, SquareFunction } from '@hanzogui/lucide-icons-2';

import { useAuth } from '~/hooks/useAuth';
import { embedded } from '~/lib/embed';
import { icon } from '../icon'

const NAV = [
  { to: '/', label: 'Dashboard', icon: LayoutDashboard, exact: true },
  { to: '/bases', label: 'Bases', icon: Boxes, exact: false },
  { to: '/collections', label: 'Collections', icon: Database, exact: false },
  { to: '/functions', label: 'Functions', icon: SquareFunction, exact: false },
  { to: '/logs', label: 'Logs', icon: ScrollText, exact: false },
  { to: '/settings', label: 'Settings', icon: Settings, exact: false },
] as const;

// Root layout: nav on every page except /login. Auth gate lives here so child
// routes don't repeat it.
//
// The brand and the signed-in account are the OUTER chrome — they say which
// product you are in and who you are. A host that offers Base as a section
// already answers both on its own frame, so embedded they are dropped and the
// host's answer stands; two of each in one page is the frame admitting it is a
// separate application. The browser itself — the links, and everything under
// them — is the same either way, because it is the same admin.
function RootLayout() {
  const { isAuthenticated, record, signOut } = useAuth();

  return (
    <div className="shell" data-embedded={ embedded ? 'true' : undefined }>
      { isAuthenticated && (
        <aside className="shell__nav">
          { !embedded && (
            <div className="shell__brand">
              <img src={icon} alt="Base" />
              <span>Base</span>
            </div>
          ) }
          <nav className="shell__links">
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
          { !embedded && (
            <div className="shell__foot">
              <div className="truncate">{ String(record?.email ?? '') }</div>
              <button type="button" onClick={ signOut } className="link">
                Sign out
              </button>
            </div>
          ) }
        </aside>
      ) }
      <main className="shell__main">
        <Outlet />
      </main>
    </div>
  );
}

export const Route = createRootRoute({ component: RootLayout });
