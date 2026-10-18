import { Check, Plus, Trash2 } from 'lucide-react';
import { useEffect, useState } from 'react';
import { useFieldArray } from 'react-hook-form';
import { toast } from 'sonner';
import * as z from 'zod';

import { Button } from '@/components/ui/button';
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form';
import { FormSubmitButton } from '@/components/ui/form-submit-button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group';
import { useAppForm } from '@/hooks/use-app-form';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';

import type {
    GrowthContactType,
    GrowthProfile,
    GrowthProfileInput,
    GrowthProfilePreset,
    GrowthScoring,
} from '../growth-types';

import { growthApi } from '../growth-api';
import { LANGUAGE_CODES, languageCode } from '../languages';

const { field, required } = copy.profile;

export const splitList = (text: string): string[] => [
    ...new Set(
        text
            .split(/[\n,，、;；]+/)
            .map((item) => item.trim())
            .filter(Boolean),
    ),
];

// The "other" chip only reveals the free-text field; what is typed there is saved as written.
const OTHER_LANGUAGE = 'other';

const selectedLanguages = (values: { languages: string[]; otherLanguages: string }): string[] => [
    ...new Set([
        ...values.languages.filter((language) => language !== OTHER_LANGUAGE),
        ...(values.languages.includes(OTHER_LANGUAGE) ? splitList(values.otherLanguages) : []),
    ]),
];

const languageFormValues = (languages: null | string[] = []) => {
    languages ??= [];
    const codes = languages.map(languageCode).filter((code): code is string => !!code);
    const others = languages.filter((language) => !languageCode(language));

    return {
        languages: others.length ? [...codes, OTHER_LANGUAGE] : codes,
        otherLanguages: others.join('、'),
    };
};

export const CONTACT_TYPES: GrowthContactType[] = [
    'platform_handle',
    'phone',
    'email',
    'whatsapp',
    'line',
    'telegram',
    'other',
];

const MAX_CUSTOM_CONTACT_LENGTH = 30;

const UNWANTED_PENALTY = 3;

const isCustomContact = (type: string) => !CONTACT_TYPES.includes(type as GrowthContactType);

const scoringText = copy.profile.scoring;

// The server's weighting for a profile that sets none; the descriptions are what the judgement model reads.
export const DEFAULT_SCORING: GrowthScoring = {
    dimensions: [
        {
            description: 'The sender uses or follows this kind of product or service themselves.',
            key: 'player',
            name: scoringText.weight.player ?? 'player',
            points: 3,
        },
        {
            description:
                'The sender is unhappy with, bored of or leaving what they use now, or asks for an alternative.',
            key: 'pain',
            name: scoringText.weight.pain ?? 'pain',
            points: 3,
        },
        {
            description:
                'The sender mentions paying for this kind of product: purchases, deposits, top-ups, subscriptions.',
            key: 'paying',
            name: scoringText.weight.paying ?? 'paying',
            points: 2,
        },
        {
            description: 'What the sender wants or uses fits the offer: its category, platform, region and style.',
            key: 'match',
            name: scoringText.weight.match ?? 'match',
            points: 3,
        },
    ],
    qualified: 7,
    recent: { points: 2 },
    watchlist: 4,
};

const MAX_DIMENSION_POINTS = 20;

// The server names the built-in dimensions in English; the page shows them under the names the owner reads.
const dimensionName = (name: string) => scoringText.defaultNames[name] ?? name;

const wholeNumber = z.string().trim().regex(/^\d+$/, { message: scoringText.invalidWeight });

const joinList = (items: string[] | undefined): string => (items ?? []).join('\n');

const nonEmptyList = (message: string) => z.string().refine((value) => splitList(value).length > 0, { message });

