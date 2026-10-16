import { Mail } from 'lucide-react';
import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import { Spinner } from '@/components/ui/spinner';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';
import { useFlow } from '@/providers/flow-provider';

import { growthApi } from '../growth-api';
import { useGrowthFlow } from '../growth-flow-provider';
import { useGrowthSettings } from '../use-growth-data';

const text = copy.leads.email;

function LeadsEmailButton() {
    const { flowId } = useFlow();
    const { run } = useGrowthFlow();
    const { data: settings } = useGrowthSettings();
    const navigate = useNavigate();
    const [isSending, setIsSending] = useState(false);

    const isConfigured = !!settings?.some((item) => item.kind === 'email' && item.source !== 'none');

    const handleSend = async () => {
        if (!flowId || !run) {
            return;
        }

        if (!isConfigured) {
            toast(text.notConfigured, {
                action: { label: text.notConfiguredLink, onClick: () => navigate(routes.settings.credentials) },
            });

            return;
        }

        setIsSending(true);

        try {
            const result = await growthApi.sendLeadsEmail(flowId, run.task_id);

            if (result.sent) {
                toast.success(text.sent(result.to.join(', ')));
            } else {
                toast.error(text.failed);
            }
        } catch (error) {
            toast.error(getApiErrorMessage(error, text.failed));
        } finally {
            setIsSending(false);
        }
    };

    return (
        <Button
            disabled={!run || isSending}
            onClick={handleSend}
            size="sm"
            variant="outline"
        >
            {isSending ? <Spinner variant="circle" /> : <Mail />}
            {text.send}
        </Button>
    );
}

export default LeadsEmailButton;
