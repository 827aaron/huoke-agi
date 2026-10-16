import { ExternalLink } from 'lucide-react';

import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Sheet, SheetContent, SheetDescription, SheetHeader, SheetTitle } from '@/components/ui/sheet';
import { copy } from '@/lib/copy';

import type { GrowthLead } from '../growth-types';

import { languageLabel } from '../languages';
import LeadScoreBadge from './lead-score-badge';

interface LeadDetailSheetProps {
    lead: GrowthLead | null;
    onOpenChange: (isOpen: boolean) => void;
}

function LeadDetailSheet({ lead, onOpenChange }: LeadDetailSheetProps) {
    const profile = lead?.profile;
    const hasProfile =
        !!profile &&
        (!!profile.bio || !!profile.languages?.length || !!profile.related_groups || !!profile.messages_30d);

    return (
        <Sheet
            onOpenChange={onOpenChange}
            open={!!lead}
        >
            <SheetContent className="flex w-full flex-col gap-0 overflow-y-auto sm:max-w-lg">
                {lead && (
                    <>
                        <SheetHeader>
                            <SheetTitle>{lead.display_name}</SheetTitle>
                            <SheetDescription className="flex flex-wrap items-center gap-2">
                                {lead.username && <span>@{lead.username}</span>}
                                <LeadScoreBadge lead={lead} />
                                {lead.delivered_at && (
                                    <span>
                                        {copy.leads.status.delivered(copy.common.date(new Date(lead.delivered_at)))}
                                    </span>
                                )}
                            </SheetDescription>
                        </SheetHeader>

                        <div className="flex flex-col gap-6 px-4 pb-6">
                            {hasProfile && (
                                <section className="flex flex-col gap-2">
                                    <h3 className="text-sm font-semibold">{copy.leads.detail.profile}</h3>
                                    <div className="text-muted-foreground flex flex-col gap-1 text-sm">
                                        {profile.bio && <p>{profile.bio}</p>}
                                        {!!profile.languages?.length && (
                                            <p>
                                                {copy.leads.detail.profileLanguages(
                                                    profile.languages.map(languageLabel),
                                                )}
                                            </p>
                                        )}
                                        {!!profile.related_groups && (
                                            <p>{copy.leads.detail.profileGroups(profile.related_groups)}</p>
                                        )}
                                        {!!profile.messages_30d && (
                                            <p>{copy.leads.detail.profileActivity30d(profile.messages_30d)}</p>
                                        )}
                                    </div>
                                </section>
                            )}

                            {!!lead.score_breakdown.length && (
                                <section className="flex flex-col gap-2">
                                    <h3 className="text-sm font-semibold">{copy.leads.detail.breakdown}</h3>
                                    <div className="flex flex-wrap gap-1.5">
                                        {lead.score_breakdown.map(({ points, signal }) => (
                                            <Badge
                                                key={signal}
                                                variant="outline"
                                            >
                                                {copy.leads.signal[signal] ?? signal} +{points}
                                            </Badge>
                                        ))}
                                    </div>
                                </section>
                            )}

                            <section className="flex flex-col gap-3">
                                <h3 className="text-sm font-semibold">{copy.leads.detail.evidence}</h3>
                                {lead.evidence.map((evidence) => (
                                    <div
                                        className="flex flex-col gap-2 rounded-md border p-3"
                                        key={evidence.id}
                                    >
                                        <p className="text-sm whitespace-pre-wrap">“{evidence.excerpt}”</p>
                                        <p className="text-muted-foreground text-xs">
                                            {copy.common.relative(new Date(evidence.posted_at))} ·{' '}
                                            {evidence.source_title}
                                        </p>
                                        {evidence.reason && (
                                            <p className="text-muted-foreground text-xs">{evidence.reason}</p>
                                        )}
                                        <Button
                                            asChild
                                            className="self-start"
                                            size="sm"
                                            variant="outline"
                                        >
                                            <a
                                                href={evidence.message_url}
                                                rel="noopener noreferrer"
                                                target="_blank"
                                            >
                                                <ExternalLink />
                                                {copy.leads.detail.openInTelegram}
                                            </a>
                                        </Button>
                                    </div>
                                ))}
                            </section>
                        </div>
                    </>
                )}
            </SheetContent>
        </Sheet>
    );
}

export default LeadDetailSheet;
