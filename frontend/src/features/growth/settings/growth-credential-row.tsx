import { ChevronRight, Loader2, Mail, PlugZap, Save } from 'lucide-react';
import { useState } from 'react';
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

export const SEARCH_PROVIDERS = ['tavily', 'google', 'perplexity', 'firecrawl', 'traversaal'] as const;

export const REQUIRED_KINDS: GrowthSettingKind[] = ['llm', 'telegram'];

const TLS_MODES = ['starttls', 'tls', 'none'] as const;

export const isProxyUrl = (value: string): boolean =>
    /^(https?|socks5h?):\/\/([^\s@/]+@)?[^\s:/@]+:\d{1,5}\/?$/i.test(value);

const EMAIL = /^[^\s@,]+@[^\s@,]+\.[^\s@,]+$/;

const FIELD_CHECKS: Record<string, (value: string) => boolean> = {
    api_hash: (value) => /^[\da-z]{32}$/i.test(value),
    api_id: (value) => /^\d+$/.test(value),
    from: (value) => EMAIL.test(value),
    notify_emails: (value) => value.split(',').every((address) => EMAIL.test(address.trim())),
    port: (value) => /^\d+$/.test(value) && Number(value) >= 1 && Number(value) <= 65535,
    url: (value) => isProxyUrl(value),
};

// Secrets go through the masked input and come back masked; everything else is shown as saved.
const SECRET_FIELDS = new Set(['api_hash', 'api_key', 'api_keys', 'password']);

// One value per line; YouTube rotates to the next key when one runs out of quota.
const MULTILINE_FIELDS = new Set(['api_keys']);

// Empty means "send to the sign-in email", so it is never required.
const OPTIONAL_FIELDS = new Set(['notify_emails']);

// A catalogue provider already knows its model and endpoint; these only override them.
const LLM_OVERRIDE_FIELDS = new Set(['model', 'server_url']);

// The custom endpoint speaks OpenAI Chat Completions only; the selector is shown but not sent.
const DISPLAY_ONLY_FIELDS = new Set(['protocol']);
const PROTOCOLS = ['openai', 'anthropic'] as const;

const isOverrideField = (kind: GrowthSettingKind, provider: null | string, field: string) =>
    kind === 'llm' && provider !== 'custom' && LLM_OVERRIDE_FIELDS.has(field);

const fieldsFor = (kind: GrowthSettingKind, provider: null | string): string[] => {
    switch (kind) {
        case 'email':
            return ['host', 'port', 'username', 'password', 'from', 'tls', 'notify_emails'];
        case 'llm':
            return provider === 'custom'
                ? ['server_url', 'protocol', 'model', 'api_key']
                : ['api_key', 'model', 'server_url'];
        case 'proxy':
            return ['url'];
        case 'telegram':
            return ['api_id', 'api_hash'];
        case 'web_search':
            return provider === 'google' ? ['api_keys', 'cx'] : ['api_keys'];
        case 'youtube':
            return ['api_keys'];
        default:
            return ['api_key'];
    }
};

const fallbackProviders = (kind: GrowthSettingKind): readonly string[] =>
    kind === 'llm' ? LLM_PROVIDERS : kind === 'web_search' ? SEARCH_PROVIDERS : [];

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

            return REQUIRED_KINDS.includes(kind) ? text.statusLine.unsetRequired : text.statusLine.unsetOptional;
    }
};

