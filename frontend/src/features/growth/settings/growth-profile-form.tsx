import { ChevronDown, ChevronUp, Plus, Trash2 } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import { Alert, AlertDescription } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';

import type { GrowthContactType, GrowthProfile, GrowthProfileInput, GrowthProfileSection } from '../growth-types';

import { growthApi } from '../growth-api';
import { parseScoring, type ScoringIssue } from '../profile-scoring';

export const CONTACT_TYPES: GrowthContactType[] = [
    'platform_handle',
    'phone',
    'email',
    'whatsapp',
    'line',
    'telegram',
    'other',
];

const MAX_PROMPT_RUNES = 8000;
const MAX_SECTIONS = 20;
const MAX_SECTION_TITLE = 80;

const { field } = copy.profile;

interface Draft {
    exclude: string;
    need: string;
    scoring: string;
    sections: GrowthProfileSection[];
    title: string;
}

const runeCount = (text: string) => [...text].length;

const clip = (text: string, max: number) => [...text].slice(0, max).join('');

const nextSectionId = (sections: GrowthProfileSection[]) => {
    const used = sections.reduce((max, section) => {
        const match = /^s(\d+)$/.exec(section.id);

        return match ? Math.max(max, Number(match[1])) : max;
    }, 0);

    return `s${used + 1}`;
};

const fromProfile = (profile: GrowthProfile | null): Draft => ({
    exclude: profile?.exclude ?? '',
    need: profile?.need ?? '',
    scoring: profile?.scoring ?? '',
    sections: (profile?.sections ?? []).map((section) => ({ ...section })),
    title: profile?.title ?? '',
});

const scoringError = (issue: ScoringIssue): string => {
    const text = copy.profile.scoring;

    switch (issue.kind) {
        case 'line':
            return text.line(issue.line);
        case 'name':
            return text.name;
        case 'no_dimension':
            return text.noDimension;
        case 'points':
            return text.points(issue.line);
        case 'unreachable':
            return text.unreachable;
        case 'watchlist':
            return text.watchlist;
    }
};

const problems = (draft: Draft): { form?: string; scoring?: string } => {
    const tooLong = [draft.need, draft.exclude, draft.scoring, ...draft.sections.map((section) => section.body)].some(
        (text) => runeCount(text) > MAX_PROMPT_RUNES,
    );

    if (tooLong) {
        return { form: copy.profile.promptTooLong(MAX_PROMPT_RUNES) };
    }

    if (draft.sections.length > MAX_SECTIONS) {
        return { form: copy.profile.tooManySections(MAX_SECTIONS) };
    }

    if (draft.sections.some((section) => section.body.trim() && !section.title.trim())) {
        return { form: copy.profile.sectionTitleRequired };
    }

    const scoring = draft.scoring.trim();

    if (scoring) {
        const parsed = parseScoring(scoring);

        if ('error' in parsed) {
            return { scoring: scoringError(parsed.error) };
        }
    }

    const sections = draft.sections.filter((section) => section.title.trim() || section.body.trim());

    if (!draft.need.trim() && !draft.exclude.trim() && !sections.length) {
        return { form: copy.profile.requiredPrompt };
    }

    return {};
};

interface GrowthProfileFormProps {
    onSaved: (profile: GrowthProfile) => void;
    profile: GrowthProfile | null;
}

