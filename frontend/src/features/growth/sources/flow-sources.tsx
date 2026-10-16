import { AlertCircle, ExternalLink, ListFilter, Plus, Search, Users, X } from 'lucide-react';
import { useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';

import { Badge, type BadgeVariant } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia } from '@/components/ui/empty';
import { InputGroup, InputGroupAddon, InputGroupButton, InputGroupInput } from '@/components/ui/input-group';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Spinner } from '@/components/ui/spinner';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { Textarea } from '@/components/ui/textarea';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';
import { formatNumber } from '@/lib/utils/format';

import type {
    GrowthPlatform,
    GrowthSourceAddResult,
    GrowthSourceJoinStatus,
    GrowthSourceOrigin,
} from '../growth-types';

import { growthApi } from '../growth-api';
import { useGrowthFlow } from '../growth-flow-provider';
import { PlatformFilter } from '../platform-filter';
import { MAX_SOURCE_LINKS, splitSourceLinks } from '../source-links';
import { SOURCE_QUERY_PARAM, useGrowthTabLink } from '../use-growth-tab-link';

const ALL = 'all';

const joinStatusVariant: Record<GrowthSourceJoinStatus, BadgeVariant> = {
    joined: 'green',
    not_joined: 'outline',
    pending_approval: 'yellow',
    unjoinable: 'red',
};

const describeResult = ({ link, outcome }: GrowthSourceAddResult): null | string => {
    switch (outcome) {
        case 'existing':
            return copy.sources.existing(link);
        case 'invalid':
            return copy.sources.invalid(link);
        case 'unresolved':
            return copy.sources.unresolved(link);
        default:
            return null;
    }
};