interface GrowthCredentialCardProps {
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
    isTelegramSignedIn = true,
    isTesting: isAutoTesting = false,
    kind,
    onChange,
    setting,
}: GrowthCredentialCardProps) {
    const providers = setting?.providers.length ? setting.providers : fallbackProviders(kind);
    const card = text.card[kind] ?? { purpose: '', title: kind };
    const isSaved = settingState(setting) !== 'unset';

    const [isEditing, setIsEditing] = useState(!isSaved);
    const [provider, setProvider] = useState<null | string>(setting?.provider ?? providers[0] ?? null);
    const [values, setValues] = useState<Record<string, string>>({});
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [isSaving, setIsSaving] = useState(false);
    const [isTesting, setIsTesting] = useState(false);

    const fields = fieldsFor(kind, provider);
    const isCustomLlm = kind === 'llm' && provider === 'custom';
    const catalogProviders = kind === 'llm' ? providers.filter((option) => option !== 'custom') : providers;
    const mainFields = fields.filter((field) => !isOverrideField(kind, provider, field));
    const overrideFields = fields.filter((field) => isOverrideField(kind, provider, field));
    const isBusy = isSaving || isTesting || isAutoTesting;
    const state = settingState(setting);
    const guide = text.guide[providers.length ? (provider ?? '') : kind];
    // Saving with the same provider may leave any field empty to keep what is stored.
    const canKeepSaved = isSaved && setting?.provider === (providers.length ? provider : null);

    const startEditing = () => {
        setValues(
            Object.fromEntries(Object.entries(setting?.fields ?? {}).filter(([field]) => !SECRET_FIELDS.has(field))),
        );
        setIsEditing(true);
    };

    const validate = (): null | Record<string, string> => {
        const nextErrors: Record<string, string> = {};
        const filled: Record<string, string> = {};

        for (const field of fields) {
            if (DISPLAY_ONLY_FIELDS.has(field)) {
                continue;
            }

            const raw = values[field] ?? (field === 'tls' ? TLS_MODES[0] : '');
            const value = MULTILINE_FIELDS.has(field)
                ? raw
                      .split('\n')
                      .map((line) => line.trim())
                      .filter(Boolean)
                      .join('\n')
                : raw.trim();
            const label = text.field[field]?.label ?? field;

            if (!value) {
                if (!canKeepSaved && !OPTIONAL_FIELDS.has(field) && !isOverrideField(kind, provider, field)) {
                    nextErrors[field] = text.fieldRequired(label);
                }

                continue;
            }

            if (FIELD_CHECKS[field] && !FIELD_CHECKS[field](value)) {
                nextErrors[field] = text.fieldInvalid[field] ?? text.reason.format ?? '';
            }

            filled[field] = value;
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
                const result = await growthApi.testEmail();

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
        const placeholder = canKeepSaved && SECRET_FIELDS.has(field) ? text.keepSaved : text.field[field]?.placeholder;
        const onValue = (value: string) => setValues((previous) => ({ ...previous, [field]: value }));

        if (field === 'protocol') {
            return (
                <Select
                    disabled={!isEditing || isBusy}
                    value={PROTOCOLS[0]}
                >
                    <SelectTrigger
                        className="w-full sm:w-72"
                        id={id}
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        {PROTOCOLS.map((protocol) => (
                            <SelectItem
                                disabled={protocol !== PROTOCOLS[0]}
                                key={protocol}
                                value={protocol}
                            >
                                {text.protocol[protocol]}
                            </SelectItem>
                        ))}
                    </SelectContent>
                </Select>
            );
        }

        if (!isEditing) {
            return (
                <div
                    className="bg-muted min-h-9 rounded-md px-3 py-2 font-mono text-sm break-all"
                    id={id}
                >
                    {field === 'tls'
                        ? (text.tlsOption[setting?.fields.tls ?? ''] ?? copy.common.dash)
                        : MULTILINE_FIELDS.has(field) && setting?.fields[field]
                          ? setting.fields[field].split('\n').map((line, index) => <div key={index}>{line}</div>)
                          : setting?.fields[field] || copy.common.dash}
                </div>
            );
        }

        if (field === 'tls') {
            return (
                <Select
                    disabled={isBusy}
                    onValueChange={onValue}
                    value={values.tls ?? setting?.fields.tls ?? TLS_MODES[0]}
                >
                    <SelectTrigger
                        className="w-full"
                        id={id}
                    >
                        <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                        {TLS_MODES.map((mode) => (
                            <SelectItem
                                key={mode}
                                value={mode}
                            >
                                {text.tlsOption[mode]}
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
            <Label htmlFor={`credential-${kind}-${field}`}>{text.field[field]?.label ?? field}</Label>
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
                <p className={state === 'invalid' ? 'text-destructive text-xs' : 'text-muted-foreground text-xs'}>
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
                                        {text.providerLabel[option] ?? option}
                                    </SelectItem>
                                ))}
                            </SelectContent>
                        </Select>
                    </div>
                )}

                {guide && !isCustomLlm && <p className="text-muted-foreground text-sm">{guide}</p>}
                {kind === 'llm' && <p className="text-muted-foreground text-xs">{text.llmTopUpNote}</p>}
                {kind === 'telegram' && <p className="text-muted-foreground text-xs">{text.telegramNote}</p>}

                <div className="grid gap-3 sm:grid-cols-2">{mainFields.map(renderField)}</div>

                {overrideFields.length > 0 && (
                    <Collapsible
                        className="border-t pt-3"
                        defaultOpen={overrideFields.some((field) => !!setting?.fields[field])}
                    >
                        <CollapsibleTrigger className="group flex items-center gap-1 text-sm font-medium">
                            <ChevronRight className="size-4 transition-transform group-data-[state=open]:rotate-90" />
                            {text.llmMode.overrides}
                        </CollapsibleTrigger>
                        <CollapsibleContent className="mt-3 flex flex-col gap-3">
                            <p className="text-muted-foreground text-xs">{text.llmMode.overridesHint}</p>
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
            </CardContent>
        </Card>
    );
}
