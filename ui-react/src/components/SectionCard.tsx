import type { ReactNode } from 'react';

interface SectionCardProps {
    title: string;
    description?: string;
    children: ReactNode;
}

export function SectionCard({ title, description, children }: SectionCardProps) {
    return (
        <section className="card">
            <h2 className="card__title">{ title }</h2>
            { description && <p className="card__desc">{ description }</p> }
            <div className="card__body">{ children }</div>
        </section>
    );
}
