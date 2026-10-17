import { format } from 'date-fns';

import { copy } from '@/lib/copy';

import type { GrowthLead } from '../growth-types';

const EVIDENCE_COLUMNS = 3;

const escapeCell = (value: null | number | string | undefined): string => {
    const text = value == null ? '' : String(value);

    return /[",\r\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
};

const formatTime = (value: null | string): string => (value ? format(new Date(value), 'yyyy-MM-dd HH:mm') : '');

export const contactsText = (lead: GrowthLead): string =>
    (lead.contacts ?? [])
        .map((contact) => copy.leads.contactText(copy.profile.contactType[contact.type] ?? contact.type, contact.value))
        .join('; ');

export function buildLeadsCsv(leads: GrowthLead[], sourceLinks: Map<number, string>): string {
    const header = copy.export.header;
    const columns = [
        header.name,
        header.username,
        header.platform,
        header.score,
        header.status,
        header.signals,
        header.contacts,
        header.source,
        header.sourceUrl,
        ...Array.from({ length: EVIDENCE_COLUMNS }, (_, index) => [
            header.evidenceText(index + 1),
            header.evidenceUrl(index + 1),
            header.evidenceTime(index + 1),
        ]).flat(),
        header.lastActive,
        header.bio,
        header.foundIn,
        header.foundRound,
        header.deliveredAt,
    ];

    const rows = leads.map((lead) => [
        lead.display_name,
        lead.username ? `@${lead.username}` : '',
        lead.platform,
        lead.score,
        copy.leads.tier[lead.tier],
        lead.signals.map((signal) => copy.leads.signal[signal] ?? signal).join('、'),
        contactsText(lead),
        lead.source_title,
        sourceLinks.get(lead.source_id) ?? '',
        ...Array.from({ length: EVIDENCE_COLUMNS }, (_, index) => {
            const evidence = lead.evidence[index];

            return [evidence?.excerpt, evidence?.message_url, formatTime(evidence?.posted_at ?? null)];
        }).flat(),
        formatTime(lead.last_active_at),
        lead.profile?.bio,
        lead.first_flow_title,
        lead.first_round,
        formatTime(lead.delivered_at),
    ]);

    // The BOM makes Excel open the UTF-8 file without garbling Chinese.
    return `\uFEFF${[columns, ...rows].map((row) => row.map(escapeCell).join(',')).join('\r\n')}\r\n`;
}

export const leadsCsvFileName = (businessName: string | undefined, date = new Date()): string =>
    copy.export.fileName(
        (businessName?.trim() || copy.export.unnamedBusiness).replaceAll(/[\\/:*?"<>|]/g, '_'),
        format(date, 'yyyyMMdd-HHmm'),
    );