const profileFormSchema = z
    .object({
        competitors: z.string(),
        contactTypes: z.array(z.string()),
        defaultTarget: z
            .string()
            .trim()
            .regex(/^\d+$/, { message: copy.profile.targetRange })
            .refine((value) => Number(value) >= 1 && Number(value) <= 1000, { message: copy.profile.targetRange }),
        dimensions: z.array(
            z.object({
                description: z.string(),
                key: z.string(),
                name: z.string().trim().min(1, { message: scoringText.nameRequired }),
                points: wholeNumber,
            }),
        ),
        games: nonEmptyList(required.games),
        genre: z.string().trim().min(1, { message: required.genre }),
        languages: z.array(z.string()),
        name: z.string().trim().min(1, { message: required.name }),
        notes: z.string(),
        otherLanguages: z.string(),
        pitch: z.string().trim().min(1, { message: required.pitch }),
        qualified: wholeNumber,
        recent: wholeNumber,
        regions: nonEmptyList(required.regions),
        signals: z.string(),
        title: z.string(),
        unwanted: z.string(),
        unwantedPenalty: wholeNumber,
        watchlist: wholeNumber,
    })
    .refine((values) => selectedLanguages(values).length > 0, { message: required.languages, path: ['languages'] })
    .superRefine((values, context) => {
        const points = values.dimensions.map((dimension) => Number(dimension.points));
        const qualified = Number(values.qualified);
        const watchlist = Number(values.watchlist);

        values.dimensions.forEach((_, index) => {
            if ((points[index] ?? 0) > MAX_DIMENSION_POINTS) {
                context.addIssue({
                    code: 'custom',
                    message: scoringText.invalidWeight,
                    path: ['dimensions', index, 'points'],
                });
            }
        });

        if (Number(values.recent) > MAX_DIMENSION_POINTS) {
            context.addIssue({ code: 'custom', message: scoringText.invalidWeight, path: ['recent'] });
        }

        if (!values.dimensions.length) {
            context.addIssue({ code: 'custom', message: scoringText.noDimension, path: ['dimensions'] });
        } else if (watchlist < 1 || qualified <= watchlist) {
            context.addIssue({ code: 'custom', message: scoringText.invalidLines, path: ['watchlist'] });
        } else if (points.reduce((sum, point) => sum + point, Number(values.recent)) < qualified) {
            context.addIssue({ code: 'custom', message: scoringText.unreachable, path: ['qualified'] });
        }
    });

type FormProfile = Omit<GrowthProfileInput, 'unwanted' | 'unwanted_penalty'> &
    Partial<Pick<GrowthProfileInput, 'unwanted' | 'unwanted_penalty'>>;

type ProfileFormValues = z.infer<typeof profileFormSchema>;

const toFormValues = (profile: FormProfile | null): ProfileFormValues => {
    const scoring = profile?.scoring?.dimensions?.length ? profile.scoring : DEFAULT_SCORING;

    return {
        competitors: joinList(profile?.competitors),
        contactTypes: profile?.contact_types?.length ? profile.contact_types : ['platform_handle'],
        defaultTarget: String(profile?.default_target ?? 50),
        dimensions: scoring.dimensions.map((dimension) => ({
            description: dimension.description,
            key: dimension.key,
            name: dimensionName(dimension.name),
            points: String(dimension.points),
        })),
        games: joinList(profile?.games),
        genre: profile?.genre ?? '',
        ...languageFormValues(profile?.languages),
        name: profile?.name ?? '',
        notes: profile?.notes ?? '',
        pitch: profile?.pitch ?? '',
        qualified: String(scoring.qualified),
        recent: String(scoring.recent?.points ?? 0),
        regions: joinList(profile?.regions),
        signals: joinList(profile?.signals),
        title: profile?.title ?? '',
        unwanted: joinList(profile?.unwanted),
        unwantedPenalty: String(profile?.unwanted_penalty || UNWANTED_PENALTY),
        watchlist: String(scoring.watchlist),
    };
};

const toInput = (values: ProfileFormValues): GrowthProfileInput => ({
    competitors: splitList(values.competitors),
    contact_types: values.contactTypes,
    default_target: Number(values.defaultTarget),
    games: splitList(values.games),
    genre: values.genre.trim(),
    languages: selectedLanguages(values),
    name: values.name.trim(),
    notes: values.notes.trim(),
    pitch: values.pitch.trim(),
    regions: splitList(values.regions),
    scoring: {
        dimensions: values.dimensions.map((dimension) => ({
            description: dimension.description.trim(),
            key: dimension.key,
            name: dimension.name.trim(),
            points: Number(dimension.points),
        })),
        qualified: Number(values.qualified),
        recent: Number(values.recent) ? { points: Number(values.recent) } : null,
        watchlist: Number(values.watchlist),
    },
    signals: splitList(values.signals),
    title: values.title.trim(),
    unwanted: splitList(values.unwanted),
    unwanted_penalty: Number(values.unwantedPenalty),
});