function FlowSources() {
    const { isSourcesLoading, refetchSources, sources, sourcesError } = useGrowthFlow();
    const { openLeads } = useGrowthTabLink();
    const [searchParams] = useSearchParams();
    const queryParam = searchParams.get(SOURCE_QUERY_PARAM);

    const [linksText, setLinksText] = useState('');
    const [isAdding, setIsAdding] = useState(false);
    const [messages, setMessages] = useState<{ isError: boolean; text: string }[]>([]);
    const [search, setSearch] = useState(queryParam ?? '');
    const [joinStatus, setJoinStatus] = useState<'all' | GrowthSourceJoinStatus>(ALL);
    const [origin, setOrigin] = useState<'all' | GrowthSourceOrigin>(ALL);
    const [platform, setPlatform] = useState<GrowthPlatform | null>(null);

    const [appliedQueryParam, setAppliedQueryParam] = useState(queryParam);

    if (queryParam !== appliedQueryParam) {
        setAppliedQueryParam(queryParam);

        if (queryParam !== null) {
            setSearch(queryParam);
        }
    }

    const visibleSources = useMemo(() => {
        const query = search.trim().toLowerCase();

        return sources.filter(
            (source) =>
                (joinStatus === ALL || source.join_status === joinStatus) &&
                (origin === ALL || source.origin === origin) &&
                (platform === null || source.platform === platform) &&
                (!query || source.title.toLowerCase().includes(query) || source.link.toLowerCase().includes(query)),
        );
    }, [sources, search, joinStatus, origin, platform]);

    const handleAdd = async () => {
        const links = splitSourceLinks(linksText);

        if (!links.length) {
            return;
        }

        if (links.length > MAX_SOURCE_LINKS) {
            setMessages([{ isError: true, text: copy.sources.tooMany }]);

            return;
        }

        setIsAdding(true);

        try {
            const { results } = await growthApi.addSources(links);
            const added = results.filter((result) => result.outcome === 'added').length;
            const problems = results.map(describeResult).filter((text): text is string => !!text);

            setMessages([
                ...(added ? [{ isError: false, text: copy.sources.addedCount(added) }] : []),
                ...problems.map((text) => ({ isError: true, text })),
            ]);

            // Keep only what still needs fixing in the box.
            setLinksText(
                results
                    .filter((result) => result.outcome === 'invalid' || result.outcome === 'unresolved')
                    .map((result) => result.link)
                    .join('\n'),
            );
            await refetchSources();
        } catch (error) {
            setMessages([{ isError: true, text: getApiErrorMessage(error, copy.common.networkError) }]);
        } finally {
            setIsAdding(false);
        }
    };

    const isFiltered = !!search.trim() || joinStatus !== ALL || origin !== ALL;

    return (
        <div className="flex h-full flex-col gap-4">
            <div className="flex flex-col gap-2 p-px">
                <Textarea
                    onChange={(event) => setLinksText(event.target.value)}
                    placeholder={copy.sources.placeholder}
                    rows={3}
                    value={linksText}
                />
                <div className="flex flex-wrap items-start gap-2">
                    <Button
                        disabled={isAdding || !linksText.trim()}
                        onClick={handleAdd}
                        size="sm"
                    >
                        {isAdding ? <Spinner variant="circle" /> : <Plus />}
                        {copy.sources.add}
                    </Button>
                    {!!messages.length && (
                        <ul className="flex min-w-0 flex-1 flex-col gap-0.5 text-sm">
                            {messages.map((message, index) => (
                                <li
                                    className={message.isError ? 'text-destructive break-all' : 'text-muted-foreground'}
                                    key={index}
                                >
                                    {message.text}
                                </li>
                            ))}
                        </ul>
                    )}
                </div>
            </div>

            <div className="flex flex-wrap gap-2 p-px">
                <InputGroup className="min-w-48 flex-1">
                    <InputGroupAddon>
                        <Search />
                    </InputGroupAddon>
                    <InputGroupInput
                        autoComplete="off"
                        onChange={(event) => setSearch(event.target.value)}
                        placeholder={copy.sources.searchPlaceholder}
                        value={search}
                    />
                    {search && (
                        <InputGroupAddon align="inline-end">
                            <InputGroupButton
                                aria-label={copy.sources.searchPlaceholder}
                                onClick={() => setSearch('')}
                            >
                                <X />
                            </InputGroupButton>
                        </InputGroupAddon>
                    )}
                </InputGroup>
                <PlatformFilter
                    onChange={setPlatform}
                    value={platform}
                />
                <Select
                    onValueChange={(value) => setJoinStatus(value as 'all' | GrowthSourceJoinStatus)}
                    value={joinStatus}
                >
                    <SelectTrigger
                        aria-label={copy.sources.column.joinStatus}
                        className="w-36"
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL}>{copy.sources.filter.allStatuses}</SelectItem>
                        {(Object.keys(copy.sources.joinStatus) as GrowthSourceJoinStatus[]).map((status) => (
                            <SelectItem
                                key={status}
                                value={status}
                            >
                                {copy.sources.joinStatus[status]}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
                <Select
                    onValueChange={(value) => setOrigin(value as 'all' | GrowthSourceOrigin)}
                    value={origin}
                >
                    <SelectTrigger
                        aria-label={copy.sources.column.origin}
                        className="w-36"
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        <SelectItem value={ALL}>{copy.sources.filter.allOrigins}</SelectItem>
                        <SelectItem value="ai">{copy.sources.origin.ai}</SelectItem>
                        <SelectItem value="manual">{copy.sources.origin.manual}</SelectItem>
                    </SelectContent>
                </Select>
            </div>

            {isSourcesLoading ? (
                <div className="flex flex-1 items-center justify-center">
                    <Spinner variant="circle" />
                </div>
            ) : sourcesError && !sources.length ? (
                <Empty>
                    <EmptyHeader>
                        <EmptyMedia variant="icon">
                            <AlertCircle />
                        </EmptyMedia>
                        <EmptyDescription>{copy.sources.loadFailed}</EmptyDescription>
                    </EmptyHeader>
                </Empty>
            ) : !visibleSources.length ? (
                <Empty>
                    <EmptyHeader>
                        <EmptyMedia variant="icon">{isFiltered ? <ListFilter /> : <Users />}</EmptyMedia>
                        <EmptyDescription>
                            {isFiltered ? copy.sources.emptyFiltered : copy.sources.empty}
                        </EmptyDescription>
                    </EmptyHeader>
                </Empty>
            ) : (
                <div className="rounded-md border">
                    <Table aria-label={copy.flowTabs.sources}>
                        <TableHeader>
                            <TableRow>
                                <TableHead>{copy.sources.column.source}</TableHead>
                                <TableHead>{copy.sources.column.origin}</TableHead>
                                <TableHead>{copy.sources.column.joinStatus}</TableHead>
                                <TableHead className="text-right">{copy.sources.column.members}</TableHead>
                                <TableHead>{copy.sources.column.lastRead}</TableHead>
                                <TableHead className="text-right">{copy.sources.column.leads}</TableHead>
                            </TableRow>
                        </TableHeader>
                        <TableBody>
                            {visibleSources.map((source) => {
                                const hint = copy.sources.joinStatusHint[source.join_status];
                                const badge = (
                                    <Badge variant={joinStatusVariant[source.join_status]}>
                                        {copy.sources.joinStatus[source.join_status]}
                                    </Badge>
                                );

                                return (
                                    <TableRow key={source.id}>
                                        <TableCell>
                                            <div className="font-medium">{source.title}</div>
                                            <a
                                                className="text-muted-foreground flex items-center gap-1 text-xs hover:underline"
                                                href={source.link}
                                                rel="noopener noreferrer"
                                                target="_blank"
                                            >
                                                {source.link}
                                                <ExternalLink className="size-3" />
                                            </a>
                                        </TableCell>
                                        <TableCell>
                                            <Badge variant={source.origin === 'ai' ? 'blue' : 'purple'}>
                                                {copy.sources.origin[source.origin]}
                                            </Badge>
                                        </TableCell>
                                        <TableCell>
                                            {hint ? (
                                                <Tooltip>
                                                    <TooltipTrigger asChild>
                                                        <span>{badge}</span>
                                                    </TooltipTrigger>
                                                    <TooltipContent className="max-w-64">{hint}</TooltipContent>
                                                </Tooltip>
                                            ) : (
                                                badge
                                            )}
                                        </TableCell>
                                        <TableCell className="text-right tabular-nums">
                                            {source.members_count == null ? '' : formatNumber(source.members_count)}
                                        </TableCell>
                                        <TableCell className="text-muted-foreground whitespace-nowrap">
                                            {source.last_read_at
                                                ? copy.common.relative(new Date(source.last_read_at))
                                                : copy.common.dash}
                                        </TableCell>
                                        <TableCell className="text-right tabular-nums">
                                            <button
                                                className="hover:underline disabled:no-underline"
                                                disabled={!source.leads_count}
                                                onClick={() => openLeads({ sourceId: source.id })}
                                                type="button"
                                            >
                                                {source.leads_count}
                                            </button>
                                        </TableCell>
                                    </TableRow>
                                );
                            })}
                        </TableBody>
                    </Table>
                </div>
            )}
        </div>
    );
}

export default FlowSources;
