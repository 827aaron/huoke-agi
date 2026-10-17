import type { LucideIcon } from 'lucide-react';

import {
    Bot,
    Brain,
    Code2,
    FileText,
    HardDrive,
    HardDriveDownload,
    HelpCircle,
    LayoutList,
    MessagesSquare,
    RefreshCw,
    Search,
    Settings,
    Sigma,
    UserSearch,
    Wrench,
} from 'lucide-react';

import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { AgentType } from '@/graphql/types';
import { agentLabel } from '@/lib/copy';
import { cn } from '@/lib/utils';

interface FlowAgentIconProps {
    className?: string;
    tooltip?: string;
    type?: AgentType;
}

const icons: Record<AgentType, LucideIcon> = {
    [AgentType.Adviser]: HelpCircle,
    [AgentType.Assistant]: Bot,
    [AgentType.Coder]: Code2,
    [AgentType.Enricher]: HardDriveDownload,
    [AgentType.Generator]: LayoutList,
    [AgentType.Installer]: Settings,
    [AgentType.Memorist]: HardDrive,
    [AgentType.Pentester]: UserSearch,
    [AgentType.PrimaryAgent]: Brain,
    [AgentType.Refiner]: RefreshCw,
    [AgentType.Reflector]: MessagesSquare,
    [AgentType.Reporter]: FileText,
    [AgentType.Searcher]: Search,
    [AgentType.Summarizer]: Sigma,
    [AgentType.ToolCallFixer]: Wrench,
};
const defaultIcon = HelpCircle;

function FlowAgentIcon({ className, type, tooltip = type }: FlowAgentIconProps) {
    const Icon = type ? icons[type] || defaultIcon : defaultIcon;
    const iconElement = <Icon className={cn('size-3 shrink-0', className)} />;

    if (!tooltip) {
        return iconElement;
    }

    return (
        <Tooltip>
            <TooltipTrigger asChild>{iconElement}</TooltipTrigger>
            <TooltipContent>{agentLabel(tooltip)}</TooltipContent>
        </Tooltip>
    );
}

export default FlowAgentIcon;