// The title names the saved profile, so a preset never fills it.
type ProfileField = Exclude<keyof GrowthProfileInput, 'title'>;

// The form fields each profile field fills. Typed as a Record so a field the server adds cannot be
// left out of a preset by accident.
export const PRESET_FORM_FIELDS: Record<ProfileField, (keyof ProfileFormValues)[]> = {
    competitors: ['competitors'],
    contact_types: ['contactTypes'],
    default_target: ['defaultTarget'],
    games: ['games'],
    genre: ['genre'],
    languages: ['languages', 'otherLanguages'],
    name: ['name'],
    notes: ['notes'],
    pitch: ['pitch'],
    regions: ['regions'],
    scoring: ['dimensions', 'recent', 'qualified', 'watchlist'],
    signals: ['signals'],
    unwanted: ['unwanted'],
    unwanted_penalty: ['unwantedPenalty'],
};

const isBlank = (value: unknown) =>
    value === null || value === undefined || value === '' || value === 0 || (Array.isArray(value) && !value.length);

// A field the preset leaves empty keeps what the owner typed, such as the name and the regions, and is
// returned in `uncovered` so the form can say so.
export const withPreset = (
    current: ProfileFormValues,
    { profile }: GrowthProfilePreset,
): { uncovered: Set<ProfileField>; values: ProfileFormValues } => {
    const fields = Object.keys(PRESET_FORM_FIELDS) as ProfileField[];
    const uncovered = new Set(fields.filter((name) => isBlank(profile[name])));
    const preset = toFormValues(
        Object.fromEntries(fields.map((name) => [name, profile[name] ?? undefined])) as GrowthProfileInput,
    );
    const values = { ...current };

    fields
        .filter((name) => !uncovered.has(name))
        .flatMap((name) => PRESET_FORM_FIELDS[name])
        .forEach((name) => Object.assign(values, { [name]: preset[name] }));

    return { uncovered, values };
};

interface GrowthProfileFormProps {
    onSaved: (profile: GrowthProfile) => void;
    profile: GrowthProfile | null;
}

const requiredMark = (name: string) => (name in required ? ' *' : '');

type TextFieldName =
    | 'competitors'
    | 'games'
    | 'genre'
    | 'name'
    | 'notes'
    | 'pitch'
    | 'regions'
    | 'signals'
    | 'title'
    | 'unwanted';

