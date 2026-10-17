import { Check, ChevronRight } from 'lucide-react';
import { useEffect, useState } from 'react';
import { toast } from 'sonner';
import * as z from 'zod';

import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
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
    GrowthScoringSignal,
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

export const CONTACT_TYPES: GrowthContactType[] = ['platform_handle', 'phone', 'email', 'messenger', 'other'];

const SCORING_SIGNALS: GrowthScoringSignal[] = ['player', 'pain', 'paying', 'active', 'match'];

const DEFAULT_SCORING = {
    qualified: 7,
    watchlist: 4,
    weights: { active: 2, match: 3, pain: 3, paying: 2, player: 3 },
};

const scoringText = copy.profile.scoring;

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
        excludes: z.string(),
        games: nonEmptyList(required.games),
        genre: z.string().trim().min(1, { message: required.genre }),
        languages: z.array(z.string()),
        name: z.string().trim().min(1, { message: required.name }),
        notes: z.string(),
        otherLanguages: z.string(),
        pitch: z.string().trim().min(1, { message: required.pitch }),
        qualified: wholeNumber,
        regions: nonEmptyList(required.regions),
        signals: z.string(),
        watchlist: wholeNumber,
        weights: z.object({
            active: wholeNumber,
            match: wholeNumber,
            pain: wholeNumber,
            paying: wholeNumber,
            player: wholeNumber,
        }),
    })
    .refine((values) => selectedLanguages(values).length > 0, { message: required.languages, path: ['languages'] })
    .superRefine((values, context) => {
        const weights = SCORING_SIGNALS.map((signal) => Number(values.weights[signal]));
        const qualified = Number(values.qualified);
        const watchlist = Number(values.watchlist);

        SCORING_SIGNALS.forEach((signal, index) => {
            if ((weights[index] ?? 0) > 10) {
                context.addIssue({ code: 'custom', message: scoringText.invalidWeight, path: ['weights', signal] });
            }
        });

        if (watchlist < 1 || qualified <= watchlist) {
            context.addIssue({ code: 'custom', message: scoringText.invalidLines, path: ['watchlist'] });
        } else if (weights.reduce((sum, weight) => sum + weight, 0) < qualified) {
            context.addIssue({ code: 'custom', message: scoringText.unreachable, path: ['qualified'] });
        }
    });

type ProfileFormValues = z.infer<typeof profileFormSchema>;

const toFormValues = (profile: GrowthProfile | null): ProfileFormValues => ({
    competitors: joinList(profile?.competitors),
    contactTypes: profile?.contact_types?.length ? profile.contact_types : ['platform_handle'],
    defaultTarget: String(profile?.default_target ?? 50),
    excludes: joinList(profile ? profile.excludes : copy.profile.defaultExcludes),
    games: joinList(profile?.games),
    genre: profile?.genre ?? '',
    ...languageFormValues(profile?.languages),
    name: profile?.name ?? '',
    notes: profile?.notes ?? '',
    pitch: profile?.pitch ?? '',
    qualified: String((profile?.scoring ?? DEFAULT_SCORING).qualified),
    regions: joinList(profile?.regions),
    signals: joinList(profile?.signals),
    watchlist: String((profile?.scoring ?? DEFAULT_SCORING).watchlist),
    weights: Object.fromEntries(
        SCORING_SIGNALS.map((signal) => [signal, String((profile?.scoring ?? DEFAULT_SCORING).weights[signal])]),
    ) as Record<GrowthScoringSignal, string>,
});

const toInput = (values: ProfileFormValues): GrowthProfileInput => ({
    competitors: splitList(values.competitors),
    contact_types: values.contactTypes as GrowthContactType[],
    default_target: Number(values.defaultTarget),
    excludes: splitList(values.excludes),
    games: splitList(values.games),
    genre: values.genre.trim(),
    languages: selectedLanguages(values),
    name: values.name.trim(),
    notes: values.notes.trim(),
    pitch: values.pitch.trim(),
    regions: splitList(values.regions),
    scoring: {
        qualified: Number(values.qualified),
        watchlist: Number(values.watchlist),
        weights: Object.fromEntries(
            SCORING_SIGNALS.map((signal) => [signal, Number(values.weights[signal])]),
        ) as Record<GrowthScoringSignal, number>,
    },
    signals: splitList(values.signals),
});

