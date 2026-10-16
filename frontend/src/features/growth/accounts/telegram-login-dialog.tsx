import { CircleCheck } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { InputPassword } from '@/components/ui/input-password';
import { Label } from '@/components/ui/label';
import { Spinner } from '@/components/ui/spinner';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';

import type { GrowthAccount } from '../growth-types';

import { accountName } from '../account-health';
import { growthApi } from '../growth-api';
import { isProxyUrl } from '../settings/growth-credential-row';

const text = copy.telegram.login;

type Step = 'code' | 'done' | 'password' | 'phone';

const STEP_NUMBER: Record<Step, number> = { code: 2, done: 3, password: 3, phone: 1 };

interface TelegramLoginDialogProps {
    // Set when signing an existing account in again; its phone is prefilled.
    account?: GrowthAccount | null;
    onClose: () => void;
    onSignedIn: (account: GrowthAccount) => void;
    open: boolean;
}

function TelegramLoginDialog({ account, onClose, onSignedIn, open }: TelegramLoginDialogProps) {
    const [step, setStep] = useState<Step>('phone');
    const [phone, setPhone] = useState(account?.external_id ?? '');
    const [proxy, setProxy] = useState('');
    const [code, setCode] = useState('');
    const [password, setPassword] = useState('');
    const [loginId, setLoginId] = useState<null | string>(null);
    const [signedIn, setSignedIn] = useState<GrowthAccount | null>(null);
    const [error, setError] = useState<null | string>(null);
    const [isBusy, setIsBusy] = useState(false);

    const run = async (action: () => Promise<void>) => {
        setIsBusy(true);
        setError(null);

        try {
            await action();
        } catch (actionError) {
            setError(getApiErrorMessage(actionError, text.failed));
        } finally {
            setIsBusy(false);
        }
    };

    const finish = (next: GrowthAccount | null) => {
        if (!next) {
            return;
        }

        setSignedIn(next);
        setStep('done');
        onSignedIn(next);
        toast.success(text.done(accountName(next)));
    };

    const handleNext = () =>
        run(async () => {
            if (step === 'phone') {
                if (proxy.trim() && !isProxyUrl(proxy.trim())) {
                    setError(copy.credentials.fieldInvalid.url ?? '');

                    return;
                }

                const result = await growthApi.startTelegramLogin({
                    account_id: account?.id,
                    phone: phone.trim(),
                    proxy: proxy.trim() || null,
                });

                setLoginId(result.login_id);
                setStep('code');

                return;
            }

            if (!loginId) {
                return;
            }

            const result =
                step === 'code'
                    ? await growthApi.submitTelegramCode(loginId, code.trim())
                    : await growthApi.submitTelegramPassword(loginId, password);

            if (result.next === 'password') {
                setStep('password');
            } else {
                finish(result.account);
            }
        });

    const canContinue =
        (step === 'phone' && /^\+?\d{6,15}$/.test(phone.trim())) ||
        (step === 'code' && /^\d{5,6}$/.test(code.trim())) ||
        (step === 'password' && password.length > 0);

    return (
        <Dialog
            onOpenChange={(isOpen) => !isOpen && onClose()}
            open={open}
        >
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>{account ? text.titleRelogin : text.title}</DialogTitle>
                    <DialogDescription>{text.step(STEP_NUMBER[step])}</DialogDescription>
                </DialogHeader>

                {step === 'phone' && (
                    <div className="flex flex-col gap-4">
                        <div className="flex flex-col gap-1.5">
                            <Label htmlFor="telegram-login-phone">{text.phone}</Label>
                            <Input
                                autoComplete="tel"
                                disabled={isBusy || !!account}
                                id="telegram-login-phone"
                                onChange={(event) => setPhone(event.target.value)}
                                placeholder={text.phonePlaceholder}
                                value={phone}
                            />
                        </div>
                        <div className="flex flex-col gap-1.5">
                            <Label htmlFor="telegram-login-proxy">{copy.telegram.proxy.field}</Label>
                            <Input
                                autoComplete="off"
                                disabled={isBusy}
                                id="telegram-login-proxy"
                                onChange={(event) => setProxy(event.target.value)}
                                placeholder={copy.telegram.proxy.placeholder}
                                value={proxy}
                            />
                            <p className="text-muted-foreground text-xs">{copy.telegram.proxy.hint}</p>
                        </div>
                    </div>
                )}

                {step === 'code' && (
                    <div className="flex flex-col gap-1.5">
                        <Label htmlFor="telegram-login-code">{text.code}</Label>
                        <Input
                            autoComplete="one-time-code"
                            disabled={isBusy}
                            id="telegram-login-code"
                            inputMode="numeric"
                            onChange={(event) => setCode(event.target.value)}
                            placeholder={text.codePlaceholder}
                            value={code}
                        />
                        <p className="text-muted-foreground text-xs">{text.codeHint(phone.trim())}</p>
                    </div>
                )}

                {step === 'password' && (
                    <div className="flex flex-col gap-1.5">
                        <Label htmlFor="telegram-login-password">{text.password}</Label>
                        <InputPassword
                            autoComplete="current-password"
                            disabled={isBusy}
                            id="telegram-login-password"
                            onChange={(event) => setPassword(event.target.value)}
                            value={password}
                        />
                        <p className="text-muted-foreground text-xs">{text.passwordHint}</p>
                    </div>
                )}

                {step === 'done' && signedIn && (
                    <div className="flex items-center gap-2 rounded-md border p-4 text-sm">
                        <CircleCheck className="size-5 text-green-500" />
                        {text.done(accountName(signedIn))}
                    </div>
                )}

                {error && <p className="text-destructive text-sm">{error}</p>}

                <DialogFooter>
                    {step === 'done' ? (
                        <Button onClick={onClose}>{text.finish}</Button>
                    ) : (
                        <>
                            {step !== 'phone' && (
                                <Button
                                    disabled={isBusy}
                                    onClick={() => {
                                        setError(null);
                                        setStep('phone');
                                    }}
                                    variant="ghost"
                                >
                                    {text.back}
                                </Button>
                            )}
                            <Button
                                disabled={isBusy || !canContinue}
                                onClick={handleNext}
                            >
                                {isBusy && <Spinner variant="circle" />}
                                {step === 'phone' ? text.sendCode : text.signIn}
                            </Button>
                        </>
                    )}
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

export default TelegramLoginDialog;
