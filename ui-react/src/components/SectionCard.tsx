import type { ReactNode } from 'react';

interface SectionCardProps {
    title: string;
    description?: string;
    children: ReactNode;
}

export function SectionCard({ title, description, children }: SectionCardProps) {
    return (
        <section className="rounded-lg border border-neutral-800 bg-neutral-900/50 p-5">
            <h2 className="mb-1 text-sm font-semibold uppercase tracking-wider text-neutral-300">
                { title }
            </h2>
            { description && (
                <p className="mb-4 text-sm text-neutral-500">{ description }</p>
            ) }
            { !description && <div className="mb-4" /> }
            { children }
        </section>
    );
}
