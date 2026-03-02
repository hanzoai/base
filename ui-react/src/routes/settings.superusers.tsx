import { createFileRoute } from '@tanstack/react-router';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useForm } from 'react-hook-form';
import { useState } from 'react';

import { base } from '~/lib/base';
import { SectionCard } from '~/components/SectionCard';

interface CreateSuperuserForm {
    email: string;
    password: string;
    passwordConfirm: string;
}

interface ChangePasswordForm {
    password: string;
    passwordConfirm: string;
}

const inputClass = 'rounded border border-neutral-700 bg-neutral-900 px-2 py-1.5 text-sm';
const btnPrimary = 'rounded bg-indigo-600 px-4 py-1.5 text-sm font-medium hover:bg-indigo-500 disabled:opacity-50';

function SuperusersSettings() {
    const qc = useQueryClient();
    const [changingPw, setChangingPw] = useState<string | null>(null);

    const superusers = useQuery({
        queryKey: ['superusers'],
        queryFn: () => base.collection('_superusers').getFullList({ sort: 'email' }),
    });

    const { register: regCreate, handleSubmit: submitCreate, formState: fsCreate, reset: resetCreate } = useForm<CreateSuperuserForm>();

    const createMutation = useMutation({
        mutationFn: (data: CreateSuperuserForm) =>
            base.collection('_superusers').create({
                email: data.email,
                password: data.password,
                passwordConfirm: data.passwordConfirm,
            }),
        onSuccess: () => {
            resetCreate();
            void qc.invalidateQueries({ queryKey: ['superusers'] });
        },
    });

    const deleteMutation = useMutation({
        mutationFn: (id: string) => base.collection('_superusers').delete(id),
        onSuccess: () => { void qc.invalidateQueries({ queryKey: ['superusers'] }); },
    });

    return (
        <div className="flex flex-col gap-6">
            <SectionCard title="Superusers" description="Manage superuser accounts.">
                { superusers.isPending && <div className="text-sm text-neutral-400">Loading...</div> }
                { superusers.error && <div className="text-sm text-red-400">{ String(superusers.error) }</div> }

                { superusers.data && (
                    <table className="mb-4 w-full text-sm">
                        <thead className="text-left text-xs uppercase tracking-wider text-neutral-500">
                            <tr>
                                <th className="py-2">Email</th>
                                <th className="py-2">ID</th>
                                <th className="py-2">Created</th>
                                <th className="py-2 text-right">Actions</th>
                            </tr>
                        </thead>
                        <tbody>
                            { superusers.data.map((su) => (
                                <tr key={ su.id } className="border-t border-neutral-800">
                                    <td className="py-2 font-medium">{ su.email }</td>
                                    <td className="py-2 text-xs text-neutral-500">{ su.id }</td>
                                    <td className="py-2 text-xs text-neutral-500">{ su.created }</td>
                                    <td className="py-2 text-right">
                                        <div className="flex items-center justify-end gap-2">
                                            <button
                                                onClick={ () => setChangingPw(changingPw === su.id ? null : su.id) }
                                                className="text-xs text-indigo-400 hover:text-indigo-300"
                                            >
                                                { changingPw === su.id ? 'Cancel' : 'Change password' }
                                            </button>
                                            <button
                                                onClick={ () => {
                                                    if (confirm(`Delete superuser "${su.email}"?`)) deleteMutation.mutate(su.id);
                                                } }
                                                disabled={ deleteMutation.isPending }
                                                className="text-xs text-red-400 hover:text-red-300 disabled:text-neutral-600"
                                            >
                                                Delete
                                            </button>
                                        </div>
                                        { changingPw === su.id && (
                                            <ChangePasswordPanel
                                                superuserId={ su.id }
                                                onDone={ () => setChangingPw(null) }
                                            />
                                        ) }
                                    </td>
                                </tr>
                            )) }
                        </tbody>
                    </table>
                ) }

                { deleteMutation.error && <div className="mb-2 text-xs text-red-400">{ deleteMutation.error.message }</div> }
            </SectionCard>

            <SectionCard title="Create superuser" description="Add a new superuser account.">
                <form onSubmit={ submitCreate((d) => createMutation.mutate(d)) } className="flex flex-col gap-3 max-w-md">
                    <label className="flex flex-col gap-1 text-sm">
                        <span className="text-neutral-400">Email</span>
                        <input { ...regCreate('email', { required: true }) } type="email" className={ inputClass } />
                    </label>
                    <label className="flex flex-col gap-1 text-sm">
                        <span className="text-neutral-400">Password</span>
                        <input { ...regCreate('password', { required: true, minLength: 10 }) } type="password" autoComplete="new-password" className={ inputClass } />
                    </label>
                    <label className="flex flex-col gap-1 text-sm">
                        <span className="text-neutral-400">Confirm password</span>
                        <input { ...regCreate('passwordConfirm', { required: true, minLength: 10 }) } type="password" autoComplete="new-password" className={ inputClass } />
                    </label>
                    <div className="flex items-center gap-2 pt-1">
                        <button type="submit" disabled={ !fsCreate.isDirty || createMutation.isPending } className={ btnPrimary }>
                            { createMutation.isPending ? 'Creating...' : 'Create superuser' }
                        </button>
                        { createMutation.isSuccess && <span className="text-xs text-green-400">Created.</span> }
                        { createMutation.error && <span className="text-xs text-red-400">{ createMutation.error.message }</span> }
                    </div>
                </form>
            </SectionCard>
        </div>
    );
}

function ChangePasswordPanel({ superuserId, onDone }: { superuserId: string; onDone: () => void }) {
    const { register, handleSubmit, formState } = useForm<ChangePasswordForm>();

    const mutation = useMutation({
        mutationFn: (data: ChangePasswordForm) =>
            base.collection('_superusers').update(superuserId, {
                password: data.password,
                passwordConfirm: data.passwordConfirm,
            }),
        onSuccess: onDone,
    });

    return (
        <form
            onSubmit={ handleSubmit((d) => mutation.mutate(d)) }
            className="mt-2 flex flex-col gap-2 rounded border border-neutral-700 bg-neutral-900 p-3 text-left"
        >
            <label className="flex flex-col gap-1 text-sm">
                <span className="text-neutral-400">New password</span>
                <input { ...register('password', { required: true, minLength: 10 }) } type="password" autoComplete="new-password" className={ inputClass } />
            </label>
            <label className="flex flex-col gap-1 text-sm">
                <span className="text-neutral-400">Confirm</span>
                <input { ...register('passwordConfirm', { required: true, minLength: 10 }) } type="password" autoComplete="new-password" className={ inputClass } />
            </label>
            <div className="flex items-center gap-2">
                <button type="submit" disabled={ !formState.isDirty || mutation.isPending } className={ btnPrimary }>
                    { mutation.isPending ? 'Saving...' : 'Update password' }
                </button>
                { mutation.error && <span className="text-xs text-red-400">{ mutation.error.message }</span> }
            </div>
        </form>
    );
}

export const Route = createFileRoute('/settings/superusers')({
    component: SuperusersSettings,
});
