import { useEffect, useRef } from 'react';

import { ScrollArea, ScrollBar } from '@/components/ui/scroll-area';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import FlowAgents from '@/features/flows/agents/flow-agents';
import FlowDashboard from '@/features/flows/dashboard/flow-dashboard';
import FlowFiles from '@/features/flows/files/flow-files';
import FlowAssistantMessages from '@/features/flows/messages/flow-assistant-messages';
import FlowAutomationMessages from '@/features/flows/messages/flow-automation-messages';
import FlowScreenshots from '@/features/flows/screenshots/flow-screenshots';
import FlowTasks from '@/features/flows/tasks/flow-tasks';
import FlowTerminal from '@/features/flows/terminal/flow-terminal';
import FlowTools from '@/features/flows/tools/flow-tools';
import FlowVectorStores from '@/features/flows/vector-stores/flow-vector-stores';
import FlowLeads from '@/features/growth/leads/flow-leads';
import FlowSources from '@/features/growth/sources/flow-sources';
import { useBreakpoint } from '@/hooks/use-breakpoint';
import { copy } from '@/lib/copy';

interface FlowTabsProps {
    activeTab: string;
    onTabChange: (tab: string) => void;
}

function FlowTabs({ activeTab, onTabChange }: FlowTabsProps) {
    const { isDesktop } = useBreakpoint();

    const previousActiveTabRef = useRef<string>(activeTab);

    useEffect(() => {
        if (activeTab === previousActiveTabRef.current) {
            return;
        }

        previousActiveTabRef.current = activeTab;
    }, [activeTab]);

    return (
        <Tabs
            className="flex size-full flex-col"
            onValueChange={onTabChange}
            value={activeTab}
        >
            <div className="max-w-full pr-4">
                <ScrollArea className="w-full pb-3">
                    <TabsList className="flex w-fit">
                        {!isDesktop && <TabsTrigger value="automation">{copy.flowTabs.automation}</TabsTrigger>}
                        {!isDesktop && <TabsTrigger value="assistant">{copy.flowTabs.assistant}</TabsTrigger>}
                        {!isDesktop && <TabsTrigger value="dashboard">{copy.flowTabs.dashboard}</TabsTrigger>}
                        <TabsTrigger value="leads">{copy.flowTabs.leads}</TabsTrigger>
                        <TabsTrigger value="sources">{copy.flowTabs.sources}</TabsTrigger>
                        <TabsTrigger value="terminal">{copy.flowTabs.terminal}</TabsTrigger>
                        <TabsTrigger value="tasks">{copy.flowTabs.tasks}</TabsTrigger>
                        <TabsTrigger value="agents">{copy.flowTabs.agents}</TabsTrigger>
                        <TabsTrigger value="tools">{copy.flowTabs.tools}</TabsTrigger>
                        <TabsTrigger value="vectorStores">{copy.flowTabs.vectorStores}</TabsTrigger>
                        <TabsTrigger value="files">{copy.flowTabs.files}</TabsTrigger>
                        <TabsTrigger value="screenshots">{copy.flowTabs.screenshots}</TabsTrigger>
                    </TabsList>
                    <ScrollBar orientation="horizontal" />
                </ScrollArea>
            </div>

            {!isDesktop && (
                <TabsContent
                    className="mt-1 flex-1 overflow-auto"
                    value="automation"
                >
                    <FlowAutomationMessages className="pr-4" />
                </TabsContent>
            )}
            {!isDesktop && (
                <TabsContent
                    className="mt-1 flex-1 overflow-auto"
                    value="assistant"
                >
                    <FlowAssistantMessages className="pr-4" />
                </TabsContent>
            )}
            {!isDesktop && (
                <TabsContent
                    className="mt-1 flex-1 overflow-auto pr-4"
                    value="dashboard"
                >
                    <FlowDashboard />
                </TabsContent>
            )}

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="leads"
            >
                <FlowLeads />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="sources"
            >
                <FlowSources />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto"
                value="terminal"
            >
                <FlowTerminal />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="tasks"
            >
                <FlowTasks />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="agents"
            >
                <FlowAgents />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="tools"
            >
                <FlowTools />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="vectorStores"
            >
                <FlowVectorStores />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="files"
            >
                <FlowFiles />
            </TabsContent>

            <TabsContent
                className="mt-1 flex-1 overflow-auto pr-4"
                value="screenshots"
            >
                <FlowScreenshots />
            </TabsContent>
        </Tabs>
    );
}

export default FlowTabs;
