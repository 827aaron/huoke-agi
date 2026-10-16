import { Badge } from '@/components/ui/badge';
import { copy } from '@/lib/copy';

import type { GrowthLead } from '../growth-types';

function LeadScoreBadge({ lead }: { lead: Pick<GrowthLead, 'score' | 'tier'> }) {
    return (
        <Badge variant={lead.tier === 'qualified' ? 'green' : 'yellow'}>
            {lead.score} · {copy.leads.tier[lead.tier]}
        </Badge>
    );
}

export default LeadScoreBadge;
