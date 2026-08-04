import { createFileRoute, Link, Outlet, redirect, useMatches } from '@tanstack/react-router';

import { base } from '~/lib/base';

const navItems = [
    { to: '/settings/smtp', label: 'SMTP' },
    { to: '/settings/backups', label: 'Backups' },
    { to: '/settings/auth', label: 'Auth providers' },
    { to: '/settings/mail', label: 'Mail templates' },
    { to: '/settings/tokens', label: 'Token options' },
    { to: '/settings/data', label: 'Import / Export' },
    { to: '/settings/superusers', label: 'Superusers' },
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

export const Route = createFileRoute('/settings')({
    beforeLoad: () => {
        if (!base.authStore.isValid || !base.authStore.isSuperuser) throw redirect({ to: '/login' });
    },
    component: SettingsLayout,
});
