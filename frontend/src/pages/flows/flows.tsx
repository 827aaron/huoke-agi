import type { ColumnDef } from '@tanstack/react-table';

import { useMutation } from '@apollo/client/react';
import { Ellipsis, Eye, GitFork, Pause, Pencil, PencilLine, Plus, Star, Trash } from 'lucide-react';
import { CheckCircle2, XCircle } from 'lucide-react';
import { useCallback, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { toast } from 'sonner';

import { FlowStatusIcon } from '@/components/icons/flow-status-icon';
import {
    AppHeader,
    AppHeaderAction,
    AppHeaderActions,
    AppHeaderContent,
    AppHeaderTitle,
} from '@/components/layouts/app/app-header';
import ConfirmationDialog from '@/components/shared/confirmation-dialog';
import { ErrorState } from '@/components/shared/error-state';
import { InlineEditInput } from '@/components/shared/inline-edit';
import { LoadingState } from '@/components/shared/loading-state';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ContextMenuItem, ContextMenuSeparator } from '@/components/ui/context-menu';
import { DataTable, DataTableColumnHeader } from '@/components/ui/data-table';
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Spinner } from '@/components/ui/spinner';
import { Toggle } from '@/components/ui/toggle';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { FlowProviderLabel } from '@/features/flows/flow-provider-label';
import { RenameFlowDocument, ResultType, StatusType, type TerminalFragmentFragment } from '@/graphql/types';
import { useIsOwnFlow } from '@/hooks/use-is-own-flow';
import { useTableState } from '@/hooks/use-table-state';
import { copy, statusLabel } from '@/lib/copy';
import { routes } from '@/lib/routes';
import { mergeHrefWithSearchParams } from '@/lib/url-params';
import { formatTableDate } from '@/lib/utils/format';
import { useFavorites } from '@/providers/favorites-provider';
import { type Flow, useFlows } from '@/providers/flows-provider';
import { useProviders } from '@/providers/providers-provider';

const statusConfig: Record<
    StatusType,
    { label: string; variant: 'default' | 'destructive' | 'outline' | 'secondary' }
> = {
    [StatusType.Created]: {
        label: statusLabel('created'),
        variant: 'outline',
    },
    [StatusType.Failed]: {
        label: statusLabel('failed'),
        variant: 'outline',
    },
    [StatusType.Finished]: {
        label: statusLabel('finished'),
        variant: 'secondary',
    },
    [StatusType.Running]: {
        label: statusLabel('running'),
        variant: 'outline',
    },
    [StatusType.Waiting]: {
        label: statusLabel('waiting'),
        variant: 'outline',
    },
};

