import { useMemo, useState } from 'react';
import { useNavigate } from 'react-router-dom';

import { AppHeader, AppHeaderContent, AppHeaderTitle } from '@/components/layouts/app/app-header';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { FlowForm, type FlowFormValues } from '@/features/flows/flow-form';
import GrowthSetupHints from '@/features/growth/growth-setup-hints';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';
import { useFlows } from '@/providers/flows-provider';
import { useProviders } from '@/providers/providers-provider';
import { useSystemSettings } from '@/providers/system-settings-provider';

function NewFlow() {
    const navigate = useNavigate();

    const { selectedProvider } = useProviders();
    const { createFlow, createFlowWithAssistant } = useFlows();
    const { settings } = useSystemSettings();

    const [isLoading, setIsLoading] = useState(false);
    const [flowType, setFlowType] = useState<'assistant' | 'automation'>('automation');
    // A chip fills the input by remounting the form with the chip's sentence; it never submits.
    const [draft, setDraft] = useState({ key: 0, message: '' });

    const shouldUseAgents = useMemo(() => {
        return settings?.assistantUseAgents ?? false;
    }, [settings?.assistantUseAgents]);

    const handleSubmit = async (values: FlowFormValues) => {
        if (isLoading) {
            return false;
        }

        setIsLoading(true);

        try {
            const flowId = flowType === 'automation' ? await createFlow(values) : await createFlowWithAssistant(values);

            if (!flowId) {
                return false;
            }

            navigate(routes.flow(flowId, { tab: flowType }));

            return true;
        } finally {
            setIsLoading(false);
        }
    };

    return (
        <>
            <AppHeader>
                <AppHeaderContent>
                    <AppHeaderTitle>{copy.newFlow.pageTitle}</AppHeaderTitle>
                </AppHeaderContent>
            </AppHeader>
            <div className="flex min-h-[calc(100dvh-3rem)] items-center justify-center p-4">
                <Card className="w-full max-w-2xl">
                    <CardContent className="flex flex-col gap-4 pt-6">
                        <div className="flex flex-col gap-2 text-center">
                            <h2 className="text-2xl font-semibold">{copy.newFlow.title}</h2>
                            <p className="text-muted-foreground">{copy.newFlow.subtitle}</p>
                        </div>
                        <GrowthSetupHints />
                        <Tabs
                            onValueChange={(value) => setFlowType(value as 'assistant' | 'automation')}
                            value={flowType}
                        >
                            <TabsList className="grid w-full grid-cols-2">
                                <TabsTrigger
                                    disabled={isLoading}
                                    value="automation"
                                >
                                    {copy.flowTabs.automation}
                                </TabsTrigger>
                                <TabsTrigger
                                    disabled={isLoading}
                                    value="assistant"
                                >
                                    {copy.flowTabs.assistant}
                                </TabsTrigger>
                            </TabsList>
                        </Tabs>
                        <p className="text-muted-foreground -mt-2 text-center text-xs">
                            {flowType === 'automation' ? copy.newFlow.automationHint : copy.newFlow.assistantHint}
                        </p>
                        <FlowForm
                            defaultValues={{
                                message: draft.message,
                                providerName: selectedProvider?.name ?? '',
                                useAgents: shouldUseAgents,
                            }}
                            isSubmitting={isLoading}
                            key={draft.key}
                            onSubmit={handleSubmit}
                            placeholder={
                                !isLoading
                                    ? flowType === 'automation'
                                        ? copy.newFlow.placeholder
                                        : 'What would you like me to help you with?'
                                    : 'Creating a new flow...'
                            }
                            type={flowType}
                        />
                        <div className="flex flex-wrap justify-center gap-2">
                            {copy.newFlow.chips.map((chip) => (
                                <Button
                                    className="rounded-full"
                                    disabled={isLoading}
                                    key={chip}
                                    onClick={() => setDraft((previous) => ({ key: previous.key + 1, message: chip }))}
                                    size="sm"
                                    variant="outline"
                                >
                                    {chip}
                                </Button>
                            ))}
                        </div>
                    </CardContent>
                </Card>
            </div>
        </>
    );
}

export default NewFlow;
