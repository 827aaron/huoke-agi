import { useState } from 'react';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';

import type { GrowthProfileInput } from '../growth-types';

import { growthApi } from '../growth-api';
import { useGrowthFlow } from '../growth-flow-provider';
import { DEFAULT_SCORING } from '../settings/growth-profile-form';

const text = copy.flowProfile.draft;

const joined = (items: null | string[] | undefined) => (items ?? []).filter(Boolean).join('、');

// The profile the flow's assistant drafted, until the owner saves it.
export function GrowthProfileDraftCard() {
    const { flowProfile, profileDraft, profiles, refetchProfiles } = useGrowthFlow();
    const [title, setTitle] = useState('');
    const [busy, setBusy] = useState<'save' | 'use' | null>(null);

    if (!profileDraft || !flowProfile) {
        return null;
    }

    const base = profiles.find((profile) => profile.id === profileDraft.base_profile_id);

    const save = async (useForFlow: boolean) => {
        setBusy(useForFlow ? 'use' : 'save');

        try {
            await growthApi.saveProfileDraft(flowProfile.flow_id, {
                title: title.trim() || undefined,
                use_for_flow: useForFlow,
            });
            toast.success(useForFlow ? text.used : text.saved);
            setTitle('');
            await refetchProfiles();
        } catch (saveError) {
            toast.error(getApiErrorMessage(saveError, copy.common.saveFailed));
        } finally {
            setBusy(null);
        }
    };

    return (
        <Card className="mb-2 gap-3 py-4">
            <CardHeader className="px-4">
                <CardTitle className="text-base">{text.title(profileDraft.title)}</CardTitle>
                {base && <p className="text-muted-foreground text-xs">{text.overwrite(base.title || base.name)}</p>}
            </CardHeader>
            <CardContent className="flex flex-col gap-3 px-4">
                <DraftBlocks profile={profileDraft.profile} />
                <Input
                    aria-label={text.nameLabel}
                    onChange={(event) => setTitle(event.target.value)}
                    placeholder={profileDraft.title || text.nameLabel}
                    value={title}
                />
                <div className="flex flex-wrap justify-end gap-2">
                    <Button
                        disabled={!!busy}
                        onClick={() => save(false)}
                        size="sm"
                        variant="outline"
                    >
                        {text.save}
                    </Button>
                    <Button
                        disabled={!!busy}
                        onClick={() => save(true)}
                        size="sm"
                    >
                        {text.use}
                    </Button>
                </div>
            </CardContent>
        </Card>
    );
}

function Block({ children, title }: { children: React.ReactNode; title: string }) {
    return (
        <div className="flex flex-col gap-1">
            <span className="text-sm font-medium">{title}</span>
            <div className="text-muted-foreground flex flex-col gap-0.5 text-sm">{children}</div>
        </div>
    );
}

function DraftBlocks({ profile }: { profile: GrowthProfileInput }) {
    const { field } = copy.profile;
    const scoring = profile.scoring?.dimensions?.length ? profile.scoring : DEFAULT_SCORING;

    return (
        <>
            <Block title={copy.profile.section.wanted}>
                <Line
                    label={field.name.label}
                    value={profile.name}
                />
                <Line
                    label={field.pitch.label}
                    value={profile.pitch}
                />
                <Line
                    label={field.regions.label}
                    value={joined(profile.regions)}
                />
                <Line
                    label={field.languages.label}
                    value={joined(profile.languages)}
                />
                <Line
                    label={field.signals.label}
                    value={joined(profile.signals)}
                />
            </Block>
            <Block title={copy.profile.section.unwanted}>
                <span>{joined(profile.unwanted) || text.noUnwanted}</span>
                <span className="text-xs">
                    {copy.profile.compliance.title}：{copy.profile.compliance.lines.join('；')}
                </span>
            </Block>
            <Block title={copy.profile.section.scoring}>
                {scoring.dimensions.map((dimension) => (
                    <span key={dimension.key || dimension.name}>
                        {copy.profile.scoring.defaultNames[dimension.name] ?? dimension.name} +{dimension.points}
                    </span>
                ))}
                {!!scoring.recent?.points && (
                    <span>
                        {copy.profile.scoring.recent} +{scoring.recent.points}
                    </span>
                )}
                <span>{text.lines(scoring.qualified, scoring.watchlist)}</span>
            </Block>
        </>
    );
}

function Line({ label, value }: { label: string; value: string }) {
    return value ? (
        <span>
            {label}：{value}
        </span>
    ) : null;
}
