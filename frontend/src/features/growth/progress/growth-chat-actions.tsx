import { List, PackageCheck } from 'lucide-react';
import { useMemo } from 'react';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { StatusType } from '@/graphql/types';
import { copy } from '@/lib/copy';
import { useFlow } from '@/providers/flow-provider';

import { useGrowthFlow } from '../growth-flow-provider';
import LeadsExportMenu from '../leads/leads-export-menu';
import { useGrowthTabLink } from '../use-growth-tab-link';
import { useSubmitGrowthMessage } from '../use-submit-growth-message';

const FALLBACK_TARGET = 50;

// The delivery card and the "find more" chips that sit above the automation input once a round ends.
function GrowthChatActions() {
    const { flowStatus } = useFlow();
    const { run } = useGrowthFlow();

    const isWaitingForInput = flowStatus === StatusType.Waiting;
    const isDelivered = run?.status === 'delivered';

    if (!isWaitingForInput && !isDelivered) {
        return null;
    }

    return (
        <GrowthChatActionsContent
            isDelivered={isDelivered}
            isWaitingForInput={isWaitingForInput}
        />
    );
}

function GrowthChatActionsContent({
    isDelivered,
    isWaitingForInput,
}: {
    isDelivered: boolean;
    isWaitingForInput: boolean;
}) {
    const { leads, profile, run } = useGrowthFlow();
    const { openLeads } = useGrowthTabLink();
    const { isSubmitting, submit } = useSubmitGrowthMessage();

    const target = profile?.default_target || FALLBACK_TARGET;

    const roundLeads = useMemo(
        () => (run ? leads.filter((lead) => lead.delivered_task_id === run.task_id) : []),
        [leads, run],
    );
    const sourceCount = useMemo(() => new Set(roundLeads.map((lead) => lead.source_id)).size, [roundLeads]);

    const quickActions = [
        { label: copy.quickActions.more(target), text: copy.quickActions.moreText(target) },
        { label: copy.quickActions.changeDirection, text: copy.quickActions.changeDirectionText },
        { label: copy.quickActions.watchOnly, text: copy.quickActions.watchOnlyText },
    ];

    return (
        <div className="flex flex-col gap-2 pb-2">
            {isDelivered && run && (
                <Card className="gap-3 py-4">
                    <CardHeader className="px-4">
                        <CardTitle className="flex items-center gap-2 text-base">
                            <PackageCheck className="size-4" />
                            {copy.delivery.title(run.delivered)}
                        </CardTitle>
                    </CardHeader>
                    <CardContent className="text-muted-foreground flex flex-col gap-1 px-4 text-sm">
                        <p>{copy.delivery.qualified(run.delivered, run.watch)}</p>
                        <p>{copy.delivery.sources(sourceCount)}</p>
                        <p>{copy.delivery.evidence}</p>
                        <div className="flex flex-wrap gap-2 pt-2">
                            <Button
                                onClick={() => openLeads()}
                                size="sm"
                            >
                                <List />
                                {copy.progress.viewLeads}
                            </Button>
                            <LeadsExportMenu variant="outline" />
                        </div>
                    </CardContent>
                </Card>
            )}
            {isWaitingForInput && (
                <div className="flex flex-wrap gap-2">
                    {quickActions.map((action) => (
                        <Button
                            className="rounded-full"
                            disabled={isSubmitting}
                            key={action.label}
                            onClick={() => submit(action.text)}
                            size="sm"
                            variant="outline"
                        >
                            {action.label}
                        </Button>
                    ))}
                </div>
            )}
        </div>
    );
}

export default GrowthChatActions;
