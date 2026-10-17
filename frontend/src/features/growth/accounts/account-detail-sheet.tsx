import { ExternalLink } from 'lucide-react';
import { useEffect, useState } from 'react';

import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet';
import { Spinner } from '@/components/ui/spinner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';

import type { GrowthAccount, GrowthAccountDetail } from '../growth-types';

import { accountLabel } from '../account-health';
import { growthApi } from '../growth-api';

const text = copy.telegram.detail;

const when = (value?: string): string => (value ? copy.common.relative(new Date(value)) : copy.common.dash);

interface AccountDetailSheetProps {
    account: GrowthAccount | null;
    onOpenChange: (isOpen: boolean) => void;
}

// Keyed by account so each account starts from its own empty state.
function AccountDetailBody({ accountId }: { accountId: number }) {
    const [detail, setDetail] = useState<GrowthAccountDetail | null>(null);
    const [error, setError] = useState<null | string>(null);

    useEffect(() => {
        let isCurrent = true;

        growthApi
            .getAccountDetail(accountId)
            .then((loaded) => isCurrent && setDetail(loaded))
            .catch((loadError) => isCurrent && setError(getApiErrorMessage(loadError, text.loadFailed)));

        return () => {
            isCurrent = false;
        };
    }, [accountId]);

    return (
        <div className="flex flex-col gap-4 px-4 pb-6">
            {error ? (
                <p className="text-destructive text-sm">{error}</p>
            ) : !detail ? (
                <div className="flex justify-center py-8">
                    <Spinner variant="circle" />
                </div>
            ) : (
                <>
                    <div className="flex flex-col gap-1 text-sm">
                        <p>{text.joinedGroups(detail.joined_groups_count)}</p>
                        <p className="text-muted-foreground">
                            {detail.dialogs_count === undefined
                                ? text.dialogsUnknown
                                : text.dialogs(detail.dialogs_count)}
                        </p>
                    </div>

                    {detail.groups.length ? (
                        <Table aria-label={text.column.title}>
                            <TableHeader>
                                <TableRow>
                                    <TableHead>{text.column.title}</TableHead>
                                    <TableHead className="text-right">{text.column.members}</TableHead>
                                    <TableHead>{text.column.joinedAt}</TableHead>
                                    <TableHead>{text.column.lastRead}</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {detail.groups.map((group) => (
                                    <TableRow key={group.source_id}>
                                        <TableCell className="whitespace-normal">
                                            {group.link ? (
                                                <a
                                                    className="inline-flex items-center gap-1 hover:underline"
                                                    href={group.link}
                                                    rel="noreferrer"
                                                    target="_blank"
                                                >
                                                    {group.title || group.link}
                                                    <ExternalLink className="size-3 shrink-0" />
                                                </a>
                                            ) : (
                                                group.title
                                            )}
                                        </TableCell>
                                        <TableCell className="text-right tabular-nums">
                                            {group.member_count || copy.common.dash}
                                        </TableCell>
                                        <TableCell className="whitespace-nowrap">{when(group.joined_at)}</TableCell>
                                        <TableCell className="whitespace-nowrap">
                                            {when(group.last_synced_at)}
                                        </TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    ) : (
                        <p className="text-muted-foreground text-sm">{text.empty}</p>
                    )}
                </>
            )}
        </div>
    );
}

function AccountDetailSheet({ account, onOpenChange }: AccountDetailSheetProps) {
    return (
        <Sheet
            onOpenChange={onOpenChange}
            open={!!account}
        >
            <SheetContent className="flex w-full flex-col gap-0 overflow-y-auto sm:max-w-xl">
                {account && (
                    <>
                        <SheetHeader>
                            <SheetTitle>{accountLabel(account)}</SheetTitle>
                            <SheetDescription>
                                {copy.telegram.health.platform[account.platform] ?? account.platform}
                            </SheetDescription>
                        </SheetHeader>
                        <AccountDetailBody
                            accountId={account.id}
                            key={account.id}
                        />
                    </>
                )}
            </SheetContent>
        </Sheet>
    );
}

export default AccountDetailSheet;
