import { useCallback, useState } from 'react';

import { useFlow } from '@/providers/flow-provider';

// The quick actions are buttons the customer presses, so they submit straight away with the flow's own
// provider instead of filling the input first.
export function useSubmitGrowthMessage() {
    const { flowData, submitAutomationMessage } = useFlow();
    const [isSubmitting, setIsSubmitting] = useState(false);
    const providerName = flowData?.flow?.provider?.name ?? '';

    const submit = useCallback(
        async (message: string) => {
            setIsSubmitting(true);

            try {
                return await submitAutomationMessage({ message, providerName, resourceIds: [], useAgents: false });
            } finally {
                setIsSubmitting(false);
            }
        },
        [providerName, submitAutomationMessage],
    );

    return { isSubmitting, submit };
}