type ProfileField = keyof GrowthProfileInput;

// The form fields each profile field fills. Typed as a Record so a field the server adds cannot be
// left out of a preset by accident.
export const PRESET_FORM_FIELDS: Record<ProfileField, (keyof ProfileFormValues)[]> = {
    competitors: ['competitors'],
    contact_types: ['contactTypes'],
    default_target: ['defaultTarget'],
    excludes: ['excludes'],
    games: ['games'],
    genre: ['genre'],
    languages: ['languages', 'otherLanguages'],
    name: ['name'],
    notes: ['notes'],
    pitch: ['pitch'],
    regions: ['regions'],
    scoring: ['weights', 'qualified', 'watchlist'],
    signals: ['signals'],
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
    const preset = toFormValues({
        ...(Object.fromEntries(fields.map((name) => [name, profile[name] ?? undefined])) as GrowthProfileInput),
        id: 0,
        updated_at: '',
    });
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
    | 'excludes'
    | 'games'
    | 'genre'
    | 'name'
    | 'notes'
    | 'pitch'
    | 'regions'
    | 'signals';

export function GrowthProfileForm({ onSaved, profile }: GrowthProfileFormProps) {
    const [error, setError] = useState<null | string>(null);

    const form = useAppForm<ProfileFormValues>({
        defaultValues: toFormValues(profile),
        schema: profileFormSchema,
    });

    const [presets, setPresets] = useState<GrowthProfilePreset[]>([]);
    const [uncovered, setUncovered] = useState<Set<ProfileField>>(new Set());
    const [presetId, setPresetId] = useState('');

    const fill = (preset: GrowthProfilePreset) => {
        const next = withPreset(form.getValues(), preset);

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
                    fill(initial);
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
                    {notCovered(name)}
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
                {renderText('excludes', true)}
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
                                    {CONTACT_TYPES.map((type) => (
                                        <ToggleGroupItem
                                            className="group data-[state=on]:bg-primary data-[state=on]:text-primary-foreground data-[state=on]:hover:bg-primary/90 data-[state=on]:hover:text-primary-foreground px-3"
                                            key={type}
                                            value={type}
                                        >
                                            <Check className="hidden size-3.5 group-data-[state=on]:block" />
                                            {copy.profile.contactType[type]}
                                        </ToggleGroupItem>
                                    ))}
                                </ToggleGroup>
                            </FormControl>
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

                <Collapsible
                    className="border-t pt-3"
                    defaultOpen={!!profile?.scoring}
                >
                    <CollapsibleTrigger className="group flex items-center gap-1 text-sm font-medium">
                        <ChevronRight className="size-4 transition-transform group-data-[state=open]:rotate-90" />
                        {scoringText.title}
                    </CollapsibleTrigger>
                    <CollapsibleContent className="mt-3 flex flex-col gap-3">
                        <p className="text-muted-foreground text-xs">{scoringText.hint}</p>
                        {notCovered('scoring')}
                        <div className="grid gap-3 sm:grid-cols-2">
                            {SCORING_SIGNALS.map((signal) => (
                                <FormField
                                    control={form.control}
                                    key={signal}
                                    name={`weights.${signal}`}
                                    render={({ field: input }) => (
                                        <FormItem>
                                            <FormLabel>{scoringText.weight[signal]}</FormLabel>
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
                            ))}
                            {(['qualified', 'watchlist'] as const).map((line) => (
                                <FormField
                                    control={form.control}
                                    key={line}
                                    name={line}
                                    render={({ field: input }) => (
                                        <FormItem>
                                            <FormLabel>{scoringText[line]}</FormLabel>
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
                            ))}
                        </div>
                    </CollapsibleContent>
                </Collapsible>

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