export function GrowthProfileForm({ onSaved, profile }: GrowthProfileFormProps) {
    const [draft, setDraft] = useState<Draft>(() => fromProfile(profile));
    const [baseline, setBaseline] = useState(() => JSON.stringify(fromProfile(profile)));
    const [editedBy, setEditedBy] = useState(profile?.edited_by);
    const [hasPrevious, setHasPrevious] = useState(profile?.has_previous);
    const [error, setError] = useState<{ form?: string; scoring?: string }>({});
    const [saving, setSaving] = useState(false);

    const dirty = JSON.stringify(draft) !== baseline;

    const setField = (patch: Partial<Draft>) => setDraft((current) => ({ ...current, ...patch }));

    const fillExample = async () => {
        try {
            const presets = await growthApi.listProfilePresets();
            const preset = presets.find((item) => item.default) ?? presets[0];

            if (!preset) {
                return;
            }

            setField({
                exclude: preset.profile.exclude ?? '',
                need: preset.profile.need ?? '',
                scoring: preset.profile.scoring ?? '',
                sections: preset.profile.sections ?? [],
            });
        } catch (fillError) {
            toast.error(getApiErrorMessage(fillError, copy.common.saveFailed));
        }
    };

    const save = async () => {
        const found = problems(draft);

        setError(found);

        if (found.form || found.scoring) {
            return;
        }

        const sections = draft.sections
            .map((section) => ({
                body: section.body.trim(),
                id: section.id,
                title: clip(section.title.trim(), MAX_SECTION_TITLE),
            }))
            .filter((section) => section.title || section.body);
        const input: GrowthProfileInput = {
            exclude: draft.exclude,
            name: draft.title.trim(),
            need: draft.need,
            scoring: draft.scoring,
            sections,
            title: draft.title.trim(),
        };

        setSaving(true);

        try {
            const saved = profile
                ? await growthApi.updateProfile(profile.id, input)
                : await growthApi.createProfile(input);

            const next = fromProfile(saved);

            setDraft(next);
            setBaseline(JSON.stringify(next));
            setEditedBy(saved.edited_by);
            setHasPrevious(saved.has_previous);
            toast.success(copy.profile.saved);
            onSaved(saved);
        } catch (saveError) {
            toast.error(getApiErrorMessage(saveError, copy.common.saveFailed));
        } finally {
            setSaving(false);
        }
    };

    const restore = async () => {
        if (!profile) {
            return;
        }

        setSaving(true);

        try {
            const saved = await growthApi.restoreProfile(profile.id);
            const next = fromProfile(saved);

            setDraft(next);
            setBaseline(JSON.stringify(next));
            setEditedBy(saved.edited_by);
            setHasPrevious(saved.has_previous);
            toast.success(copy.profile.restored);
            onSaved(saved);
        } catch (restoreError) {
            toast.error(getApiErrorMessage(restoreError, copy.common.saveFailed));
        } finally {
            setSaving(false);
        }
    };

    const move = (index: number, delta: number) => {
        setDraft((current) => {
            const sections = [...current.sections];
            const target = index + delta;
            const section = sections[index];

            if (!section || target < 0 || target >= sections.length) {
                return current;
            }

            sections.splice(index, 1);
            sections.splice(target, 0, section);

            return { ...current, sections };
        });
    };

    return (
        <form
            className="flex flex-col gap-4"
            onSubmit={(event) => {
                event.preventDefault();
                void save();
            }}
        >
            {editedBy === 'assistant' && (
                <Alert>
                    <AlertDescription className="flex flex-wrap items-center justify-between gap-2">
                        <span>{copy.profile.assistantEdited}</span>
                        {hasPrevious && (
                            <Button
                                disabled={saving}
                                onClick={() => void restore()}
                                size="sm"
                                type="button"
                                variant="outline"
                            >
                                {copy.profile.restore}
                            </Button>
                        )}
                    </AlertDescription>
                </Alert>
            )}
            <div className="flex flex-col gap-2">
                <Label htmlFor="profile-title">{field.title.label}</Label>
                <Input
                    id="profile-title"
                    onChange={(event) => setField({ title: event.target.value })}
                    placeholder={field.title.placeholder}
                    value={draft.title}
                />
                <p className="text-muted-foreground text-xs">{field.title.help}</p>
            </div>
            <p className="text-muted-foreground text-sm">{copy.profile.aiHint}</p>
            <Prompt
                help={field.need.help}
                id="profile-need"
                label={field.need.label}
                onChange={(need) => setField({ need })}
                placeholder={field.need.placeholder}
                value={draft.need}
            />
            <Prompt
                help={field.exclude.help}
                id="profile-exclude"
                label={field.exclude.label}
                onChange={(exclude) => setField({ exclude })}
                placeholder={field.exclude.placeholder}
                value={draft.exclude}
            />
            <p className="text-muted-foreground text-xs">
                <span className="font-medium">{copy.profile.compliance.title}</span>
                {copy.profile.compliance.lines.join('；')}
            </p>
            <Prompt
                error={error.scoring}
                help={field.scoring.help}
                id="profile-scoring"
                label={field.scoring.label}
                onChange={(scoring) => setField({ scoring })}
                placeholder={field.scoring.placeholder}
                value={draft.scoring}
            />
            <p className="text-muted-foreground text-xs">{copy.profile.scoring.format}</p>
            <p className="text-muted-foreground text-xs">{copy.profile.scoring.hint}</p>
            {draft.sections.map((section, index) => (
                <div
                    className="flex flex-col gap-2 rounded-md border p-3"
                    key={section.id}
                >
                    <div className="flex gap-2">
                        <Input
                            aria-label={`${field.sectionTitle.label} ${index + 1}`}
                            onChange={(event) =>
                                setField({
                                    sections: draft.sections.map((item) =>
                                        item.id === section.id ? { ...item, title: event.target.value } : item,
                                    ),
                                })
                            }
                            placeholder={field.sectionTitle.placeholder}
                            value={section.title}
                        />
                        <Button
                            aria-label={copy.profile.moveUp}
                            disabled={index === 0}
                            onClick={() => move(index, -1)}
                            size="icon"
                            type="button"
                            variant="ghost"
                        >
                            <ChevronUp />
                        </Button>
                        <Button
                            aria-label={copy.profile.moveDown}
                            disabled={index === draft.sections.length - 1}
                            onClick={() => move(index, 1)}
                            size="icon"
                            type="button"
                            variant="ghost"
                        >
                            <ChevronDown />
                        </Button>
                        <Button
                            aria-label={copy.profile.sectionRemove}
                            onClick={() =>
                                setField({ sections: draft.sections.filter((item) => item.id !== section.id) })
                            }
                            size="icon"
                            type="button"
                            variant="ghost"
                        >
                            <Trash2 />
                        </Button>
                    </div>
                    <Textarea
                        aria-label={`${field.sectionBody.label} ${index + 1}`}
                        maxHeight={480}
                        minHeight={120}
                        onChange={(event) =>
                            setField({
                                sections: draft.sections.map((item) =>
                                    item.id === section.id ? { ...item, body: event.target.value } : item,
                                ),
                            })
                        }
                        placeholder={field.sectionBody.placeholder}
                        value={section.body}
                    />
                    <p className="text-muted-foreground text-xs">
                        {copy.profile.chars(runeCount(section.body), MAX_PROMPT_RUNES)}
                    </p>
                </div>
            ))}
            <div className="flex flex-wrap gap-2">
                <Button
                    className="self-start"
                    disabled={draft.sections.length >= MAX_SECTIONS}
                    onClick={() =>
                        setField({
                            sections: [...draft.sections, { body: '', id: nextSectionId(draft.sections), title: '' }],
                        })
                    }
                    size="sm"
                    type="button"
                    variant="outline"
                >
                    <Plus />
                    {copy.profile.sectionAdd}
                </Button>
                <Button
                    className="self-start"
                    onClick={() => void fillExample()}
                    size="sm"
                    type="button"
                    variant="outline"
                >
                    {copy.profile.example}
                </Button>
            </div>
            {error.form && <p className="text-destructive text-sm">{error.form}</p>}
            <div className="flex items-center justify-end gap-3">
                {dirty && <p className="text-muted-foreground text-xs">{copy.profile.unsaved}</p>}
                <Button
                    disabled={saving}
                    size="sm"
                    type="submit"
                >
                    {copy.profile.save}
                </Button>
            </div>
        </form>
    );
}

function Prompt({
    error,
    help,
    id,
    label,
    onChange,
    placeholder,
    value,
}: {
    error?: string;
    help: string;
    id: string;
    label: string;
    onChange: (value: string) => void;
    placeholder: string;
    value: string;
}) {
    return (
        <div className="flex flex-col gap-2">
            <Label htmlFor={id}>{label}</Label>
            <Textarea
                id={id}
                maxHeight={480}
                minHeight={120}
                onChange={(event) => onChange(event.target.value)}
                placeholder={placeholder}
                value={value}
            />
            <div className="text-muted-foreground flex justify-between gap-3 text-xs">
                <span>{help}</span>
                <span>{copy.profile.chars(runeCount(value), MAX_PROMPT_RUNES)}</span>
            </div>
            {error && <p className="text-destructive text-sm">{error}</p>}
        </div>
    );
}
