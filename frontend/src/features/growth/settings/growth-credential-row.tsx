import { Loader2, Mail, PlugZap, Save } from 'lucide-react';
import { useState } from 'react';
import { Link } from 'react-router-dom';
import { toast } from 'sonner';

import { Badge, type BadgeVariant } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card';
import { Input } from '@/components/ui/input';
import { InputPassword } from '@/components/ui/input-password';
import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Spinner } from '@/components/ui/spinner';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';

import type { GrowthSetting, GrowthSettingInput, GrowthSettingKind, GrowthSettingStatus } from '../growth-types';

import { growthApi } from '../growth-api';

const text = copy.credentials;

export const LLM_PROVIDERS = [
    'openai',
    'anthropic',
    'gemini',
    'deepseek',
    'kimi',
    'qwen',
    'glm',
    'minimax',
    'mistral',
    'xai',
    'custom',
] as const;

export const SEARCH_PROVIDERS = ['tavily', 'perplexity', 'firecrawl', 'traversaal', 'google'] as const;

const EMAIL = /^[^\s@,]+@[^\s@,]+\.[^\s@,]+$/;

const FIELD_CHECKS: Record<string, (value: string) => boolean> = {
    api_hash: (value) => /^[\da-z]{32}$/i.test(value),
    api_id: (value) => /^\d+$/.test(value),
    notify_emails: (value) => value.split(',').every((address) => EMAIL.test(address.trim())),
    smtp_from: (value) => EMAIL.test(value),
    smtp_port: (value) => /^\d+$/.test(value) && Number(value) >= 1 && Number(value) <= 65535,
};

// Shown as typed; everything else is a secret and goes through the masked password input.
const PLAIN_FIELDS = new Set(['notify_emails', 'smtp_from', 'smtp_host', 'smtp_port', 'smtp_user']);

export const REQUIRED_KINDS: GrowthSettingKind[] = ['llm', 'telegram'];

export const settingId = (kind: GrowthSettingKind, provider: null | string): string =>
    kind === 'web_search' ? `${kind}:${provider}` : kind;

const fieldsFor = (kind: GrowthSettingKind, provider: null | string): string[] => {
    switch (kind) {
        case 'email':
            return ['smtp_host', 'smtp_port', 'smtp_user', 'smtp_password', 'smtp_from', 'notify_emails'];
        case 'llm':
            return provider === 'custom' ? ['api_key', 'server_url'] : ['api_key'];
        case 'telegram':
            return ['api_id', 'api_hash'];
        case 'web_search':
            return provider === 'google' ? ['api_key', 'cx'] : ['api_key'];
        default:
            return ['api_key'];
    }
};

const providersFor = (kind: GrowthSettingKind): null | readonly string[] =>
    kind === 'llm' ? LLM_PROVIDERS : kind === 'web_search' ? SEARCH_PROVIDERS : null;

const statusVariant: Record<GrowthSettingStatus, BadgeVariant> = {
    connected: 'green',
    invalid: 'red',
    unset: 'secondary',
};

const reasonText = (setting: GrowthSetting) =>
    (setting.reason_code && text.reason[setting.reason_code]) || text.unknownReason;

const statusLine = (kind: GrowthSettingKind, setting: GrowthSetting | undefined): string => {
    if (setting?.status === 'connected') {
        return text.statusLine.connected(setting.tested_at ? copy.common.relative(new Date(setting.tested_at)) : '');
    }

    if (setting?.status === 'invalid') {
        return text.statusLine.invalid(reasonText(setting));
    }

    if (kind === 'web_search') {
        return text.statusLine.unsetSearch;
    }

    return REQUIRED_KINDS.includes(kind) ? text.statusLine.unsetRequired : text.statusLine.unsetOptional;
};

interface GrowthCredentialCardProps {
    // Shown under the Telegram card once its credentials work but no account is signed in yet.
    isTelegramSignedIn?: boolean;
    isTesting?: boolean;
    kind: GrowthSettingKind;
    onChange: (previousId: null | string, next: GrowthSetting | null) => void;
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
    const providers = providersFor(kind);
    const card = text.card[kind] ?? { purpose: '', title: kind };

    const [isEditing, setIsEditing] = useState(!setting?.masked);
    const [provider, setProvider] = useState<null | string>(setting?.provider ?? providers?.[0] ?? null);
    const [values, setValues] = useState<Record<string, string>>({});
    const [errors, setErrors] = useState<Record<string, string>>({});
    const [isSaving, setIsSaving] = useState(false);
    const [isTesting, setIsTesting] = useState(false);

    const fields = fieldsFor(kind, provider);
    const isBusy = isSaving || isTesting || isAutoTesting;
    const status = setting?.status ?? 'unset';
    const guide = text.guide[kind === 'llm' || kind === 'web_search' ? (provider ?? '') : kind];