function Flows() {
    const navigate = useNavigate();
    const location = useLocation();
    const { deleteFlow, finishFlow, flows, flowsError, isLoading, refetch } = useFlows();
    const { canToggleFavorite, isFavoriteFlow, toggleFavoriteFlow } = useFavorites();
    const { providers } = useProviders();
    const isOwnFlow = useIsOwnFlow();
    const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);
    const [deletingFlow, setDeletingFlow] = useState<Flow | null>(null);
    const [finishingFlowIds, setFinishingFlowIds] = useState<Set<string>>(new Set());
    const [deletingFlowIds, setDeletingFlowIds] = useState<Set<string>>(new Set());
    const [editingFlowId, setEditingFlowId] = useState<null | string>(null);
    const editingInputRef = useRef<HTMLInputElement>(null);
    const [renameFlowMutation, { loading: isRenameLoading }] = useMutation(RenameFlowDocument);

    const { filter, pageIndex: currentPage, setFilter, setPage: handlePageChange } = useTableState();

    const handleFlowOpen = useCallback(
        (flowId: string) => {
            navigate(mergeHrefWithSearchParams(routes.flow(flowId), new URLSearchParams(location.search)));
        },
        [navigate, location.search],
    );

    const handleFlowDeleteDialogOpen = useCallback((flow: Flow) => {
        setDeletingFlow(flow);
        setIsDeleteDialogOpen(true);
    }, []);

    const handleFlowRenameStart = useCallback((flow: Flow) => {
        setEditingFlowId(flow.id);
    }, []);

    const handleFlowDelete = async () => {
        if (!deletingFlow) {
            return;
        }

        setDeletingFlowIds((previousIds) => new Set(previousIds).add(deletingFlow.id));

        try {
            const success = await deleteFlow(deletingFlow);

            if (success) {
                setDeletingFlow(null);
            }
        } finally {
            setDeletingFlowIds((previousIds) => {
                const newIds = new Set(previousIds);
                newIds.delete(deletingFlow.id);

                return newIds;
            });
        }
    };

    const handleFlowRenameSave = useCallback(async () => {
        const newTitle = editingInputRef.current?.value.trim();

        if (!editingFlowId || !newTitle) {
            return;
        }

        try {
            const { data } = await renameFlowMutation({
                variables: {
                    flowId: editingFlowId,
                    title: newTitle,
                },
            });

            if (data?.renameFlow === ResultType.Success) {
                toast.success(copy.flows.renamed);
                setEditingFlowId(null);
            }
        } catch (error) {
            const errorMessage = error instanceof Error ? error.message : copy.flows.renameFailed;
            toast.error(errorMessage);
        }
    }, [editingFlowId, renameFlowMutation]);

    const handleFlowRenameCancel = useCallback(() => {
        setEditingFlowId(null);
    }, []);

    const handleFlowFinish = useCallback(
        async (flow: Flow) => {
            setFinishingFlowIds((previousIds) => new Set(previousIds).add(flow.id));

            try {
                await finishFlow(flow);
            } finally {
                setFinishingFlowIds((previousIds) => {
                    const newIds = new Set(previousIds);
                    newIds.delete(flow.id);

                    return newIds;
                });
            }
        },
        [finishFlow],
    );

    const columns: ColumnDef<Flow>[] = useMemo(
        () => [
            {
                accessorKey: 'id',
                cell: ({ row }) => <div className="font-mono text-sm whitespace-nowrap">{row.getValue('id')}</div>,
                enableHiding: false,
                header: ({ column }) => (
                    <DataTableColumnHeader
                        column={column}
                        title="ID"
                    />
                ),
                maxSize: 110,
                meta: { columnMenuLabel: 'ID', searchable: true },
                minSize: 80,
                size: 90,
            },
            {
                accessorKey: 'title',
                cell: ({ row }) => {
                    const flow = row.original;
                    const isEditing = editingFlowId === flow.id;
                    const title = row.getValue('title') as string;

                    if (isEditing) {
                        return (
                            <div onClick={(e) => e.stopPropagation()}>
                                <InlineEditInput
                                    autoFocus
                                    busy={isRenameLoading}
                                    defaultValue={title}
                                    inputRef={editingInputRef}
                                    onCancel={handleFlowRenameCancel}
                                    onSave={handleFlowRenameSave}
                                    placeholder={copy.flows.titlePlaceholder}
                                />
                            </div>
                        );
                    }

                    return <div className="truncate font-medium">{title}</div>;
                },
                enableHiding: false,
                header: ({ column }) => (
                    <DataTableColumnHeader
                        column={column}
                        title={copy.flows.titleColumn}
                    />
                ),
                meta: { columnMenuLabel: copy.flows.titleColumn, searchable: true },
                minSize: 200,
                size: 240,
            },
            {
                accessorKey: 'status',
                cell: ({ row }) => {
                    const status = row.getValue('status') as StatusType;
                    const config = statusConfig[status];

                    return (
                        <Badge variant={config.variant}>
                            <FlowStatusIcon
                                className="size-3"
                                status={status}
                            />
                            {config.label}
                        </Badge>
                    );
                },
                header: ({ column }) => (
                    <DataTableColumnHeader
                        column={column}
                        title={copy.flows.status}
                    />
                ),
                maxSize: 130,
                meta: { columnMenuLabel: copy.flows.status, searchable: true },
                minSize: 80,
                size: 100,
            },
            {
                // accessorFn returns the provider name as a plain string so it
                // participates in the DataTable global filter (search input).
                // The cell renderer still reads the original provider object
                // directly through `row.original`, so the icon + label stay
                // intact.
                accessorFn: (row) => row.provider?.name ?? '',
                cell: ({ row }) => (
                    <FlowProviderLabel
                        isOwnFlow={isOwnFlow(row.original)}
                        provider={row.original.provider}
                        providers={providers}
                    />
                ),
                header: ({ column }) => (
                    <DataTableColumnHeader
                        column={column}
                        title={copy.flows.provider}
                    />
                ),
                id: 'provider',
                maxSize: 240,
                meta: { columnMenuLabel: copy.flows.provider, searchable: true },
                minSize: 100,
                size: 160,
                sortingFn: (rowA, rowB) => {
                    const nameA = rowA.original.provider?.name || '';
                    const nameB = rowB.original.provider?.name || '';

                    return nameA.localeCompare(nameB);
                },
            },
            {
                // accessorFn joins all terminal images into one string for the
                // global search; the cell still derives its presentation from
                // the original array on `row.original`, and sortingFn keeps
                // ordering by count (more intuitive than alphabetical).
                accessorFn: (row) => (row.terminals ?? []).map((t) => t.image).join(' '),
                cell: ({ row }) => {
                    const flow = row.original;
                    const terminals = flow.terminals || [];

                    if (terminals.length === 0) {
                        return <span className="text-muted-foreground text-sm">{copy.flows.noTerminals}</span>;
                    }

                    const isAnyConnected = terminals.some((t: TerminalFragmentFragment) => t.connected);
                    const images = [...new Set(terminals.map((t: TerminalFragmentFragment) => t.image))];

                    return (
                        <Tooltip>
                            <TooltipTrigger asChild>
                                <div className="flex items-center gap-2 overflow-hidden">
                                    {isAnyConnected ? (
                                        <CheckCircle2 className="size-4 shrink-0 text-green-500" />
                                    ) : (
                                        <XCircle className="text-muted-foreground size-4 shrink-0" />
                                    )}
                                    <span className="truncate text-sm">{images.join(', ')}</span>
                                </div>
                            </TooltipTrigger>
                            <TooltipContent>
                                <div className="flex flex-col gap-1">
                                    {terminals.map((terminal: TerminalFragmentFragment) => (
                                        <div
                                            className="flex items-center gap-2"
                                            key={terminal.id}
                                        >
                                            <span className="text-xs">{terminal.image}</span>
                                            <span className="text-muted-foreground text-xs">
                                                ({terminal.connected ? copy.flows.connected : copy.flows.disconnected})
                                            </span>
                                        </div>
                                    ))}
                                </div>
                            </TooltipContent>
                        </Tooltip>
                    );
                },
                header: ({ column }) => (
                    <DataTableColumnHeader
                        column={column}
                        title={copy.flows.terminals}
                    />
                ),
                id: 'terminals',
                maxSize: 220,
                meta: { columnMenuLabel: copy.flows.terminals, searchable: true },
                minSize: 140,
                size: 160,
                sortingFn: (rowA, rowB) => {
                    const terminalsA = rowA.original.terminals || [];
                    const terminalsB = rowB.original.terminals || [];

                    return terminalsA.length - terminalsB.length;
                },
            },
            {
                accessorKey: 'createdAt',
                cell: ({ row }) => {
                    const dateString = row.getValue('createdAt') as string;

                    return <div className="text-sm whitespace-nowrap">{formatTableDate(new Date(dateString))}</div>;
                },
                header: ({ column }) => (
                    <DataTableColumnHeader
                        column={column}
                        title={copy.providers.created}
                    />
                ),
                maxSize: 170,
                meta: { columnMenuLabel: copy.providers.created },
                minSize: 128,
                size: 136,
                sortingFn: (rowA, rowB) => {
                    const dateA = new Date(rowA.getValue('createdAt') as string);
                    const dateB = new Date(rowB.getValue('createdAt') as string);

                    return dateA.getTime() - dateB.getTime();
                },
            },
            {
                accessorKey: 'updatedAt',
                cell: ({ row }) => {
                    const dateString = row.getValue('updatedAt') as string;

                    return <div className="text-sm whitespace-nowrap">{formatTableDate(new Date(dateString))}</div>;
                },
                header: ({ column }) => (
                    <DataTableColumnHeader
                        column={column}
                        title={copy.flows.updated}
                    />
                ),
                maxSize: 170,
                meta: { columnMenuLabel: copy.flows.updated },
                minSize: 128,
                size: 136,
                sortingFn: (rowA, rowB) => {
                    const dateA = new Date(rowA.getValue('updatedAt') as string);
                    const dateB = new Date(rowB.getValue('updatedAt') as string);

                    return dateA.getTime() - dateB.getTime();
                },
            },
            {
                cell: ({ row }) => {
                    const flow = row.original;
                    const isRunning = ![StatusType.Failed, StatusType.Finished].includes(flow.status);

                    return (
                        <div className="flex items-center justify-end gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                            {canToggleFavorite(flow) && (
                                <Toggle
                                    aria-label={copy.sidebar.toggleFavorite}
                                    className="border-none data-[state=on]:bg-transparent data-[state=on]:*:[svg]:fill-yellow-500 data-[state=on]:*:[svg]:stroke-yellow-500"
                                    onClick={async (event) => {
                                        event.stopPropagation();
                                        await toggleFavoriteFlow(flow.id);
                                    }}
                                    pressed={isFavoriteFlow(flow.id)}
                                    size="sm"
                                    variant="outline"
                                >
                                    <Star />
                                </Toggle>
                            )}
                            <DropdownMenu>
                                <DropdownMenuTrigger asChild>
                                    <Button
                                        aria-label={copy.providers.openMenu}
                                        className="size-8 p-0"
                                        onClick={(e) => e.stopPropagation()}
                                        variant="ghost"
                                    >
                                        <Ellipsis />
                                    </Button>
                                </DropdownMenuTrigger>
                                <DropdownMenuContent
                                    align="end"
                                    className="min-w-24"
                                    onClick={(e) => e.stopPropagation()}
                                >
                                    <DropdownMenuItem onClick={() => handleFlowOpen(flow.id)}>
                                        <Eye />
                                        {copy.flows.view}
                                    </DropdownMenuItem>
                                    <DropdownMenuItem onClick={() => handleFlowRenameStart(flow)}>
                                        <PencilLine className="size-3" />
                                        {copy.action.rename}
                                    </DropdownMenuItem>
                                    {isRunning && (
                                        <DropdownMenuItem
                                            disabled={finishingFlowIds.has(flow.id)}
                                            onClick={() => handleFlowFinish(flow)}
                                        >
                                            {finishingFlowIds.has(flow.id) ? (
                                                <>
                                                    <Spinner variant="circle" />
                                                    {copy.flows.finishing}
                                                </>
                                            ) : (
                                                <>
                                                    <Pause />
                                                    {copy.flows.finish}
                                                </>
                                            )}
                                        </DropdownMenuItem>
                                    )}
                                    <DropdownMenuSeparator />
                                    <DropdownMenuItem
                                        disabled={deletingFlowIds.has(flow.id)}
                                        onClick={() => handleFlowDeleteDialogOpen(flow)}
                                    >
                                        {deletingFlowIds.has(flow.id) ? (
                                            <>
                                                <Spinner variant="circle" />
                                                {copy.action.deleting}
                                            </>
                                        ) : (
                                            <>
                                                <Trash />
                                                {copy.action.delete}
                                            </>
                                        )}
                                    </DropdownMenuItem>
                                </DropdownMenuContent>
                            </DropdownMenu>
                        </div>
                    );
                },
                enableHiding: false,
                header: () => null,
                id: 'actions',
                maxSize: 100,
                meta: { preventRowClick: true },
                minSize: 90,
                size: 96,
            },
        ],
        [
            canToggleFavorite,
            deletingFlowIds,
            isOwnFlow,
            providers,
            editingFlowId,
            finishingFlowIds,
            handleFlowDeleteDialogOpen,
            handleFlowFinish,
            handleFlowOpen,
            handleFlowRenameCancel,
            handleFlowRenameSave,
            handleFlowRenameStart,
            isFavoriteFlow,
            isRenameLoading,
            toggleFavoriteFlow,
        ],
    );

    const renderRowContextMenu = useCallback(
        (flow: Flow) => {
            const isRunning = ![StatusType.Failed, StatusType.Finished].includes(flow.status);

            return (
                <>
                    {canToggleFavorite(flow) && (
                        <>
                            <ContextMenuItem onClick={async () => toggleFavoriteFlow(flow.id)}>
                                <Star />
                                {isFavoriteFlow(flow.id) ? copy.flows.removeFavorite : copy.flows.addFavorite}
                            </ContextMenuItem>
                            <ContextMenuSeparator />
                        </>
                    )}
                    <ContextMenuItem onClick={() => handleFlowOpen(flow.id)}>
                        <Eye />
                        {copy.flows.view}
                    </ContextMenuItem>
                    <ContextMenuItem onClick={() => handleFlowRenameStart(flow)}>
                        <Pencil />
                        {copy.action.rename}
                    </ContextMenuItem>

                    {isRunning && (
                        <ContextMenuItem
                            disabled={finishingFlowIds.has(flow.id)}
                            onClick={() => handleFlowFinish(flow)}
                        >
                            <Pause />
                            {finishingFlowIds.has(flow.id) ? copy.flows.finishing : copy.flows.finish}
                        </ContextMenuItem>
                    )}
                    <ContextMenuSeparator />
                    <ContextMenuItem
                        disabled={deletingFlowIds.has(flow.id)}
                        onClick={() => handleFlowDeleteDialogOpen(flow)}
                    >
                        <Trash />
                        {deletingFlowIds.has(flow.id) ? copy.action.deleting : copy.action.delete}
                    </ContextMenuItem>
                </>
            );
        },
        [
            canToggleFavorite,
            deletingFlowIds,
            finishingFlowIds,
            handleFlowDeleteDialogOpen,
            handleFlowFinish,
            handleFlowOpen,
            handleFlowRenameStart,
            isFavoriteFlow,
            toggleFavoriteFlow,
        ],
    );

    const handleRowClick = useCallback(
        (flow: Flow) => {
            if (editingFlowId !== flow.id) {
                handleFlowOpen(flow.id);
            }
        },
        [editingFlowId, handleFlowOpen],
    );

    const pageHeader = (
        <AppHeader>
            <AppHeaderContent>
                <AppHeaderTitle icon={<GitFork className="size-4 shrink-0" />}>{copy.flows.title}</AppHeaderTitle>
            </AppHeaderContent>
            <AppHeaderActions>
                <AppHeaderAction
                    icon={<Plus />}
                    label={copy.flows.newFlow}
                    onClick={() => navigate(routes.newFlow)}
                    variant="secondary"
                />
            </AppHeaderActions>
        </AppHeader>
    );

    if (isLoading) {
        return (
            <>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <LoadingState
                        description={copy.flows.loadingHint}
                        title={copy.flows.loading}
                    />
                </div>
            </>
        );
    }

    // Error surface only when there's no data — a failed background refetch must not blank a working list.
    if (flowsError && flows.length === 0) {
        return (
            <>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <ErrorState
                        message={flowsError.message}
                        onRetry={refetch}
                        title={copy.flows.loadFailed}
                    />
                </div>
            </>
        );
    }

    if (flows.length === 0) {
        return (
            <>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <Empty>
                        <EmptyHeader>
                            <EmptyMedia variant="icon">
                                <GitFork />
                            </EmptyMedia>
                            <EmptyTitle>{copy.flows.empty}</EmptyTitle>
                            <EmptyDescription>{copy.flows.emptyHint}</EmptyDescription>
                        </EmptyHeader>
                        <EmptyContent>
                            <Button
                                onClick={() => navigate(routes.newFlow)}
                                variant="secondary"
                            >
                                <Plus />
                                {copy.flows.newFlow}
                            </Button>
                        </EmptyContent>
                    </Empty>
                </div>
            </>
        );
    }

    return (
        <>
            {pageHeader}
            <div className="flex flex-col gap-4 p-4 pt-0">
                <DataTable<Flow>
                    columns={columns}
                    data={flows}
                    empty={{ entityName: copy.flows.title }}
                    filterPlaceholder={copy.flows.filter}
                    filterValue={filter}
                    isVirtualized
                    label={copy.flows.title}
                    onFilterChange={setFilter}
                    onPageChange={handlePageChange}
                    onRowClick={handleRowClick}
                    pageIndex={currentPage}
                    renderRowContextMenu={renderRowContextMenu}
                />

                <ConfirmationDialog
                    cancelText={copy.action.cancel}
                    confirmText={copy.action.delete}
                    handleConfirm={handleFlowDelete}
                    handleOpenChange={setIsDeleteDialogOpen}
                    isOpen={isDeleteDialogOpen}
                    itemName={deletingFlow?.title}
                    itemType={copy.itemType.flow}
                />
            </div>
        </>
    );
}

export default Flows;
