import { isToday, isYesterday } from 'date-fns';
import { AlertCircle, ChevronRight, ListFilter, Plus, Search, UserSearch, X } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { toast } from 'sonner';

import ConfirmationDialog from '@/components/shared/confirmation-dialog';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import {
    DropdownMenu,
    DropdownMenuCheckboxItem,
    DropdownMenuContent,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia } from '@/components/ui/empty';
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Spinner } from '@/components/ui/spinner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';
import { useFlow } from '@/providers/flow-provider';

import type { GrowthLead, GrowthLeadTier } from '../growth-types';

import { growthApi } from '../growth-api';
import { useGrowthFlow } from '../growth-flow-provider';
import { PlatformFilter } from '../platform-filter';
import { LEAD_SOURCE_PARAM, LEAD_TIER_PARAM, useGrowthTabLink } from '../use-growth-tab-link';
import LeadDetailSheet from './lead-detail-sheet';
import LeadFormDialog from './lead-form-dialog';
import LeadScoreBadge from './lead-score-badge';
import LeadsEmailButton from './leads-email-button';
import LeadsExportMenu from './leads-export-menu';
import {
    EMPTY_LEAD_FILTERS,
    filterLeads,
    groupLeadsByDay,
    type LeadContactFilter,
    type LeadDayGroup,
    type LeadDeliveryFilter,
    type LeadFilters,
    type LeadFlag,
    type LeadFoundWithin,
    type LeadSort,
    sortLeads,
} from './leads-filter';

const ALL = 'all';
const FLAGS: LeadFlag[] = ['pain', 'paying', 'recent'];
const MIN_SCORES = [5, 7, 9];

const dayTitle = (group: LeadDayGroup): string => {
    const date = copy.common.date(group.day);
    const relative = isToday(group.day)
        ? copy.leads.group.today
        : isYesterday(group.day)
          ? copy.leads.group.yesterday
          : null;

    return [date, relative, group.rounds.length ? copy.leads.group.rounds(group.rounds) : null]
        .filter(Boolean)
        .join(' · ');
};

const readFilters = (params: URLSearchParams): LeadFilters => {
    const tier = params.get(LEAD_TIER_PARAM);
    const sourceId = Number(params.get(LEAD_SOURCE_PARAM));

    return {
        ...EMPTY_LEAD_FILTERS,
        sourceId: Number.isInteger(sourceId) && sourceId > 0 ? sourceId : null,
        tier: tier === 'qualified' || tier === 'watch' ? tier : 'all',
    };
};

interface LeadsTableProps {
    leads: GrowthLead[];
    onOpenLead: (lead: GrowthLead) => void;
    onOpenSource: (title: string) => void;
}

