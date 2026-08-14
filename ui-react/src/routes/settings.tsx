import { createFileRoute, Link, Outlet, useMatches } from '@tanstack/react-router';

import { requireSuperuser } from '~/lib/guard';

const navItems = [
    { to: '/settings/smtp', label: 'SMTP' },
    { to: '/settings/backups', label: 'Backups' },
    { to: '/settings/auth', label: 'Auth providers' },
    { to: '/settings/tokens', label: 'Token options' },
    { to: '/settings/data', label: 'Import / Export' },
    { to: '/settings/logs', label: 'Log settings' },
    { to: '/settings/rate-limits', label: 'Rate limits' },
    { to: '/settings/crons', label: 'Cron jobs' },
    { to: '/settings/application', label: 'Application' },
] as const;

function SettingsLayout() {
    const matches = useMatches();
    const currentPath = matches[matches.length - 1]?.pathname ?? '';

    return (
        <div className="settings">
            <nav className="settings__nav">
                <h1 className="page__title">Settings</h1>
                { navItems.map((item) => (
                    <Link
                        key={ item.to }
                        to={ item.to }
                        className="shell__link"
                        data-active={ currentPath.startsWith(item.to) ? 'true' : undefined }
                    >
                        { item.label }
                    </Link>
                )) }
            </nav>
            <div className="grow">
                <Outlet />
            </div>
        </div>
    );
}

// Every page under /settings is a child of this layout, so this runs before all
// of them. Restating it on a child is a second place for it to drift, which is
// what `/settings/application` and `/settings/crons` were doing.
export const Route = createFileRoute('/settings')({
    beforeLoad: requireSuperuser,
    component: SettingsLayout,
});
