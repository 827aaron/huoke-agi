import { Download } from 'lucide-react';
import { useMemo } from 'react';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { copy } from '@/lib/copy';
import { Log } from '@/lib/log';
import { downloadTextFile } from '@/lib/report';
import { cn } from '@/lib/utils';

import type { GrowthLead } from '../growth-types';

import { useGrowthFlow } from '../growth-flow-provider';
import { buildLeadsCsv, leadsCsvFileName } from './leads-csv';

interface LeadsExportMenuProps {
    filteredLeads?: GrowthLead[];
    size?: 'default' | 'sm';
    variant?: 'default' | 'outline';
}

function LeadsExportMenu({ filteredLeads, size = 'sm', variant = 'default' }: LeadsExportMenuProps) {
    const { leads, profile, run, sources } = useGrowthFlow();

    const options = useMemo(() => {
        const roundDelivered = run ? leads.filter((lead) => lead.delivered_task_id === run.task_id) : [];

        return [
            { id: 'round', label: copy.export.currentRoundDelivered, leads: roundDelivered },
            { id: 'delivered', label: copy.export.allDelivered, leads: leads.filter((lead) => lead.delivered_at) },
            { id: 'filtered', label: copy.export.filtered, leads: filteredLeads ?? leads },
            {
                id: 'qualified',
                label: copy.export.allQualified,
                leads: leads.filter((lead) => lead.tier === 'qualified'),
            },
        ];
    }, [filteredLeads, leads, run]);

    const defaultId = options[0]?.leads.length ? 'round' : 'filtered';

    const handleExport = (items: GrowthLead[]) => {
        try {
            const links = new Map(sources.map((source) => [source.id, source.link]));

            downloadTextFile(buildLeadsCsv(items, links), leadsCsvFileName(profile?.name), 'text/csv;charset=utf-8');
        } catch (error) {
            Log.error('Failed to export leads:', error);
            toast.error(copy.export.failed);
        }
    };

    return (
        <DropdownMenu>
            <DropdownMenuTrigger asChild>
                <Button
                    size={size}
                    variant={variant}
                >
                    <Download />
                    {copy.export.button}
                </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
                {options.map((option) => (
                    <DropdownMenuItem
                        className={cn(option.id === defaultId && 'font-medium')}
                        disabled={!option.leads.length}
                        key={option.id}
                        onClick={() => handleExport(option.leads)}
                        title={option.leads.length ? undefined : copy.export.nothing}
                    >
                        {option.label}
                        <span className="text-muted-foreground ml-auto pl-4 text-xs">{option.leads.length}</span>
                    </DropdownMenuItem>
                ))}
            </DropdownMenuContent>
        </DropdownMenu>
    );
}

export default LeadsExportMenu;
