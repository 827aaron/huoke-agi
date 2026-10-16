import { ExternalLink, List, RotateCcw, Search } from 'lucide-react';

import { Button } from '@/components/ui/button';
import { Progress } from '@/components/ui/progress';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';
import { cn } from '@/lib/utils';

import type { GrowthRun } from '../growth-types';

import { accountsBanner, accountsBannerText } from '../account-health';
import { useGrowthFlow } from '../growth-flow-provider';
import LeadsExportMenu from '../leads/leads-export-menu';
import { useGrowthTabLink } from '../use-growth-tab-link';
import { useSubmitGrowthMessage } from '../use-submit-growth-message';

const isActive = (run: GrowthRun) => run.status === 'running' || run.status === 'waiting';

function GrowthProgressBar() {
    const { accounts, run } = useGrowthFlow();
    const { openLeads, openSources } = useGrowthTabLink();
    const { isSubmitting, submit } = useSubmitGrowthMessage();

    if (!run || run.target === null) {
        return null;
    }

    const target = run.target;
    const isDeliveredState = run.status === 'delivered' || run.status === 'missed';
    const count = isDeliveredState ? run.delivered : run.found;
    const line = isDeliveredState ? copy.progress.deliveredLine(count, target) : copy.progress.foundLine(count, target);
    const percent = target > 0 ? Math.min(100, (count / target) * 100) : 0;
    const isReached = count >= target;

    const banner = accounts && isActive(run) ? accountsBanner(accounts) : null;
    const action = banner
        ? accountsBannerText(banner)
        : run.status === 'waiting' && run.flood_wait_seconds
          ? copy.telegram.floodWait(run.flood_wait_seconds)
          : isActive(run)
            ? run.current_action
            : null;

    return (
        <div className="bg-background flex flex-col gap-1.5 border-b px-4 py-2 text-sm">
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1">
                <span>
                    {line.prefix} <strong className="text-base tabular-nums">{line.count}</strong>
                    {line.suffix}
                </span>
                {isActive(run) && (
                    <>
                        <button
                            className="text-muted-foreground hover:text-foreground hover:underline"
                            onClick={() => openLeads({ tier: 'watch' })}
                            type="button"
                        >
                            {copy.progress.watch(run.watch)}
                        </button>
                        <button
                            className="text-muted-foreground hover:text-foreground hidden hover:underline md:inline"
                            onClick={() => openSources()}
                            type="button"
                        >
                            {copy.progress.sources(run.sources)}
                        </button>
                        <span className="text-muted-foreground ml-auto flex items-center gap-1.5 text-xs">
                            <span className="size-2 animate-pulse rounded-full bg-green-500" />
                            {copy.progress.running}
                        </span>
                    </>
                )}
                {run.status === 'delivered' && (
                    <div className="ml-auto flex gap-2">
                        <Button
                            onClick={() => openLeads()}
                            size="sm"
                            variant="outline"
                        >
                            <List />
                            {copy.progress.viewLeads}
                        </Button>
                        <LeadsExportMenu />
                    </div>
                )}
                {run.status === 'missed' && (
                    <>
                        <span className="text-muted-foreground">{copy.progress.missed}</span>
                        <div className="ml-auto flex gap-2">
                            <Button
                                asChild
                                size="sm"
                                variant="outline"
                            >
                                <a
                                    href={routes.flowReport(run.flow_id)}
                                    rel="noopener noreferrer"
                                    target="_blank"
                                >
                                    <ExternalLink />
                                    {copy.progress.viewReport}
                                </a>
                            </Button>
                            <Button
                                disabled={isSubmitting}
                                onClick={() => submit(copy.quickActions.moreText(target - run.delivered))}
                                size="sm"
                            >
                                <Search />
                                {copy.progress.more(target - run.delivered)}
                            </Button>
                        </div>
                    </>
                )}
                {run.status === 'failed' && (
                    <>
                        <span className="text-destructive">{copy.progress.failed(run.error ?? '', run.found)}</span>
                        <Button
                            className="ml-auto"
                            disabled={isSubmitting}
                            onClick={() => submit(copy.progress.retryText)}
                            size="sm"
                            variant="outline"
                        >
                            <RotateCcw />
                            {copy.progress.retry}
                        </Button>
                    </>
                )}
            </div>
            <Progress
                aria-label={`${line.prefix} ${line.count}${line.suffix}`}
                className={cn('h-1.5', isReached && '[&>[data-slot=progress-indicator]]:bg-green-500')}
                value={percent}
            />
            {run.notify_status === 'sent' && (
                <div className="text-muted-foreground text-xs">{copy.progress.emailSent}</div>
            )}
            {run.notify_status === 'failed' && (
                <div className="text-destructive text-xs">
                    {copy.progress.emailFailed(run.notify_error ?? copy.common.dash)}
                </div>
            )}
            {action && (
                <div
                    className={cn(
                        'truncate text-xs',
                        banner ? 'text-yellow-800 dark:text-yellow-400' : 'text-muted-foreground',
                    )}
                >
                    {action}
                </div>
            )}
        </div>
    );
}

export default GrowthProgressBar;
