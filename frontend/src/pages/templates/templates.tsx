import type { ColumnDef } from '@tanstack/react-table';

import { Ellipsis, FileText, Pencil, PencilLine, Plus, Trash } from 'lucide-react';
import { useCallback, useRef, useState } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import { toast } from 'sonner';

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
import { useTableState } from '@/hooks/use-table-state';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';
import { mergeHrefWithSearchParams } from '@/lib/url-params';
import { type Template, useTemplates } from '@/providers/templates-provider';

function Templates() {
    const navigate = useNavigate();
    const location = useLocation();
    const { deleteTemplate, error, isLoading, refetch, templates, updateTemplate } = useTemplates();
    const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);
    const [deletingTemplate, setDeletingTemplate] = useState<null | Template>(null);
    const [deletingIds, setDeletingIds] = useState<Set<string>>(new Set());
    const [editingTemplateId, setEditingTemplateId] = useState<null | string>(null);
    const [isRenameLoading, setIsRenameLoading] = useState(false);
    const editingInputRef = useRef<HTMLInputElement>(null);

    const { filter, setFilter } = useTableState();

    const handleTemplateOpen = useCallback(
        (templateId: string) => {
            navigate(mergeHrefWithSearchParams(routes.template(templateId), new URLSearchParams(location.search)));
        },
        [navigate, location.search],
    );

    const handleDeleteDialogOpen = useCallback((template: Template) => {
        setDeletingTemplate(template);
        setIsDeleteDialogOpen(true);
    }, []);

    const handleTemplateRenameStart = useCallback((template: Template) => {
        setEditingTemplateId(template.id);
    }, []);

    const handleTemplateRenameCancel = useCallback(() => {
        setEditingTemplateId(null);
    }, []);

    const handleTemplateRenameSave = useCallback(async () => {
        const newTitle = editingInputRef.current?.value.trim();

        if (!editingTemplateId || !newTitle) {
            return;
        }

        const template = templates.find((t) => t.id === editingTemplateId);

        if (!template) {
            return;
        }

        if (newTitle === template.title) {
            setEditingTemplateId(null);

            return;
        }

        setIsRenameLoading(true);

        try {
            await updateTemplate(editingTemplateId, { text: template.text, title: newTitle });
            toast.success(copy.templatePage.renamed);
            setEditingTemplateId(null);
        } catch {
            // Error already handled in provider with toast
        } finally {
            setIsRenameLoading(false);
        }
    }, [editingTemplateId, templates, updateTemplate]);

    const handleDelete = async () => {
        if (!deletingTemplate) {
            return;
        }

        setDeletingIds((prev) => new Set(prev).add(deletingTemplate.id));

        try {
            await deleteTemplate(deletingTemplate.id);
            setDeletingTemplate(null);
        } catch {
            // Error already handled in provider with toast
        } finally {
            setDeletingIds((prev) => {
                const next = new Set(prev);
                next.delete(deletingTemplate.id);

                return next;
            });
        }
    };

    const columns: ColumnDef<Template>[] = [
        {
            accessorKey: 'title',
            cell: ({ row }) => {
                const template = row.original;
                const isEditing = editingTemplateId === template.id;
                const title = row.getValue('title') as string;

                if (isEditing) {
                    return (
                        <div onClick={(e) => e.stopPropagation()}>
                            <InlineEditInput
                                autoFocus
                                busy={isRenameLoading}
                                defaultValue={title}
                                inputRef={editingInputRef}
                                onCancel={handleTemplateRenameCancel}
                                onSave={handleTemplateRenameSave}
                                placeholder={copy.templatePage.renamePlaceholder}
                            />
                        </div>
                    );
                }

                return <div className="max-w-[380px] truncate font-medium">{title}</div>;
            },
            header: ({ column }) => (
                <DataTableColumnHeader
                    column={column}
                    title={copy.templatePage.title}
                />
            ),
            meta: { searchable: true },
        },
        {
            accessorKey: 'text',
            cell: ({ row }) => {
                const text = (row.getValue('text') as string) ?? '';

                return <div className="text-muted-foreground max-w-[380px] truncate text-sm">{text}</div>;
            },
            header: ({ column }) => (
                <DataTableColumnHeader
                    column={column}
                    title={copy.templatePage.text}
                />
            ),
            meta: { searchable: true },
        },
        {
            cell: ({ row }) => {
                const template = row.original;

                return (
                    <div className="flex items-center justify-end gap-1 opacity-0 transition-opacity group-hover:opacity-100">
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
                                <DropdownMenuItem onClick={() => handleTemplateOpen(template.id)}>
                                    <Pencil />
                                    {copy.action.edit}
                                </DropdownMenuItem>
                                <DropdownMenuItem onClick={() => handleTemplateRenameStart(template)}>
                                    <Pencil className="size-3" />
                                    {copy.action.rename}
                                </DropdownMenuItem>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem
                                    disabled={deletingIds.has(template.id)}
                                    onClick={() => handleDeleteDialogOpen(template)}
                                >
                                    {deletingIds.has(template.id) ? (
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
            meta: { preventRowClick: true },
            size: 48,
        },
    ];

    const renderRowContextMenu = (template: Template) => (
        <>
            <ContextMenuItem onClick={() => handleTemplateOpen(template.id)}>
                <Pencil />
                {copy.action.edit}
            </ContextMenuItem>
            <ContextMenuItem onClick={() => handleTemplateRenameStart(template)}>
                <PencilLine />
                {copy.action.rename}
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem
                disabled={deletingIds.has(template.id)}
                onClick={() => handleDeleteDialogOpen(template)}
            >
                <Trash />
                {deletingIds.has(template.id) ? copy.action.deleting : copy.action.delete}
            </ContextMenuItem>
        </>
    );

    const pageHeader = (
        <AppHeader>
            <AppHeaderContent>
                <AppHeaderTitle icon={<FileText className="size-4 shrink-0" />}>
                    {copy.templatePage.titles}
                </AppHeaderTitle>
            </AppHeaderContent>
            <AppHeaderActions>
                <AppHeaderAction
                    icon={<Plus />}
                    label={copy.templatePage.newTemplate}
                    onClick={() => navigate(routes.newTemplate)}
                    variant="secondary"
                />
            </AppHeaderActions>
        </AppHeader>
    );

    if (isLoading && !templates.length) {
        return (
            <>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <LoadingState
                        description={copy.templatePage.loadingHint}
                        title={copy.templatePage.loading}
                    />
                </div>
            </>
        );
    }

    // Error surface only when there's no data — a failed background refetch must not blank a working list.
    if (error && !templates.length) {
        return (
            <>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <ErrorState
                        message={error.message}
                        onRetry={refetch}
                        title={copy.templatePage.loadListFailed}
                    />
                </div>
            </>
        );
    }

    if (!templates.length) {
        return (
            <>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <Empty>
                        <EmptyHeader>
                            <EmptyMedia variant="icon">
                                <FileText />
                            </EmptyMedia>
                            <EmptyTitle>{copy.templatePage.empty}</EmptyTitle>
                            <EmptyDescription>{copy.templatePage.emptyHint}</EmptyDescription>
                        </EmptyHeader>
                        <EmptyContent>
                            <Button
                                onClick={() => navigate(routes.newTemplate)}
                                variant="secondary"
                            >
                                <Plus />
                                {copy.templatePage.newTemplate}
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
                <DataTable
                    columns={columns}
                    data={templates}
                    empty={{ entityName: 'templates' }}
                    filterPlaceholder={copy.templatePage.filter}
                    filterValue={filter}
                    label={copy.templatePage.titles}
                    onFilterChange={setFilter}
                    onRowClick={(template) => {
                        if (editingTemplateId !== template.id) {
                            handleTemplateOpen(template.id);
                        }
                    }}
                    renderRowContextMenu={renderRowContextMenu}
                />

                <ConfirmationDialog
                    cancelText={copy.action.cancel}
                    confirmText={copy.action.delete}
                    handleConfirm={handleDelete}
                    handleOpenChange={setIsDeleteDialogOpen}
                    isOpen={isDeleteDialogOpen}
                    itemName={deletingTemplate?.title}
                    itemType={copy.itemType.template}
                />
            </div>
        </>
    );
}

export default Templates;
