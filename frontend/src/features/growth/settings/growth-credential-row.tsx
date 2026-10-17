import { ChevronRight, Loader2, Mail, PlugZap, Save } from 'lucide-react';
import { type ReactNode, useState } from 'react';
import { Link } from 'react-router-dom';
import { toast } from 'sonner';

import { Badge, type BadgeVariant } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import { Input } from '@/components/ui/input';
import { InputPassword } from '@/components/ui/input-password';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Spinner } from '@/components/ui/spinner';
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Textarea } from '@/components/ui/textarea';
import { getApiErrorCode, getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';

import type { GrowthSetting, GrowthSettingKind } from '../growth-types';

import { growthApi } from '../growth-api';

const text = copy.credentials;

export const LLM_PROVIDERS = [
    'openai',
    'anthropic',
    'gemini',
    'deepseek',
    'glm',
    'kimi',
    'qwen',
    'minimax',
    'mistral',
    'xai',
    'custom',
] as const;

// The embedders the backend ships; the vector memory and the knowledge base depend on one of them.
export const EMBEDDING_PROVIDERS = [
    'openai',
    'openrouter',
    'ollama',
    'mistral',
    'jina',
    'huggingface',
    'googleai',
    'voyageai',
] as const;

export const SEARCH_PROVIDERS = ['tavily', 'google', 'perplexity', 'firecrawl', 'traversaal'] as const;

export const REQUIRED_KINDS: GrowthSettingKind[] = ['llm', 'screening'];

const TLS_MODES = ['starttls', 'tls', 'none'] as const;

// The Telegram join pace: suggestions that only raise reminders, each empty field taking the server's default.
const PACE_FIELDS = [
    'joins_per_day',
    'join_interval_minutes',
    'join_ramp',
    'private_joins_per_day',
    'joined_groups_warning',
    'read_interval_seconds',
    'first_sync_days',
    'first_sync_messages',
    'risk_reminders',
];

const SMTP_FIELDS = ['host', 'port', 'username', 'password', 'from', 'tls'];

const COUNT = /^\d+$/;
const RANGE = /^\d+(\s*-\s*\d+)?$/;

export const isProxyUrl = (value: string): boolean =>
    /^(https?|socks5h?):\/\/([^\s@/]+@)?[^\s:/@]+:\d{1,5}\/?$/i.test(value);

const EMAIL = /^[^\s@,]+@[^\s@,]+\.[^\s@,]+$/;

const FIELD_CHECKS: Record<string, (value: string) => boolean> = {
    api_hash: (value) => /^[\da-z]{32}$/i.test(value),
    api_id: (value) => /^\d+$/.test(value),
    api_url: (value) => /^https?:\/\/\S+$/i.test(value),
    first_sync_days: (value) => COUNT.test(value),
    first_sync_messages: (value) => COUNT.test(value),
    from: (value) => EMAIL.test(value),
    join_interval_minutes: (value) => RANGE.test(value),
    join_ramp: (value) => /^\d+(\s*,\s*\d+)*$/.test(value),
    joined_groups_warning: (value) => COUNT.test(value),
    joins_per_day: (value) => COUNT.test(value),
    notify_emails: (value) => value.split(',').every((address) => EMAIL.test(address.trim())),
    port: (value) => /^\d+$/.test(value) && Number(value) >= 1 && Number(value) <= 65535,
    private_joins_per_day: (value) => COUNT.test(value),
    read_interval_seconds: (value) => RANGE.test(value),
    url: (value) => isProxyUrl(value),
};

// Secrets go through the masked input and come back masked; everything else is shown as saved.
const SECRET_FIELDS = new Set(['api_hash', 'api_key', 'api_keys', 'password']);

// The server masks these too, and keeps the stored value when they are left empty, so editing starts them empty.
const MASKED_FIELDS = new Set([...SECRET_FIELDS, 'url']);

// One value per line; YouTube rotates to the next key when one runs out of quota.
const MULTILINE_FIELDS = new Set(['api_keys']);

// Empty means "send to the sign-in email", the server's default, or Jev's latest model, so these are never required.
const OPTIONAL_FIELDS = new Set(['notify_emails', ...PACE_FIELDS]);

// A catalogue provider already knows its model and endpoint; these only override them.
const LLM_OVERRIDE_FIELDS = new Set(['model', 'server_url']);

const PROTOCOLS = ['openai_chat', 'anthropic_messages'] as const;

const SELECT_OPTIONS: Record<string, readonly string[]> = {
    protocol: PROTOCOLS,
    risk_reminders: ['on', 'off'],
    tls: TLS_MODES,
};

const selectLabel = (field: string, value: string): string | undefined => text.selectOption[field]?.[value];

// Fields picked from a list rather than typed, with the option a new setting starts on.
const SELECT_DEFAULTS: Record<string, string> = { protocol: 'openai_chat', risk_reminders: 'on', tls: 'starttls' };

// Relay providers serve many models under their own names, so the card can ask them for the list.
// The backend keeps a saved field when it is sent empty, so going back to the newest Jev
// means saving its alias explicitly.
const latestScreeningModel = (provider: null | string) =>
    provider === 'openrouter' ? '~typesafe/jev-latest' : 'jev-latest';

const canListModels = (kind: GrowthSettingKind, provider: null | string) =>
    (kind === 'llm' && provider === 'custom') ||
    (kind === 'screening' && !!provider && provider !== 'typesafe') ||
    (kind === 'embedding' && (provider === 'openai' || provider === 'openrouter'));

// With the platform's SMTP server set in .env, a customer only names where the email goes.
const usesPlatformSmtp = (kind: GrowthSettingKind, setting: GrowthSetting | undefined) =>
    kind === 'email' && setting?.platform_smtp === true;

// Fields folded under the card's "advanced" toggle.
const isOverrideField = (
    kind: GrowthSettingKind,
    provider: null | string,
    field: string,
    setting: GrowthSetting | undefined,
) =>
    (kind === 'llm' && provider !== 'custom' && LLM_OVERRIDE_FIELDS.has(field)) ||
    (kind === 'embedding' && LLM_OVERRIDE_FIELDS.has(field) && !(provider === 'openrouter' && field === 'model')) ||
    (kind === 'telegram' && PACE_FIELDS.includes(field)) ||
    (usesPlatformSmtp(kind, setting) && SMTP_FIELDS.includes(field));

// Empty embedding fields mean the built-in local embedder.
const isOptionalField = (
    kind: GrowthSettingKind,
    provider: null | string,
    field: string,
    setting: GrowthSetting | undefined,
) =>
    OPTIONAL_FIELDS.has(field) ||
    kind === 'embedding' ||
    (kind === 'screening' && field === 'model') ||
    isOverrideField(kind, provider, field, setting);

const fieldsFor = (kind: GrowthSettingKind, provider: null | string, setting: GrowthSetting | undefined): string[] => {
    switch (kind) {
        case 'email':
            return usesPlatformSmtp(kind, setting)
                ? ['notify_emails', ...SMTP_FIELDS]
                : [...SMTP_FIELDS, 'notify_emails'];
        case 'embedding':
            return ['api_key', 'model', 'server_url'];
        case 'llm':
            return provider === 'custom'
                ? ['server_url', 'protocol', 'model', 'api_key']
                : ['api_key', 'model', 'server_url'];
        case 'proxy':
            return ['url'];
        // A server without Jev routing reports no model field, and refuses one it does not know.
        case 'screening':
            if (!setting || !('model' in setting.fields)) {
                return ['api_key'];
            }

            return provider === 'custom' ? ['api_url', 'model', 'api_key'] : ['api_key', 'model'];
        case 'telegram':
            return ['api_id', 'api_hash', ...PACE_FIELDS];
        case 'web_search':
            return provider === 'google' ? ['api_keys', 'cx'] : ['api_keys'];
        case 'youtube':
            return ['api_keys'];
        default:
            return ['api_key'];
    }
};

const fallbackProviders = (kind: GrowthSettingKind): readonly string[] =>
    kind === 'llm'
        ? LLM_PROVIDERS
        : kind === 'embedding'
          ? EMBEDDING_PROVIDERS
          : kind === 'web_search'
            ? SEARCH_PROVIDERS
            : [];

export type GrowthSettingState = 'connected' | 'invalid' | 'unset' | 'untested';

export const settingState = (setting: GrowthSetting | undefined): GrowthSettingState =>
    !setting || setting.source === 'none' ? 'unset' : setting.status;

const stateVariant: Record<GrowthSettingState, BadgeVariant> = {
    connected: 'green',
    invalid: 'red',
    unset: 'secondary',
    untested: 'yellow',
};

const reasonText = (setting: GrowthSetting) =>
    (setting.status_reason && (text.reason[setting.status_reason] ?? setting.status_reason)) || text.unknownReason;

const fieldLabel = (kind: GrowthSettingKind, provider: null | string, field: string): string =>
    text.fieldByKind[`${kind}.${provider}.${field}`]?.label ??
    text.fieldByKind[`${kind}.${field}`]?.label ??
    text.field[field]?.label ??
    field;

const fieldPlaceholder = (kind: GrowthSettingKind, provider: null | string, field: string): string | undefined =>
    text.fieldByKind[`${kind}.${provider}.${field}`]?.placeholder ??
    text.fieldByKind[`${kind}.${field}`]?.placeholder ??
    text.field[field]?.placeholder;

const statusLine = (kind: GrowthSettingKind, setting: GrowthSetting | undefined): string => {
    switch (settingState(setting)) {
        case 'connected':
            return text.statusLine.connected(
                setting?.tested_at ? copy.common.relative(new Date(setting.tested_at)) : '',
            );
        case 'invalid':
            return text.statusLine.invalid(setting ? reasonText(setting) : '');
        case 'untested':
            return text.statusLine.untested;
        default:
            if (kind === 'web_search') {
                return text.statusLine.unsetSearch;
            }

            if (kind === 'screening') {
                return text.statusLine.unsetScreening;
            }

            return REQUIRED_KINDS.includes(kind) ? text.statusLine.unsetRequired : text.statusLine.unsetOptional;
    }
};

interface GrowthCredentialCardProps {
    // Rendered at the bottom of the card, below its own actions.
    children?: ReactNode;
    // Shown under the Telegram card once its credentials work but no account is signed in yet.
    isTelegramSignedIn?: boolean;
    isTesting?: boolean;
    kind: GrowthSettingKind;
    onChange: (next: GrowthSetting | null) => void;
    setting: GrowthSetting | undefined;
}

// Nothing here asks for confirmation: Save stores and tests, Clear clears, Replace keeps the old key live
// until the new one is saved.
export function GrowthCredentialCard({
    children,
    isTelegramSignedIn = true,
    isTesting: isAutoTesting = false,
    kind,
    onChange,
    setting,
}: GrowthCredentialCardProps) {
    const providers = setting?.providers?.length ? setting.providers : fallbackProviders(kind);
    const card = text.card[kind] ?? { purpose: '', title: kind };
    const isSaved = settingState(setting) !== 'unset';

    const [isEditing, setIsEditing] = useState(!isSaved);
    const [provider, setProvider] = useState<null | string>(setting?.provider ?? providers[0] ?? null);
    const [values, setValues] = useState<Record<string, string>>({});
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [isSaving, setIsSaving] = useState(false);
    const [isTesting, setIsTesting] = useState(false);
    const [isListingModels, setIsListingModels] = useState(false);
    const [models, setModels] = useState<null | string[]>(null);

    const fields = fieldsFor(kind, provider, setting);
    const isCustomLlm = kind === 'llm' && provider === 'custom';
    const catalogProviders = kind === 'llm' ? providers.filter((option) => option !== 'custom') : providers;
    const mainFields = fields.filter((field) => !isOverrideField(kind, provider, field, setting));
    const overrideFields = fields.filter((field) => isOverrideField(kind, provider, field, setting));
    const advanced = text.advanced[kind] ?? { hint: text.llmMode.overridesHint, title: text.llmMode.overrides };
    const isBusy = isSaving || isTesting || isAutoTesting || isListingModels;
    const state = settingState(setting);
    const guide = providers.length
        ? (text.guide[`${kind}.${provider}`] ?? text.guide[provider ?? ''])
        : text.guide[kind];
    const isRequiredUnset = REQUIRED_KINDS.includes(kind) && state === 'unset';
    // Saving with the same provider may leave any field empty to keep what is stored.
    const canKeepSaved = isSaved && (setting?.provider ?? null) === (providers.length ? provider : null);

    const startEditing = () => {
        setValues(
            Object.fromEntries(Object.entries(setting?.fields ?? {}).filter(([field]) => !MASKED_FIELDS.has(field))),
        );
        setIsEditing(true);
    };

    const validate = (): null | Record<string, string> => {
        const nextErrors: Record<string, string> = {};
        const filled: Record<string, string> = {};

        for (const field of fields) {
            const raw = values[field] ?? SELECT_DEFAULTS[field] ?? '';
            const value = MULTILINE_FIELDS.has(field)
                ? raw
                      .split('\n')
                      .map((line) => line.trim())
                      .filter(Boolean)
                      .join('\n')
                : raw.trim();
            const label = fieldLabel(kind, provider, field);

            if (!value) {
                if (!canKeepSaved && !isOptionalField(kind, provider, field, setting)) {
                    nextErrors[field] = text.fieldRequired(label);
                }

                continue;
            }

            if (FIELD_CHECKS[field] && !FIELD_CHECKS[field](value)) {
                nextErrors[field] = text.fieldInvalid[field] ?? text.reason.format ?? '';
            }

            filled[field] = value;
        }

        // An empty host is the one empty value the server does not keep: it drops the customer's own server and goes
        // back to the platform's, and it refuses the other SMTP fields without a host.
        if (usesPlatformSmtp(kind, setting) && !filled.host) {
            for (const field of SMTP_FIELDS) {
                delete nextErrors[field];
                delete filled[field];
            }

            filled.host = '';
        }

        setErrors(nextErrors);

        return Object.keys(nextErrors).length ? null : filled;
    };

    const handleSave = async () => {
        const filled = validate();

        if (!filled) {
            return;
        }

        setIsSaving(true);

        try {
            await growthApi.saveSetting(kind, { fields: filled, provider: providers.length ? provider : null });
            const tested = await growthApi.testSetting(kind);

            if (tested.status === 'connected') {
                toast.success(text.saved(card.title));
            } else {
                toast.error(text.savedButFailed(card.title, reasonText(tested)));
            }

            setValues({});
            setIsEditing(false);
            onChange(tested);
        } catch (error) {
            toast.error(
                getApiErrorCode(error) === 'Growth.NoSecretKey'
                    ? text.noSecretKey
                    : getApiErrorMessage(error, copy.common.saveFailed),
            );
        } finally {
            setIsSaving(false);
        }
    };

    const handleTest = async () => {
        setIsTesting(true);

        try {
            if (kind === 'email') {
                const result = await growthApi.testEmail(text.mailLanguage);

                if (result.sent) {
                    toast.success(text.testEmailSent(result.to.join(', ')));
                } else {
                    toast.error(text.testEmailFailed);
                }
            } else {
                onChange(await growthApi.testSetting(kind));
            }
        } catch (error) {
            toast.error(getApiErrorMessage(error, kind === 'email' ? text.testEmailFailed : copy.common.networkError));
        } finally {
            setIsTesting(false);
        }
    };

    // An unchanged key is sent empty, and the server lists the models with the one it has saved.
    const handleListModels = async () => {
        setIsListingModels(true);

        try {
            const listed = await growthApi.listModels(
                kind,
                kind === 'llm'
                    ? {
                          api_key: values.api_key?.trim() ?? '',
                          protocol: values.protocol ?? '',
                          provider,
                          server_url: values.server_url?.trim() ?? '',
                      }
                    : kind === 'embedding'
                      ? { api_key: values.api_key?.trim() ?? '', provider, server_url: values.server_url?.trim() ?? '' }
                      : { api_key: values.api_key?.trim() ?? '', api_url: values.api_url?.trim() ?? '', provider },
            );

            if (listed.length) {
                setModels(listed.map((model) => model.id));
            } else {
                setModels(null);
                toast.error(text.modelsEmpty);
            }
        } catch {
            setModels(null);
            toast.error(text.modelsFailed);
        } finally {
            setIsListingModels(false);
        }
    };

    const handleClear = async () => {
        try {
            await growthApi.clearSetting(kind);
            toast.success(text.cleared(card.title));
            setValues({});
            setIsEditing(true);
            onChange(null);
        } catch (error) {
            toast.error(getApiErrorMessage(error, copy.common.saveFailed));
        }
    };

    const renderInput = (field: string) => {
        const id = `credential-${kind}-${field}`;
        const placeholder =
            canKeepSaved && MASKED_FIELDS.has(field) ? text.keepSaved : fieldPlaceholder(kind, provider, field);
        const onValue = (value: string) => setValues((previous) => ({ ...previous, [field]: value }));

        if (!isEditing) {
            return (
                <div
                    className="bg-muted min-h-9 rounded-md px-3 py-2 font-mono text-sm break-all"
                    id={id}
                >
                    {field in SELECT_DEFAULTS
                        ? (selectLabel(field, setting?.fields[field] ?? SELECT_DEFAULTS[field] ?? '') ??
                          copy.common.dash)
                        : MULTILINE_FIELDS.has(field) && setting?.fields[field]
                          ? setting.fields[field]
                                .split(/[\n,]/)
                                .filter(Boolean)
                                .map((line, index) => <div key={index}>{line}</div>)
                          : setting?.fields[field] || copy.common.dash}
                </div>
            );
        }

        if (field in SELECT_DEFAULTS) {
            return (
                <Select
                    disabled={isBusy}
                    onValueChange={onValue}
                    value={values[field] ?? setting?.fields[field] ?? SELECT_DEFAULTS[field]}
                >
                    <SelectTrigger
                        className="w-full"
                        id={id}
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        {(SELECT_OPTIONS[field] ?? []).map((option) => (
                            <SelectItem
                                key={option}
                                value={option}
                            >
                                {selectLabel(field, option)}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
            );
        }

        if (field === 'model' && models) {
            return (
                <Select
                    disabled={isBusy}
                    onValueChange={onValue}
                    value={values.model || undefined}
                >
                    <SelectTrigger
                        className="w-full"
                        id={id}
                    >
                        <SelectValue placeholder={text.modelsPick} />
                    </SelectTrigger>
                    <SelectContent>
                        {models.map((option) => (
                            <SelectItem
                                key={option}
                                value={option}
                            >
                                {option}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
            );
        }

        if (MULTILINE_FIELDS.has(field)) {
            return (
                <Textarea
                    aria-invalid={!!errors[field]}
                    autoComplete="off"
                    className="font-mono"
                    disabled={isBusy}
                    id={id}
                    onChange={(event) => onValue(event.target.value)}
                    placeholder={placeholder}
                    rows={3}
                    value={values[field] ?? ''}
                />
            );
        }

        const Component = SECRET_FIELDS.has(field) ? InputPassword : Input;

        return (
            <Component
                aria-invalid={!!errors[field]}
                autoComplete="off"
                disabled={isBusy}
                id={id}
                onChange={(event) => onValue(event.target.value)}
                placeholder={placeholder}
                value={values[field] ?? ''}
            />
        );
    };

    const renderField = (field: string) => (
        <div
            className={MULTILINE_FIELDS.has(field) ? 'flex flex-col gap-1.5 sm:col-span-2' : 'flex flex-col gap-1.5'}
            key={field}
        >
            <div className="flex min-h-5 items-center justify-between gap-2">
                <Label htmlFor={`credential-${kind}-${field}`}>{fieldLabel(kind, provider, field)}</Label>
                <div className="flex items-center gap-3">
                    {field === 'model' &&
                        isEditing &&
                        kind === 'screening' &&
                        !!setting?.fields.model &&
                        setting.fields.model !== latestScreeningModel(provider) && (
                            <Button
                                className="h-auto p-0 text-xs"
                                disabled={isBusy}
                                onClick={() => {
                                    setModels(null);
                                    setValues((previous) => ({ ...previous, model: latestScreeningModel(provider) }));
                                }}
                                type="button"
                                variant="link"
                            >
                                {text.modelsLatest}
                            </Button>
                        )}
                    {field === 'model' && isEditing && canListModels(kind, provider) && (
                        <Button
                            className="h-auto p-0 text-xs"
                            disabled={isBusy}
                            onClick={models ? () => setModels(null) : handleListModels}
                            type="button"
                            variant="link"
                        >
                            {isListingModels && <Spinner variant="circle" />}
                            {models ? text.modelsManual : text.modelsFetch}
                        </Button>
                    )}
                </div>
            </div>
            {renderInput(field)}
            {text.fieldHelp[`${kind}.${field}`] && (
                <p className="text-muted-foreground text-xs">{text.fieldHelp[`${kind}.${field}`]}</p>
            )}
            {errors[field] && <p className="text-destructive text-xs">{errors[field]}</p>}
        </div>
    );

    return (
        <Card>
            <CardHeader className="gap-2">
                <div className="flex items-start justify-between gap-3">
                    <div className="flex flex-wrap items-center gap-2">
                        <CardTitle>{card.title}</CardTitle>
                        <Badge variant="outline">{REQUIRED_KINDS.includes(kind) ? text.required : text.optional}</Badge>
                        {isBusy && !isSaving ? (
                            <Badge variant="secondary">
                                <Loader2 className="size-3 animate-spin" />
                                {text.testing}
                            </Badge>
                        ) : (
                            <Badge variant={stateVariant[state]}>{text.status[state]}</Badge>
                        )}
                        {setting?.source === 'env' && <Badge variant="blue">{text.envSource}</Badge>}
                    </div>
                    {setting?.source === 'database' && (
                        <Button
                            className="h-auto p-0"
                            disabled={isBusy}
                            onClick={handleClear}
                            variant="link"
                        >
                            {text.clear}
                        </Button>
                    )}
                </div>
                <p
                    className={
                        state === 'invalid' || (kind === 'screening' && isRequiredUnset)
                            ? 'text-destructive text-xs'
                            : 'text-muted-foreground text-xs'
                    }
                >
                    {statusLine(kind, setting)}
                </p>
                {setting?.source === 'env' && <p className="text-muted-foreground text-xs">{text.envSourceNote}</p>}
                <CardDescription>
                    {card.purpose}{' '}
                    <Link
                        className="font-medium whitespace-nowrap underline"
                        rel="noreferrer"
                        target="_blank"
                        to={`${routes.help.keys}#${kind}`}
                    >
                        {text.howToApply}
                    </Link>
                </CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
                {kind === 'llm' && (
                    <div className="flex flex-col gap-2">
                        <Tabs
                            onValueChange={(mode) => {
                                setProvider(
                                    mode === 'custom'
                                        ? 'custom'
                                        : setting?.provider && setting.provider !== 'custom'
                                          ? setting.provider
                                          : (catalogProviders[0] ?? null),
                                );
                                setErrors({});
                            }}
                            value={isCustomLlm ? 'custom' : 'catalog'}
                        >
                            <TabsList>
                                <TabsTrigger
                                    disabled={!isEditing || isBusy}
                                    value="catalog"
                                >
                                    {text.llmMode.catalog}
                                </TabsTrigger>
                                <TabsTrigger
                                    disabled={!isEditing || isBusy}
                                    value="custom"
                                >
                                    {text.llmMode.custom}
                                </TabsTrigger>
                            </TabsList>
                        </Tabs>
                        <p className="text-muted-foreground text-sm">
                            {isCustomLlm ? text.llmMode.customHint : text.llmMode.catalogHint}
                        </p>
                    </div>
                )}

                {catalogProviders.length > 0 && !isCustomLlm && (
                    <div className="flex flex-col gap-1.5">
                        <Label>{text.provider[kind]}</Label>
                        <Select
                            disabled={!isEditing || isBusy}
                            onValueChange={(value) => {
                                setProvider(value);
                                setErrors({});
                                setModels(null);
                            }}
                            value={provider ?? undefined}
                        >
                            <SelectTrigger className="w-full sm:w-72">
                                <SelectValue />
                            </SelectTrigger>
                            <SelectContent>
                                {catalogProviders.map((option) => (
                                    <SelectItem
                                        key={option}
                                        value={option}
                                    >
                                        {text.providerLabel[`${kind}.${option}`] ??
                                            text.providerLabel[option] ??
                                            option}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                )}

                {guide && !isCustomLlm && <p className="text-muted-foreground text-sm">{guide}</p>}
                {kind === 'llm' && <p className="text-muted-foreground text-xs">{text.llmTopUpNote}</p>}
                {kind === 'telegram' && <p className="text-muted-foreground text-xs">{text.telegramNote}</p>}
                {usesPlatformSmtp(kind, setting) && (
                    <p className="text-muted-foreground text-xs">{text.platformSmtpOn}</p>
                )}
                {kind === 'email' && setting?.platform_smtp === false && (
                    <p className="text-xs text-yellow-800 dark:text-yellow-400">{text.platformSmtpOff}</p>
                )}

                <div className="grid gap-3 sm:grid-cols-2">{mainFields.map(renderField)}</div>

                {overrideFields.length > 0 && (
                    <Collapsible
                        className="border-t pt-3"
                        // The pace always comes back filled with its defaults, so it stays folded.
                        defaultOpen={kind !== 'telegram' && overrideFields.some((field) => !!setting?.fields[field])}
                    >
                        <CollapsibleTrigger className="group flex items-center gap-1 text-sm font-medium">
                            <ChevronRight className="size-4 transition-transform group-data-[state=open]:rotate-90" />
                            {advanced.title}
                        </CollapsibleTrigger>
                        <CollapsibleContent className="mt-3 flex flex-col gap-3">
                            <p className="text-muted-foreground text-xs">{advanced.hint}</p>
                            <div className="grid gap-3 sm:grid-cols-2">{overrideFields.map(renderField)}</div>
                        </CollapsibleContent>
                    </Collapsible>
                )}

                {kind === 'llm' && (
                    <Link
                        className="self-start text-sm font-medium underline"
                        to={routes.settings.providers}
                    >
                        {text.llmMode.rolesLink}
                    </Link>
                )}

                <div className="flex flex-wrap items-center gap-2">
                    {isEditing ? (
                        <>
                            <Button
                                disabled={isBusy}
                                onClick={handleSave}
                                size="sm"
                            >
                                {isSaving ? <Spinner variant="circle" /> : <Save />}
                                {text.saveAndTest}
                            </Button>
                            {isSaved && (
                                <Button
                                    disabled={isBusy}
                                    onClick={() => {
                                        setIsEditing(false);
                                        setValues({});
                                        setErrors({});
                                        setProvider(setting?.provider ?? providers[0] ?? null);
                                        setModels(null);
                                    }}
                                    size="sm"
                                    variant="ghost"
                                >
                                    {text.cancel}
                                </Button>
                            )}
                        </>
                    ) : (
                        <>
                            <Button
                                disabled={isBusy}
                                onClick={handleTest}
                                size="sm"
                                variant="outline"
                            >
                                {isTesting ? <Spinner variant="circle" /> : kind === 'email' ? <Mail /> : <PlugZap />}
                                {kind === 'email' ? text.testEmail : text.test}
                            </Button>
                            <Button
                                disabled={isBusy}
                                onClick={startEditing}
                                size="sm"
                                variant="ghost"
                            >
                                {text.replace}
                            </Button>
                        </>
                    )}
                </div>

                {kind === 'telegram' && state === 'connected' && !isTelegramSignedIn && (
                    <Link
                        className="text-sm font-medium underline"
                        to={routes.settings.accounts}
                    >
                        {text.telegramNextStep}
                    </Link>
                )}
                {children}
            </CardContent>
        </Card>
    );
}
