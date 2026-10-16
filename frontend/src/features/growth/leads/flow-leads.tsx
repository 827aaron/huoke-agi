import { AlertCircle, ListFilter, Search, UserSearch, X } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
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
import { copy } from '@/lib/copy';

import type { GrowthLead, GrowthLeadTier } from '../growth-types';

import { useGrowthFlow } from '../growth-flow-provider';
import { LEAD_SOURCE_PARAM, LEAD_TIER_PARAM, useGrowthTabLink } from '../use-growth-tab-link';
import LeadDetailSheet from './lead-detail-sheet';
import LeadScoreBadge from './lead-score-badge';
import LeadsEmailButton from './leads-email-button';
import LeadsExportMenu from './leads-export-menu';
import {
    EMPTY_LEAD_FILTERS,
    filterLeads,
    type LeadDeliveryFilter,
    type LeadFilters,
    type LeadFlag,
    sortLeads,
} from './leads-filter';

const ALL = 'all';
const FLAGS: LeadFlag[] = ['competitor', 'paying', 'recent'];

const readFilters = (params: URLSearchParams): LeadFilters => {
    const tier = params.get(LEAD_TIER_PARAM);
    const sourceId = Number(params.get(LEAD_SOURCE_PARAM));

    return {
        ...EMPTY_LEAD_FILTERS,
        sourceId: Number.isInteger(sourceId) && sourceId > 0 ? sourceId : null,
        tier: tier === 'qualified' || tier === 'watch' ? tier : 'all',
    };
};

function FlowLeads() {
    const { isLeadsLoading, leads, leadsError, sources } = useGrowthFlow();
    const { openSources } = useGrowthTabLink();
    const [searchParams] = useSearchParams();

    const tierParam = searchParams.get(LEAD_TIER_PARAM);
    const sourceParam = searchParams.get(LEAD_SOURCE_PARAM);

    const [filters, setFilters] = useState<LeadFilters>(() => readFilters(searchParams));
    const [selectedLead, setSelectedLead] = useState<GrowthLead | null>(null);

    // A link from the progress bar or the Sources tab arrives as URL params; a new one replaces the filters.
    const linkKey = `${tierParam}|${sourceParam}`;
    const [appliedLinkKey, setAppliedLinkKey] = useState(linkKey);

    if (linkKey !== appliedLinkKey) {
        setAppliedLinkKey(linkKey);

        if (tierParam !== null || sourceParam !== null) {
            setFilters(readFilters(searchParams));
        }
    }

    const visibleLeads = useMemo(() => sortLeads(filterLeads(leads, filters)), [leads, filters]);
    const sourceOptions = useMemo(
        () => sources.filter((source) => leads.some((lead) => lead.source_id === source.id)),
        [leads, sources],
    );

    const update = (patch: Partial<LeadFilters>) => setFilters((previous) => ({ ...previous, ...patch }));
    const isFiltered =
        !!filters.search.trim() ||
        filters.tier !== 'all' ||
        filters.delivery !== 'all' ||
        filters.sourceId !== null ||
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
                    onValueChange={(value) => update({ tier: value as 'all' | GrowthLeadTier })}
                    value={filters.tier}
                >
                    <SelectTrigger className="w-28">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL}>{copy.leads.filter.all}</SelectItem>
                        <SelectItem value="qualified">{copy.leads.filter.qualified}</SelectItem>
                        <SelectItem value="watch">{copy.leads.filter.watch}</SelectItem>
                    </SelectContent>
                </Select>
                <Select
                    onValueChange={(value) => update({ delivery: value as LeadDeliveryFilter })}
                    value={filters.delivery}
                >
                    <SelectTrigger className="w-28">
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL}>{copy.leads.filter.all}</SelectItem>
                        <SelectItem value="not_delivered">{copy.leads.filter.notDelivered}</SelectItem>
                        <SelectItem value="delivered">{copy.leads.filter.delivered}</SelectItem>
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
                    onValueChange={(value) => update({ sourceId: value === ALL ? null : Number(value) })}
                    value={filters.sourceId === null ? ALL : String(filters.sourceId)}
                >
                    <SelectTrigger className="w-40">
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
                <div className="ml-auto flex gap-2">
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
                    <div className="rounded-md border">
                        <Table aria-label={copy.flowTabs.leads}>
                            <TableHeader>
                                <TableRow>
                                    <TableHead>{copy.leads.column.player}</TableHead>
                                    <TableHead>{copy.leads.column.score}</TableHead>
                                    <TableHead>{copy.leads.column.signals}</TableHead>
                                    <TableHead>{copy.leads.column.source}</TableHead>
                                    <TableHead>{copy.leads.column.evidence}</TableHead>
                                    <TableHead>{copy.leads.column.lastActive}</TableHead>
                                    <TableHead>{copy.leads.column.status}</TableHead>
                                    <TableHead>{copy.leads.column.foundIn}</TableHead>
                                </TableRow>
                            </TableHeader>
                            <TableBody>
                                {visibleLeads.map((lead) => (
                                    <TableRow
                                        className="cursor-pointer"
                                        key={lead.id}
                                        onClick={() => setSelectedLead(lead)}
                                    >
                                        <TableCell>
                                            <div className="font-medium">{lead.display_name}</div>
                                            {lead.username && (
                                                <div className="text-muted-foreground text-xs">@{lead.username}</div>
                                            )}
                                        </TableCell>
                                        <TableCell>
                                            <LeadScoreBadge lead={lead} />
                                        </TableCell>
                                        <TableCell>
                                            <div className="flex max-w-56 flex-wrap gap-1">
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
                                            <button
                                                className="hover:underline"
                                                onClick={(event) => {
                                                    event.stopPropagation();
                                                    openSources(lead.source_title);
                                                }}
                                                type="button"
                                            >
                                                {lead.source_title}
                                            </button>
                                        </TableCell>
                                        <TableCell>
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
                                                ? copy.leads.status.delivered(
                                                      copy.common.date(new Date(lead.delivered_at)),
                                                  )
                                                : copy.leads.status.notDelivered}
                                        </TableCell>
                                        <TableCell className="text-muted-foreground whitespace-nowrap">
                                            {copy.leads.foundIn(lead.first_flow_title, lead.first_round)}
                                        </TableCell>
                                    </TableRow>
                                ))}
                            </TableBody>
                        </Table>
                    </div>
                </>
            )}

            <LeadDetailSheet
                lead={selectedLead}
                onOpenChange={(isOpen) => !isOpen && setSelectedLead(null)}
            />
        </div>
    );
}

export default FlowLeads;