export function GrowthProfileForm({ onSaved, profile }: GrowthProfileFormProps) {
    const [error, setError] = useState<null | string>(null);

    const form = useAppForm<ProfileFormValues>({
        defaultValues: toFormValues(profile),
        schema: profileFormSchema,
    });
    const dimensions = useFieldArray({ control: form.control, name: 'dimensions' });

    const [presets, setPresets] = useState<GrowthProfilePreset[]>([]);
    const [uncovered, setUncovered] = useState<Set<ProfileField>>(new Set());
    const [presetId, setPresetId] = useState('');
    const [customContacts, setCustomContacts] = useState<string[]>(
        () => profile?.contact_types?.filter(isCustomContact) ?? [],
    );
    const [customDraft, setCustomDraft] = useState('');
    const [customError, setCustomError] = useState<null | string>(null);

    const addCustomContact = () => {
        const name = customDraft.trim();

        if (!name) {
            return;
        }

        if ([...name].length > MAX_CUSTOM_CONTACT_LENGTH) {
            setCustomError(field.contactTypes.customTooLong(MAX_CUSTOM_CONTACT_LENGTH));

            return;
        }

        const sameName = (other: string) => other.toLocaleLowerCase() === name.toLocaleLowerCase();
        const existing =
            CONTACT_TYPES.find((type) => sameName(type) || sameName(copy.profile.contactType[type] ?? '')) ??
            customContacts.find(sameName);
        const selected = form.getValues('contactTypes');

        if (!existing) {
            setCustomContacts((previous) => [...previous, name]);
        }

        const type = existing ?? name;

        if (!selected.includes(type)) {
            form.setValue('contactTypes', [...selected, type], { shouldDirty: true });
        }

        setCustomDraft('');
        setCustomError(null);
    };

    const fill = (preset: GrowthProfilePreset, keepTyped = false) => {
        const current = form.getValues();
        const next = withPreset(current, preset);

        // The presets load after the form shows, so the default one must not overwrite what the owner
        // typed meanwhile; picking a preset by hand does.
        if (keepTyped) {
            const blank = toFormValues(null);

            (Object.keys(blank) as (keyof ProfileFormValues)[])
                .filter((name) => JSON.stringify(current[name]) !== JSON.stringify(blank[name]))
                .forEach((name) => Object.assign(next.values, { [name]: current[name] }));
        }

        form.reset(next.values, { keepDefaultValues: true });
        setUncovered(next.uncovered);
    };

    useEffect(() => {
        let isCancelled = false;

        growthApi
            .listProfilePresets()
            .then((list) => {
                if (isCancelled) {
                    return;
                }

                const initial = list.find((preset) => preset.default) ?? list[0];

                setPresets(list);

                if (initial && !profile) {
                    setPresetId(initial.id);
                    fill(initial, true);
                }
            })
            .catch(() => {});

        return () => {
            isCancelled = true;
        };
        // Runs once: a new profile starts from the default preset; a saved one is left as it is.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    const handlePreset = (id: string) => {
        const preset = presets.find((item) => item.id === id);

        setPresetId(id);

        if (preset) {
            fill(preset);
        }
    };

    const handleSubmit = async (values: ProfileFormValues) => {
        setError(null);

        try {
            const input = toInput(values);
            const saved = profile
                ? await growthApi.updateProfile(profile.id, input)
                : await growthApi.createProfile(input);

            toast.success(copy.profile.saved);
            form.reset(toFormValues(saved));
            onSaved(saved);
        } catch (saveError) {
            setError(getApiErrorMessage(saveError, copy.common.saveFailed));
        }
    };

    const notCovered = (name: ProfileField) =>
        uncovered.has(name) && (
            <p className="text-xs text-yellow-800 dark:text-yellow-300">{copy.profile.preset.notCovered}</p>
        );

    const renderNumber = (name: 'qualified' | 'recent' | 'unwantedPenalty' | 'watchlist', label: string) => (
        <FormField
            control={form.control}
            name={name}
            render={({ field: input }) => (
                <FormItem>
                    <FormLabel>{label}</FormLabel>
                    <FormControl>
                        <Input
                            {...input}
                            className="w-24"
                            inputMode="numeric"
                        />
                    </FormControl>
                    <FormMessage />
                </FormItem>
            )}
        />
    );

    const renderText = (name: TextFieldName, multiline = false) => (
        <FormField
            control={form.control}
            name={name}
            render={({ field: input }) => (
                <FormItem>
                    <FormLabel>
                        {field[name].label}
                        {requiredMark(name)}
                    </FormLabel>
                    <FormControl>
                        {multiline ? (
                            <Textarea
                                {...input}
                                placeholder={field[name].placeholder}
                                rows={2}
                            />
                        ) : (
                            <Input
                                {...input}
                                placeholder={field[name].placeholder}
                            />
                        )}
                    </FormControl>
                    <FormDescription>{field[name].help}</FormDescription>
                    {name !== 'title' && notCovered(name)}
                    <FormMessage />
                </FormItem>
            )}
        />
    );

    return (
        <Form {...form}>
            <form
                className="flex flex-col gap-5"
                noValidate
                onSubmit={form.handleSubmit(handleSubmit)}
            >
                {renderText('title')}
                <h3 className="border-t pt-4 text-base font-semibold">{copy.profile.section.wanted}</h3>
                <p className="text-muted-foreground text-sm">{copy.profile.aiHint}</p>
                {presets.length > 0 && (
                    <div className="flex flex-col gap-2">
                        <Label htmlFor="growth-profile-preset">{copy.profile.preset.label}</Label>
                        <Select
                            onValueChange={handlePreset}
                            value={presetId || undefined}
                        >
                            <SelectTrigger
                                className="w-full"
                                id="growth-profile-preset"
                            >
                                <SelectValue placeholder={copy.profile.preset.placeholder} />
                            </SelectTrigger>
                            <SelectContent>
                                {presets.map((preset) => (
                                    <SelectItem
                                        key={preset.id}
                                        value={preset.id}
                                    >
                                        {copy.profile.preset.names[preset.id] ?? preset.id}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                        <p className="text-muted-foreground text-xs">{copy.profile.preset.help}</p>
                    </div>
                )}
                {renderText('name')}
                {renderText('games', true)}
                {renderText('genre')}
                {renderText('pitch', true)}
                {renderText('regions')}
                <FormField
                    control={form.control}
                    name="languages"
                    render={({ field: input }) => (
                        <FormItem>
                            <FormLabel>
                                {field.languages.label}
                                {requiredMark('languages')}
                            </FormLabel>
                            <FormControl>
                                <ToggleGroup
                                    className="flex-wrap justify-start"
                                    onValueChange={input.onChange}
                                    type="multiple"
                                    value={input.value}
                                    variant="outline"
                                >
                                    {[...LANGUAGE_CODES, OTHER_LANGUAGE].map((code) => (
                                        <ToggleGroupItem
                                            className="group data-[state=on]:bg-primary data-[state=on]:text-primary-foreground data-[state=on]:hover:bg-primary/90 data-[state=on]:hover:text-primary-foreground px-3"
                                            key={code}
                                            value={code}
                                        >
                                            <Check className="hidden size-3.5 group-data-[state=on]:block" />
                                            {code === OTHER_LANGUAGE
                                                ? copy.profile.otherLanguage
                                                : copy.profile.languages[code]}
                                        </ToggleGroupItem>
                                    ))}
                                </ToggleGroup>
                            </FormControl>
                            {input.value.includes(OTHER_LANGUAGE) && (
                                <FormField
                                    control={form.control}
                                    name="otherLanguages"
                                    render={({ field: other }) => (
                                        <Input
                                            aria-label={copy.profile.otherLanguage}
                                            placeholder={copy.profile.otherLanguagePlaceholder}
                                            {...other}
                                        />
                                    )}
                                />
                            )}
                            <FormDescription>{field.languages.help}</FormDescription>
                            {notCovered('languages')}
                            <FormMessage />
                        </FormItem>
                    )}
                />
                {renderText('signals', true)}
                {renderText('competitors', true)}
                {renderText('notes', true)}
                <FormField
                    control={form.control}
                    name="contactTypes"
                    render={({ field: input }) => (
                        <FormItem>
                            <FormLabel>{field.contactTypes.label}</FormLabel>
                            <FormControl>
                                <ToggleGroup
                                    className="flex-wrap justify-start"
                                    onValueChange={(value: string[]) =>
                                        input.onChange(value.length ? value : ['platform_handle'])
                                    }
                                    type="multiple"
                                    value={input.value}
                                    variant="outline"
                                >
                                    {[...CONTACT_TYPES, ...customContacts].map((type) => (
                                        <ToggleGroupItem
                                            className="group data-[state=on]:bg-primary data-[state=on]:text-primary-foreground data-[state=on]:hover:bg-primary/90 data-[state=on]:hover:text-primary-foreground px-3"
                                            key={type}
                                            value={type}
                                        >
                                            <Check className="hidden size-3.5 group-data-[state=on]:block" />
                                            {copy.profile.contactType[type] ?? type}
                                        </ToggleGroupItem>
                                    ))}
                                </ToggleGroup>
                            </FormControl>
                            {input.value.includes('other') && (
                                <div className="flex gap-2">
                                    <Input
                                        aria-label={field.contactTypes.customLabel}
                                        onChange={(event) => setCustomDraft(event.target.value)}
                                        onKeyDown={(event) => {
                                            if (event.key === 'Enter') {
                                                event.preventDefault();
                                                addCustomContact();
                                            }
                                        }}
                                        placeholder={field.contactTypes.customPlaceholder}
                                        value={customDraft}
                                    />
                                    <Button
                                        aria-label={field.contactTypes.customAdd}
                                        onClick={addCustomContact}
                                        size="icon"
                                        type="button"
                                        variant="outline"
                                    >
                                        <Plus />
                                    </Button>
                                </div>
                            )}
                            {customError && <p className="text-destructive text-sm">{customError}</p>}
                            <FormDescription>{field.contactTypes.help}</FormDescription>
                            {notCovered('contact_types')}
                            <FormMessage />
                        </FormItem>
                    )}
                />
                <FormField
                    control={form.control}
                    name="defaultTarget"
                    render={({ field: input }) => (
                        <FormItem>
                            <FormLabel>{field.defaultTarget.label}</FormLabel>
                            <FormControl>
                                <Input
                                    {...input}
                                    className="w-32"
                                    inputMode="numeric"
                                    placeholder={field.defaultTarget.placeholder}
                                />
                            </FormControl>
                            <FormDescription>{field.defaultTarget.help}</FormDescription>
                            {notCovered('default_target')}
                            <FormMessage />
                        </FormItem>
                    )}
                />

                <h3 className="border-t pt-4 text-base font-semibold">{copy.profile.section.unwanted}</h3>
                {renderText('unwanted', true)}
                {renderNumber('unwantedPenalty', field.unwantedPenalty.label)}
                <div className="bg-muted text-muted-foreground flex flex-col gap-1 rounded-md p-3 text-sm">
                    <span className="font-medium">{copy.profile.compliance.title}</span>
                    {copy.profile.compliance.lines.map((line) => (
                        <span key={line}>· {line}</span>
                    ))}
                </div>

                <h3 className="border-t pt-4 text-base font-semibold">{copy.profile.section.scoring}</h3>
                <p className="text-muted-foreground text-xs">{scoringText.hint}</p>
                {notCovered('scoring')}
                {dimensions.fields.map((dimension, index) => (
                    <div
                        className="flex flex-col gap-2 rounded-md border p-3"
                        key={dimension.id}
                    >
                        <div className="flex items-start gap-2">
                            <FormField
                                control={form.control}
                                name={`dimensions.${index}.name`}
                                render={({ field: input }) => (
                                    <FormItem className="flex-1">
                                        <FormLabel>{scoringText.name}</FormLabel>
                                        <FormControl>
                                            <Input {...input} />
                                        </FormControl>
                                        <FormMessage />
                                    </FormItem>
                                )}
                            />
                            <FormField
                                control={form.control}
                                name={`dimensions.${index}.points`}
                                render={({ field: input }) => (
                                    <FormItem>
                                        <FormLabel>{scoringText.points}</FormLabel>
                                        <FormControl>
                                            <Input
                                                {...input}
                                                className="w-20"
                                                inputMode="numeric"
                                            />
                                        </FormControl>
                                        <FormMessage />
                                    </FormItem>
                                )}
                            />
                            <Button
                                aria-label={scoringText.remove}
                                className="mt-6"
                                onClick={() => dimensions.remove(index)}
                                size="icon"
                                type="button"
                                variant="ghost"
                            >
                                <Trash2 />
                            </Button>
                        </div>
                        <FormField
                            control={form.control}
                            name={`dimensions.${index}.description`}
                            render={({ field: input }) => (
                                <FormItem>
                                    <FormLabel>{scoringText.description}</FormLabel>
                                    <FormControl>
                                        <Textarea
                                            {...input}
                                            placeholder={scoringText.descriptionPlaceholder}
                                            rows={2}
                                        />
                                    </FormControl>
                                    <FormMessage />
                                </FormItem>
                            )}
                        />
                    </div>
                ))}
                {form.formState.errors.dimensions?.root?.message || form.formState.errors.dimensions?.message ? (
                    <p className="text-destructive text-sm">
                        {form.formState.errors.dimensions?.root?.message ?? form.formState.errors.dimensions?.message}
                    </p>
                ) : null}
                <Button
                    className="self-start"
                    onClick={() => dimensions.append({ description: '', key: '', name: '', points: '1' })}
                    size="sm"
                    type="button"
                    variant="outline"
                >
                    <Plus />
                    {scoringText.add}
                </Button>
                <div className="grid gap-3 sm:grid-cols-3">
                    {renderNumber('recent', scoringText.recent)}
                    {renderNumber('qualified', scoringText.qualified)}
                    {renderNumber('watchlist', scoringText.watchlist)}
                </div>

                {error && <div className="text-destructive text-sm">{error}</div>}

                <div className="flex justify-end">
                    <FormSubmitButton size="sm">
                        <span>{copy.profile.save}</span>
                    </FormSubmitButton>
                </div>
            </form>
        </Form>
    );
}