    const validate = (): null | Record<string, string> => {
        const nextErrors: Record<string, string> = {};
        const trimmed: Record<string, string> = {};

        for (const field of fields) {
            const value = (values[field] ?? '').trim();
            const label = text.field[field]?.label ?? field;

            if (!value) {
                nextErrors[field] = text.fieldRequired(label);
            } else if (FIELD_CHECKS[field] && !FIELD_CHECKS[field](value)) {
                nextErrors[field] = text.fieldInvalid[field] ?? text.reason.format ?? '';
            }

            trimmed[field] = value;
        }

        setErrors(nextErrors);

        return Object.keys(nextErrors).length ? null : trimmed;
    };

    const handleSave = async () => {
        const trimmed = validate();

        if (!trimmed) {
            return;
        }

        const input: GrowthSettingInput = { kind, provider: providers ? provider : null, values: trimmed };
        const id = settingId(kind, input.provider);

        setIsSaving(true);

        try {
            const saved = await growthApi.saveSetting(id, input);

            if (setting && setting.id !== id) {
                await growthApi.clearSetting(setting.id);
            }

            if (saved.status === 'connected') {
                toast.success(text.saved(card.title));
            } else {
                toast.error(text.savedButFailed(card.title, reasonText(saved)));
            }

            setValues({});
            setIsEditing(false);
            onChange(setting?.id ?? null, saved);
        } catch (error) {
            toast.error(getApiErrorMessage(error, copy.common.saveFailed));
        } finally {
            setIsSaving(false);
        }
    };

    const handleTest = async () => {
        if (!setting) {
            return;
        }

        setIsTesting(true);

        try {
            if (kind === 'email') {
                const { sent_to } = await growthApi.testEmail();

                toast.success(text.testEmailSent(sent_to.join(', ')));
            } else {
                onChange(setting.id, await growthApi.testSetting(setting.id));
            }
        } catch (error) {
            toast.error(getApiErrorMessage(error, copy.common.networkError));
        } finally {
            setIsTesting(false);
        }
    };

    const handleClear = async () => {
        if (!setting) {
            setValues({});

            return;
        }

        try {
            await growthApi.clearSetting(setting.id);
            toast.success(text.cleared(card.title));
            setValues({});
            setIsEditing(true);
            onChange(setting.id, null);
        } catch (error) {
            toast.error(getApiErrorMessage(error, copy.common.saveFailed));
        }
    };

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
                            <Badge variant={statusVariant[status]}>{text.status[status]}</Badge>
                        )}
                    </div>
                    {setting && (
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
                <p className={status === 'invalid' ? 'text-destructive text-xs' : 'text-muted-foreground text-xs'}>
                    {statusLine(kind, setting)}
                </p>
                <CardDescription>{card.purpose}</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
                {providers && (
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
                                {providers.map((option) => (
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

                {guide && <p className="text-muted-foreground text-sm">{guide}</p>}
                {kind === 'llm' && <p className="text-muted-foreground text-xs">{text.llmTopUpNote}</p>}
                {kind === 'telegram' && <p className="text-muted-foreground text-xs">{text.telegramNote}</p>}

                <div className="grid gap-3 sm:grid-cols-2">
                    {fields.map((field) => {
                        const id = `credential-${kind}-${field}`;
                        const masked = setting?.masked?.[field];

                        return (
                            <div
                                className="flex flex-col gap-1.5"
                                key={field}
                            >
                                <Label htmlFor={id}>{text.field[field]?.label ?? field}</Label>
                                {isEditing && PLAIN_FIELDS.has(field) ? (
                                    <Input
                                        aria-invalid={!!errors[field]}
                                        autoComplete="off"
                                        disabled={isBusy}
                                        id={id}
                                        onChange={(event) =>
                                            setValues((previous) => ({ ...previous, [field]: event.target.value }))
                                        }
                                        placeholder={text.field[field]?.placeholder}
                                        value={values[field] ?? ''}
                                    />
                                ) : isEditing ? (
                                    <InputPassword
                                        aria-invalid={!!errors[field]}
                                        autoComplete="off"
                                        disabled={isBusy}
                                        id={id}
                                        onChange={(event) =>
                                            setValues((previous) => ({ ...previous, [field]: event.target.value }))
                                        }
                                        placeholder={text.field[field]?.placeholder}
                                        value={values[field] ?? ''}
                                    />
                                ) : (
                                    <div
                                        className="bg-muted rounded-md px-3 py-2 font-mono text-sm"
                                        id={id}
                                    >
                                        {masked ?? copy.common.dash}
                                    </div>
                                )}
                                {errors[field] && <p className="text-destructive text-xs">{errors[field]}</p>}
                            </div>
                        );
                    })}
                </div>

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
                            {setting && (
                                <Button
                                    disabled={isBusy}
                                    onClick={() => {
                                        setIsEditing(false);
                                        setValues({});
                                        setErrors({});
                                        setProvider(setting.provider);
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
                                onClick={() => setIsEditing(true)}
                                size="sm"
                                variant="ghost"
                            >
                                {text.replace}
                            </Button>
                        </>
                    )}
                </div>

                {kind === 'telegram' && status === 'connected' && !isTelegramSignedIn && (
                    <Link
                        className="text-sm font-medium underline"
                        to={routes.settings.telegram}
                    >
                        {text.telegramNextStep}
                    </Link>
                )}
            </CardContent>
        </Card>
    );
}
