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
        <div className="flex gap-6">
            <nav className="w-44 shrink-0">
                <h1 className="mb-3 text-xl font-semibold">Settings</h1>
                <ul className="flex flex-col gap-0.5 text-sm">
                    { navItems.map((item) => (
                        <li key={ item.to }>
                            <Link
                                to={ item.to }
                                className={
                                    'block rounded px-2 py-1 ' +
                                    (currentPath.startsWith(item.to)
                                        ? 'bg-neutral-800 text-neutral-100'
                                        : 'text-neutral-400 hover:bg-neutral-800/50 hover:text-neutral-200')
                                }
                            >
                                { item.label }
                            </Link>
                        </li>
                    )) }
                </ul>
            </nav>
            <div className="min-w-0 flex-1">
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
