import { format } from 'date-fns';

import { copy } from '@/lib/copy';

import type { GrowthLead, GrowthLeadEvidence } from '../growth-types';

export interface LeadsCsvOptions {
    // Reads a signal key as the list page shows it: a built-in label or a profile dimension's name.
    signalLabel?: (signal: string) => string;
    // Source link by source id, the fallback when a piece of evidence has no message link.
    sourceLinks?: Map<number, string>;
}

const escapeCell = (value: null | number | string | undefined): string => {
    const text = value == null ? '' : String(value);

    return /[",\r\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
};

const formatTime = (value: null | string | undefined): string =>
    value ? format(new Date(value), 'yyyy-MM-dd HH:mm') : '';

export const contactsText = (lead: GrowthLead): string =>
    (lead.contacts ?? [])
        .map((contact) => copy.leads.contactText(copy.profile.contactType[contact.type] ?? contact.type, contact.value))
        .join('; ');

// The way to reach a lead that has no contact of its own: its Telegram handle, or the
// profile link a web lead is identified by.
const profileLink = (lead: GrowthLead): string => {
    if (lead.platform === 'telegram' && lead.username) {
        return `https://t.me/${lead.username.replace(/^@/, '')}`;
    }

    if (lead.platform_user_id.includes('://')) {
        return lead.platform_user_id;
    }

    if (lead.platform === 'web' && lead.platform_user_id.includes('/')) {
        return `https://${lead.platform_user_id}`;
    }

    return '';
};

const reachCell = (lead: GrowthLead): string => contactsText(lead) || profileLink(lead);

const newestEvidence = (lead: GrowthLead): GrowthLeadEvidence | null =>
    lead.evidence.reduce<GrowthLeadEvidence | null>(
        (latest, item) => (!latest || item.posted_at > latest.posted_at ? item : latest),
        null,
    );

export function buildLeadsCsv(leads: GrowthLead[], { signalLabel, sourceLinks }: LeadsCsvOptions = {}): string {
    const header = copy.export.header;
    const columns = [
        header.name,
        header.username,
        header.platform,
        header.contacts,
        header.score,
        header.signals,
        header.reason,
        header.source,
        header.quote,
        header.quoteUrl,
        header.quoteTime,
    ];

    const rows = leads.map((lead) => {
        const evidence = newestEvidence(lead);

        return [
            lead.display_name,
            lead.username ? `@${lead.username}` : lead.platform_user_id,
            copy.telegram.health.platform[lead.platform] ?? lead.platform,
            reachCell(lead),
            copy.leads.scoreText(lead.score, copy.leads.tier[lead.tier]),
            lead.signals.map((signal) => (signalLabel ?? ((key) => copy.leads.signal[key] ?? key))(signal)).join('、'),
            lead.match_reason ?? '',
            evidence?.source_title || lead.source_title,
            evidence?.excerpt,
            evidence?.message_url || sourceLinks?.get(evidence?.source_id ?? lead.source_id) || '',
            formatTime(evidence?.posted_at),
        ];
    });

    // The BOM makes Excel open the UTF-8 file without garbling Chinese.
    return `\uFEFF${[columns, ...rows].map((row) => row.map(escapeCell).join(',')).join('\r\n')}\r\n`;
}

const fileSafe = (text: string): string => text.replaceAll(/[\\/:*?"<>|\s]+/g, '_');

export const leadsCsvFileName = (businessName: string | undefined, scope: string, date = new Date()): string =>
    copy.export.fileName(
        fileSafe(businessName?.trim() || copy.export.unnamedBusiness),
        fileSafe(scope),
        format(date, 'yyyyMMdd-HHmm'),
    );