function FlowLeads() {
    const { isLeadsLoading, leads, leadsError, refetchLeads, sources } = useGrowthFlow();
    const { flowId } = useFlow();
    const { openSources } = useGrowthTabLink();
    const [searchParams] = useSearchParams();

    const tierParam = searchParams.get(LEAD_TIER_PARAM);
    const sourceParam = searchParams.get(LEAD_SOURCE_PARAM);

    const [filters, setFilters] = useState<LeadFilters>(() => readFilters(searchParams));
    const [selectedLead, setSelectedLead] = useState<GrowthLead | null>(null);
    // `null` adds a lead by hand; undefined keeps the form closed.
    const [formLead, setFormLead] = useState<GrowthLead | null | undefined>(undefined);
    const [pendingDelete, setPendingDelete] = useState<GrowthLead | null>(null);
    // Only days the owner toggled; the rest follow the default of the newest day open.
    const [openDays, setOpenDays] = useState<Record<string, boolean>>({});

    // A link from the progress bar or the Sources tab arrives as URL params; a new one replaces the filters.
    const linkKey = `${tierParam}|${sourceParam}`;
    const [appliedLinkKey, setAppliedLinkKey] = useState(linkKey);

    if (linkKey !== appliedLinkKey) {
        setAppliedLinkKey(linkKey);

        if (tierParam !== null || sourceParam !== null) {
            setFilters(readFilters(searchParams));
        }
    }

    const visibleLeads = useMemo(() => sortLeads(filterLeads(leads, filters), filters.sort), [leads, filters]);
    const groups = useMemo(() => groupLeadsByDay(visibleLeads), [visibleLeads]);
    const sourceOptions = useMemo(
        () => sources.filter((source) => leads.some((lead) => lead.source_id === source.id)),
        [leads, sources],
    );
    const roundOptions = useMemo(
        () => [...new Set(leads.map((lead) => lead.first_round).filter((round) => round > 0))].sort((a, b) => b - a),
        [leads],
    );

    const handleDelete = async () => {
        if (!pendingDelete) {
            return;
        }

        try {
            await growthApi.deleteLead(pendingDelete.id);
            toast.success(copy.leads.deleted);
            setSelectedLead(null);
            await refetchLeads();
        } catch (error) {
            toast.error(getApiErrorMessage(error, copy.common.networkError));
        } finally {
            setPendingDelete(null);
        }
    };

    const update = (patch: Partial<LeadFilters>) => setFilters((previous) => ({ ...previous, ...patch }));
    const isFiltered =
        !!filters.search.trim() ||
        filters.tier !== 'all' ||
        filters.delivery !== 'all' ||
        filters.sourceId !== null ||
        filters.platform !== null ||
        filters.foundWithin !== 'all' ||
        filters.round !== null ||
        filters.minScore !== null ||
        filters.flags.length > 0;

    return (
        <div className="flex h-full flex-col">
            <div className="bg-background sticky top-0 z-10 flex flex-wrap gap-2 p-px pb-4">
                <InputGroup className="min-w-48 flex-1">
                    <InputGroupAddon>
                        <Search />
                    </InputGroupAddon>
                    <InputGroupInput
                        autoComplete="off"
                        onChange={(event) => update({ search: event.target.value })}
                        placeholder={copy.leads.searchPlaceholder}
                        value={filters.search}
                    />
                    {filters.search && (
                        <InputGroupAddon align="inline-end">
                            <InputGroupButton
                                aria-label={copy.leads.searchPlaceholder}
                                onClick={() => update({ search: '' })}
                            >
                                <X />
                            </InputGroupButton>
                        </InputGroupAddon>
                    )}
                </InputGroup>
                <Select
                    onValueChange={(value) => update({ foundWithin: value as LeadFoundWithin })}
                    value={filters.foundWithin}
                >
                    <SelectTrigger
                        aria-label={copy.leads.filter.foundWithin}
                        className="w-28"
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL}>{copy.leads.filter.allDates}</SelectItem>
                        <SelectItem value="today">{copy.leads.filter.today}</SelectItem>
                        <SelectItem value="7d">{copy.leads.filter.last7Days}</SelectItem>
                        <SelectItem value="30d">{copy.leads.filter.last30Days}</SelectItem>
                    </SelectContent>
                </Select>
                <Select
                    onValueChange={(value) => update({ round: value === ALL ? null : Number(value) })}
                    value={filters.round === null ? ALL : String(filters.round)}
                >
                    <SelectTrigger
                        aria-label={copy.leads.filter.round}
                        className="w-28"
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL}>{copy.leads.filter.allRounds}</SelectItem>
                        {roundOptions.map((round) => (
                            <SelectItem
                                key={round}
                                value={String(round)}
                            >
                                {copy.leads.filter.roundOption(round)}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
                <Select
                    onValueChange={(value) => update({ tier: value as 'all' | GrowthLeadTier })}
                    value={filters.tier}
                >
                    <SelectTrigger
                        aria-label={copy.leads.column.score}
                        className="w-28"
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL}>{copy.leads.filter.allTiers}</SelectItem>
                        <SelectItem value="qualified">{copy.leads.filter.qualified}</SelectItem>
                        <SelectItem value="watch">{copy.leads.filter.watch}</SelectItem>
                    </SelectContent>
                </Select>
                <Select
                    onValueChange={(value) => update({ delivery: value as LeadDeliveryFilter })}
                    value={filters.delivery}
                >
                    <SelectTrigger
                        aria-label={copy.leads.column.status}
                        className="w-28"
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL}>{copy.leads.filter.allStatuses}</SelectItem>
                        <SelectItem value="not_delivered">{copy.leads.filter.notDelivered}</SelectItem>
                        <SelectItem value="delivered">{copy.leads.filter.delivered}</SelectItem>
                    </SelectContent>
                </Select>
                <Select
                    onValueChange={(value) => update({ minScore: value === ALL ? null : Number(value) })}
                    value={filters.minScore === null ? ALL : String(filters.minScore)}
                >
                    <SelectTrigger
                        aria-label={copy.leads.filter.minScore}
                        className="w-32"
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL}>{copy.leads.filter.allScores}</SelectItem>
                        {MIN_SCORES.map((score) => (
                            <SelectItem
                                key={score}
                                value={String(score)}
                            >
                                {copy.leads.filter.minScoreOption(score)}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
                <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                        <Button variant="outline">
                            <ListFilter />
                            {copy.leads.filter.flags}
                            {!!filters.flags.length && <Badge variant="secondary">{filters.flags.length}</Badge>}
                        </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="start">
                        {FLAGS.map((flag) => (
                            <DropdownMenuCheckboxItem
                                checked={filters.flags.includes(flag)}
                                key={flag}
                                onCheckedChange={(isChecked) =>
                                    update({
                                        flags: isChecked
                                            ? [...filters.flags, flag]
                                            : filters.flags.filter((item) => item !== flag),
                                    })
                                }
                                onSelect={(event) => event.preventDefault()}
                            >
                                {copy.leads.flag[flag]}
                            </DropdownMenuCheckboxItem>
                        ))}
                    </DropdownMenuContent>
                </DropdownMenu>
                <Select
                    onValueChange={(value) => update({ contact: value as LeadContactFilter })}
                    value={filters.contact}
                >
                    <SelectTrigger
                        aria-label={copy.leads.column.contacts}
                        className="w-36"
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL}>{copy.leads.filter.allContacts}</SelectItem>
                        <SelectItem value="with">{copy.leads.filter.withContact}</SelectItem>
                        <SelectItem value="without">{copy.leads.filter.withoutContact}</SelectItem>
                    </SelectContent>
                </Select>
                <PlatformFilter
                    onChange={(platform) => update({ platform })}
                    value={filters.platform}
                />
                <Select
                    onValueChange={(value) => update({ sourceId: value === ALL ? null : Number(value) })}
                    value={filters.sourceId === null ? ALL : String(filters.sourceId)}
                >
                    <SelectTrigger
                        aria-label={copy.leads.column.source}
                        className="w-40"
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL}>{copy.leads.filter.allSources}</SelectItem>
                        {sourceOptions.map((source) => (
                            <SelectItem
                                key={source.id}
                                value={String(source.id)}
                            >
                                {source.title}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
                <Select
                    onValueChange={(value) => update({ sort: value as LeadSort })}
                    value={filters.sort}
                >
                    <SelectTrigger
                        aria-label={copy.leads.filter.sort}
                        className="w-36"
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value="score">{copy.leads.filter.sortScore}</SelectItem>
                        <SelectItem value="newest">{copy.leads.filter.sortNewest}</SelectItem>
                    </SelectContent>
                </Select>
                <div className="ml-auto flex gap-2">
                    <Button
                        onClick={() => setFormLead(null)}
                        variant="outline"
                    >
                        <Plus />
                        {copy.leads.add}
                    </Button>
                    <LeadsEmailButton />
                    <LeadsExportMenu filteredLeads={visibleLeads} />
                </div>
            </div>

            {isLeadsLoading ? (
                <div className="flex flex-1 items-center justify-center">
                    <Spinner variant="circle" />
                </div>
            ) : leadsError && !leads.length ? (
                <Empty>
                    <EmptyHeader>
                        <EmptyMedia variant="icon">
                            <AlertCircle />
                        </EmptyMedia>
                        <EmptyDescription>{copy.leads.loadFailed}</EmptyDescription>
                    </EmptyHeader>
                </Empty>
            ) : !visibleLeads.length ? (
                <Empty>
                    <EmptyHeader>
                        <EmptyMedia variant="icon">{isFiltered ? <ListFilter /> : <UserSearch />}</EmptyMedia>
                        <EmptyDescription>{isFiltered ? copy.leads.emptyFiltered : copy.leads.empty}</EmptyDescription>
                    </EmptyHeader>
                </Empty>
            ) : (
                <>
                    <div className="text-muted-foreground pb-2 text-xs">{copy.leads.total(visibleLeads.length)}</div>
                    <div className="flex flex-col gap-2">
                        {groups.map((group, index) => (
                            <Collapsible
                                key={group.key}
                                onOpenChange={(isOpen) =>
                                    setOpenDays((previous) => ({ ...previous, [group.key]: isOpen }))
                                }
                                open={openDays[group.key] ?? index === 0}
                            >
                                <CollapsibleTrigger className="hover:bg-muted/50 group flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm">
                                    <ChevronRight className="size-4 shrink-0 transition-transform group-data-[state=open]:rotate-90" />
                                    <span className="font-medium">{dayTitle(group)}</span>
                                    <span className="text-muted-foreground text-xs">
                                        {copy.leads.group.summary(group.leads.length, group.qualified, group.watch)}
                                    </span>
                                </CollapsibleTrigger>
                                <CollapsibleContent className="pt-1">
                                    <LeadsTable
                                        leads={group.leads}
                                        onOpenLead={setSelectedLead}
                                        onOpenSource={openSources}
                                    />
                                </CollapsibleContent>
                            </Collapsible>
                        ))}
                    </div>
                </>
            )}

            <LeadDetailSheet
                lead={selectedLead}
                onDelete={setPendingDelete}
                onEdit={setFormLead}
                onOpenChange={(isOpen) => !isOpen && setSelectedLead(null)}
            />
            {formLead !== undefined && (
                <LeadFormDialog
                    flowId={flowId ? Number(flowId) : null}
                    lead={formLead ?? undefined}
                    onClose={() => setFormLead(undefined)}
                    onSaved={async () => {
                        setSelectedLead(null);
                        await refetchLeads();
                    }}
                />
            )}
            <ConfirmationDialog
                confirmText={copy.action.delete}
                description={pendingDelete ? copy.leads.deleteDescription(pendingDelete.display_name) : undefined}
                handleConfirm={handleDelete}
                handleOpenChange={(isOpen) => !isOpen && setPendingDelete(null)}
                isOpen={!!pendingDelete}
                title={copy.leads.delete}
            />
        </div>
    );
}

function LeadsTable({ leads, onOpenLead, onOpenSource }: LeadsTableProps) {
    return (
        <div className="rounded-md border">
            <Table aria-label={copy.flowTabs.leads}>
                <TableHeader>
                    <TableRow>
                        <TableHead>{copy.leads.column.player}</TableHead>
                        <TableHead>{copy.leads.column.score}</TableHead>
                        <TableHead>{copy.leads.column.signals}</TableHead>
                        <TableHead>{copy.leads.column.contacts}</TableHead>
                        <TableHead>{copy.leads.column.source}</TableHead>
                        <TableHead>{copy.leads.column.evidence}</TableHead>
                        <TableHead>{copy.leads.column.lastActive}</TableHead>
                        <TableHead>{copy.leads.column.status}</TableHead>
                        <TableHead>{copy.leads.column.foundIn}</TableHead>
                    </TableRow>
                </TableHeader>
                <TableBody>
                    {leads.map((lead) => (
                        <TableRow
                            className="cursor-pointer"
                            key={lead.id}
                            onClick={() => onOpenLead(lead)}
                        >
                            <TableCell>
                                <div className="font-medium">{lead.display_name}</div>
                                {lead.username && <div className="text-muted-foreground text-xs">@{lead.username}</div>}
                            </TableCell>
                            <TableCell>
                                <LeadScoreBadge lead={lead} />
                            </TableCell>
                            <TableCell>
                                <div className="flex w-56 flex-wrap gap-1">
                                    {lead.signals.map((signal) => (
                                        <Badge
                                            key={signal}
                                            variant="outline"
                                        >
                                            {copy.leads.signal[signal] ?? signal}
                                        </Badge>
                                    ))}
                                </div>
                            </TableCell>
                            <TableCell>
                                {lead.contacts?.length ? (
                                    <div className="flex w-48 flex-col gap-0.5 text-xs">
                                        {lead.contacts.map((contact) => (
                                            <span
                                                className="break-all"
                                                key={`${contact.type}:${contact.value}`}
                                            >
                                                {copy.leads.contactText(
                                                    copy.profile.contactType[contact.type] ?? contact.type,
                                                    contact.value,
                                                )}
                                            </span>
                                        ))}
                                    </div>
                                ) : (
                                    <span className="text-muted-foreground text-xs whitespace-nowrap">
                                        {copy.leads.contactOnlyHandle}
                                    </span>
                                )}
                            </TableCell>
                            <TableCell>
                                <button
                                    className="hover:underline"
                                    onClick={(event) => {
                                        event.stopPropagation();
                                        onOpenSource(lead.source_title);
                                    }}
                                    type="button"
                                >
                                    {lead.source_title}
                                </button>
                            </TableCell>
                            <TableCell className="whitespace-nowrap">
                                <Tooltip>
                                    <TooltipTrigger asChild>
                                        <span>{copy.leads.evidenceCount(lead.evidence.length)}</span>
                                    </TooltipTrigger>
                                    {lead.evidence[0] && (
                                        <TooltipContent className="max-w-64">
                                            {[...lead.evidence[0].excerpt].slice(0, 40).join('')}
                                        </TooltipContent>
                                    )}
                                </Tooltip>
                            </TableCell>
                            <TableCell className="text-muted-foreground whitespace-nowrap">
                                {lead.last_active_at
                                    ? copy.common.relative(new Date(lead.last_active_at))
                                    : copy.common.dash}
                            </TableCell>
                            <TableCell className="whitespace-nowrap">
                                {lead.delivered_at
                                    ? copy.leads.status.delivered(copy.common.date(new Date(lead.delivered_at)))
                                    : copy.leads.status.notDelivered}
                            </TableCell>
                            <TableCell className="text-muted-foreground whitespace-nowrap">
                                {lead.origin === 'manual'
                                    ? copy.leads.manual
                                    : copy.leads.foundIn(lead.first_flow_title, lead.first_round)}
                            </TableCell>
                        </TableRow>
                    ))}
                </TableBody>
            </Table>
        </div>
    );
}

export default FlowLeads;
