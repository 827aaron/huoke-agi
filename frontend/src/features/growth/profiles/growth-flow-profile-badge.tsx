import { Briefcase } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { copy } from '@/lib/copy';

import { useGrowthFlow } from '../growth-flow-provider';

// The profile the task judges with, shown once the server has answered.
export function GrowthFlowProfileBadge() {
    const { flowProfile } = useGrowthFlow();

    if (!flowProfile?.title) {
        return null;
    }

    const badge = (
        <Badge
            className="max-w-48 gap-1"
            variant="outline"
        >
            <Briefcase className="size-3 shrink-0" />
            <span className="truncate">{copy.flowProfile.label(flowProfile.title)}</span>
        </Badge>
    );

    if (!flowProfile.fallback) {
        return badge;
    }

    return (
        <Tooltip>
            <TooltipTrigger asChild>{badge}</TooltipTrigger>
            <TooltipContent>{copy.flowProfile.fallback}</TooltipContent>
        </Tooltip>
    );
}
