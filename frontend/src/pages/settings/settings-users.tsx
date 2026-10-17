import { Plus, Users } from 'lucide-react';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';

import { AppHeader, AppHeaderContent, AppHeaderTitle } from '@/components/layouts/app/app-header';
import { Badge, type BadgeVariant } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { InputPassword } from '@/components/ui/input-password';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Spinner } from '@/components/ui/spinner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { newPasswordSchema } from '@/features/authentication/password-change-form';
import { type ManagedUser, ROLE_ADMIN, ROLE_USER, usersApi } from '@/features/users/users-api';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';
import { useUser } from '@/providers/user-provider';

const text = copy.users;

const EMAIL = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

const statusVariant: Record<string, BadgeVariant> = { active: 'green', blocked: 'secondary', created: 'yellow' };

const passwordError = (password: string): null | string => {
    const result = newPasswordSchema.safeParse(password);

    return result.success ? null : (result.error.issues[0]?.message ?? copy.auth.passwordPolicy);
};

function CreateUserDialog({ onClose, onCreated }: { onClose: () => void; onCreated: (user: ManagedUser) => void }) {
    const [mail, setMail] = useState('');
    const [name, setName] = useState('');
    const [password, setPassword] = useState('');
    const [roleId, setRoleId] = useState(ROLE_USER);
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [isSaving, setIsSaving] = useState(false);

    const handleCreate = async () => {
        const nextErrors: Record<string, string> = {};
        const invalidPassword = passwordError(password);

        if (!EMAIL.test(mail.trim())) {
            nextErrors.mail = text.mailInvalid;
        }

        if (invalidPassword) {
            nextErrors.password = invalidPassword;
        }

        setErrors(nextErrors);

        if (Object.keys(nextErrors).length) {
            return;
        }

        setIsSaving(true);

        try {
            const created = await usersApi.create({ mail: mail.trim(), name: name.trim(), password, role_id: roleId });
            toast.success(text.created(created.mail));
            onCreated(created);
        } catch (error) {
            setErrors({ form: getApiErrorMessage(error, copy.common.saveFailed) });
        } finally {
            setIsSaving(false);
        }
    };

    return (
        <Dialog
            onOpenChange={(isOpen) => !isOpen && onClose()}
            open
        >
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>{text.add}</DialogTitle>
                    <DialogDescription>{text.description}</DialogDescription>
                </DialogHeader>
                <div className="flex flex-col gap-3">
                    <div className="flex flex-col gap-1.5">
                        <Label htmlFor="user-mail">{text.field.mail}</Label>
                        <Input
                            aria-invalid={!!errors.mail}
                            autoComplete="off"
                            id="user-mail"
                            onChange={(event) => setMail(event.target.value)}
                            value={mail}
                        />
                        {errors.mail && <p className="text-destructive text-xs">{errors.mail}</p>}
                    </div>
                    <div className="flex flex-col gap-1.5">
                        <Label htmlFor="user-name">{text.field.name}</Label>
                        <Input
                            autoComplete="off"
                            id="user-name"
                            onChange={(event) => setName(event.target.value)}
                            value={name}
                        />
                    </div>
                    <div className="flex flex-col gap-1.5">
                        <Label htmlFor="user-password">{text.field.password}</Label>
                        <InputPassword
                            aria-invalid={!!errors.password}
                            autoComplete="new-password"
                            id="user-password"
                            onChange={(event) => setPassword(event.target.value)}
                            value={password}
                        />
                        <p className="text-muted-foreground text-xs">{text.passwordHint}</p>
                        {errors.password && <p className="text-destructive text-xs">{errors.password}</p>}
                    </div>
                    <div className="flex flex-col gap-1.5">
                        <Label htmlFor="user-role">{text.field.role}</Label>
                        <Select
                            onValueChange={(value) => setRoleId(Number(value))}
                            value={String(roleId)}
                        >
                            <SelectTrigger
                                className="w-full"
                                id="user-role"
                            >
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {[ROLE_USER, ROLE_ADMIN].map((role) => (
                                    <SelectItem
                                        key={role}
                                        value={String(role)}
                                    >
                                        {text.role[role]}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                    {errors.form && <p className="text-destructive text-sm">{errors.form}</p>}
                </div>
                <DialogFooter>
                    <Button
                        disabled={isSaving}
                        onClick={onClose}
                        variant="ghost"
                    >
                        {text.cancel}
                    </Button>
                    <Button
                        disabled={isSaving}
                        onClick={handleCreate}
                    >
                        {isSaving && <Spinner variant="circle" />}
                        {text.create}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

function ResetPasswordDialog({ onClose, user }: { onClose: () => void; user: ManagedUser }) {
    const [password, setPassword] = useState('');
    const [error, setError] = useState<null | string>(null);
    const [isSaving, setIsSaving] = useState(false);

    const handleSave = async () => {
        const invalid = passwordError(password);

        setError(invalid);

        if (invalid) {
            return;
        }

        setIsSaving(true);

        try {
            await usersApi.resetPassword(user, password);
            toast.success(text.resetPasswordDone(user.mail));
            onClose();
        } catch (saveError) {
            setError(getApiErrorMessage(saveError, copy.common.saveFailed));
        } finally {
            setIsSaving(false);
        }
    };

    return (
        <Dialog
            onOpenChange={(isOpen) => !isOpen && onClose()}
            open
        >
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>{text.resetPasswordTitle(user.mail)}</DialogTitle>
                    <DialogDescription>{text.passwordHint}</DialogDescription>
                </DialogHeader>
                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="user-new-password">{text.newPassword}</Label>
                    <InputPassword
                        aria-invalid={!!error}
                        autoComplete="new-password"
                        id="user-new-password"
                        onChange={(event) => setPassword(event.target.value)}
                        value={password}
                    />
                    {error && <p className="text-destructive text-xs">{error}</p>}
                </div>
                <DialogFooter>
                    <Button
                        disabled={isSaving}
                        onClick={onClose}
                        variant="ghost"
                    >
                        {text.cancel}
                    </Button>
                    <Button
                        disabled={isSaving}
                        onClick={handleSave}
                    >
                        {isSaving && <Spinner variant="circle" />}
                        {text.save}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

function SettingsUsers() {
    const { authInfo } = useUser();
    const canView = !!authInfo?.privileges?.includes('users.view');
    const canCreate = !!authInfo?.privileges?.includes('users.create');
    const canEdit = !!authInfo?.privileges?.includes('users.edit');
    const canDelete = !!authInfo?.privileges?.includes('users.delete');

    const [users, setUsers] = useState<ManagedUser[] | null>(null);
    const [loadError, setLoadError] = useState(false);
    const [isCreating, setIsCreating] = useState(false);
    const [resetting, setResetting] = useState<ManagedUser | null>(null);
    const [busyHash, setBusyHash] = useState<null | string>(null);

    useEffect(() => {
        if (!canView) {
            return;
        }

        let isCancelled = false;

        usersApi
            .list()
            .then((list) => !isCancelled && setUsers(list))
            .catch(() => !isCancelled && setLoadError(true));

        return () => {
            isCancelled = true;
        };
    }, [canView]);

    const act = async (user: ManagedUser, action: () => Promise<ManagedUser | void>, done: string) => {
        setBusyHash(user.hash);

        try {
            const next = await action();
            setUsers((previous) =>
                (previous ?? []).flatMap((item) => (item.hash !== user.hash ? [item] : next ? [next] : [])),
            );
            toast.success(done);
        } catch (error) {
            toast.error(getApiErrorMessage(error, copy.common.saveFailed));
        } finally {
            setBusyHash(null);
        }
    };

    const renderBody = () => {
        if (!canView) {
            return <p className="text-muted-foreground text-sm">{text.noPermission}</p>;
        }

        if (loadError) {
            return <p className="text-destructive text-sm">{text.loadFailed}</p>;
        }

        if (!users) {
            return (
                <div className="flex justify-center py-8">
                    <Spinner variant="circle" />
                </div>
            );
        }

        return (
            <div className="rounded-md border">
                <Table aria-label={text.title}>
                    <TableHeader>
                        <TableRow>
                            <TableHead>{text.column.mail}</TableHead>
                            <TableHead>{text.column.name}</TableHead>
                            <TableHead>{text.column.role}</TableHead>
                            <TableHead>{text.column.status}</TableHead>
                            <TableHead>{text.column.created}</TableHead>
                            <TableHead className="text-right">{text.column.actions}</TableHead>
                        </TableRow>
                    </TableHeader>
                    <TableBody>
                        {users.map((user) => {
                            const isSelf = user.hash === authInfo?.user?.hash;
                            const isBusy = busyHash === user.hash;
                            const isBlocked = user.status === 'blocked';

                            return (
                                <TableRow key={user.hash}>
                                    <TableCell className="whitespace-nowrap">
                                        {user.mail}
                                        {isSelf && <span className="text-muted-foreground">{text.self}</span>}
                                    </TableCell>
                                    <TableCell>{user.name || copy.common.dash}</TableCell>
                                    <TableCell className="whitespace-nowrap">
                                        {text.role[user.role_id] ?? user.role?.name}
                                    </TableCell>
                                    <TableCell>
                                        <Badge variant={statusVariant[user.status] ?? 'secondary'}>
                                            {text.status[user.status] ?? user.status}
                                        </Badge>
                                    </TableCell>
                                    <TableCell className="text-muted-foreground whitespace-nowrap">
                                        {copy.common.date(new Date(user.created_at))}
                                    </TableCell>
                                    <TableCell>
                                        {!isSelf && (
                                            <div className="flex justify-end gap-1">
                                                {canEdit && user.type === 'local' && (
                                                    <Button
                                                        disabled={isBusy}
                                                        onClick={() => setResetting(user)}
                                                        size="sm"
                                                        variant="ghost"
                                                    >
                                                        {text.resetPassword}
                                                    </Button>
                                                )}
                                                {canEdit && (
                                                    <Button
                                                        disabled={isBusy}
                                                        onClick={() =>
                                                            act(
                                                                user,
                                                                () => usersApi.setBlocked(user, !isBlocked),
                                                                isBlocked
                                                                    ? text.enabled(user.mail)
                                                                    : text.disabled(user.mail),
                                                            )
                                                        }
                                                        size="sm"
                                                        variant="ghost"
                                                    >
                                                        {isBlocked ? text.enable : text.disable}
                                                    </Button>
                                                )}
                                                {canDelete && (
                                                    <Button
                                                        className="text-destructive"
                                                        disabled={isBusy}
                                                        onClick={() =>
                                                            act(
                                                                user,
                                                                () => usersApi.delete(user),
                                                                text.deleted(user.mail),
                                                            )
                                                        }
                                                        size="sm"
                                                        variant="ghost"
                                                    >
                                                        {text.delete}
                                                    </Button>
                                                )}
                                            </div>
                                        )}
                                    </TableCell>
                                </TableRow>
                            );
                        })}
                    </TableBody>
                </Table>
                {users.length <= 1 && <p className="text-muted-foreground p-4 text-sm">{text.empty}</p>}
            </div>
        );
    };

    return (
        <>
            <AppHeader>
                <AppHeaderContent>
                    <AppHeaderTitle icon={<Users className="size-4 shrink-0" />}>{text.title}</AppHeaderTitle>
                </AppHeaderContent>
            </AppHeader>
            <div className="mx-auto flex w-full max-w-4xl flex-col gap-4 p-4">
                <div className="flex flex-wrap items-center justify-between gap-2">
                    <p className="text-muted-foreground text-sm">{text.description}</p>
                    {canView && canCreate && (
                        <Button
                            onClick={() => setIsCreating(true)}
                            size="sm"
                        >
                            <Plus />
                            {text.add}
                        </Button>
                    )}
                </div>
                {renderBody()}
            </div>
            {isCreating && (
                <CreateUserDialog
                    onClose={() => setIsCreating(false)}
                    onCreated={(created) => {
                        setUsers((previous) => [...(previous ?? []), created]);
                        setIsCreating(false);
                    }}
                />
            )}
            {resetting && (
                <ResetPasswordDialog
                    onClose={() => setResetting(null)}
                    user={resetting}
                />
            )}
        </>
    );
}

export default SettingsUsers;
