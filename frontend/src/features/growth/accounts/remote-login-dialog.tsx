import { ChevronDown, Monitor } from 'lucide-react';
import { useState } from 'react';
import { toast } from 'sonner';

import { Button } from '@/components/ui/button';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Spinner } from '@/components/ui/spinner';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';

import type { GrowthAccount, GrowthFingerprint, GrowthRemoteLogin } from '../growth-types';

import { accountName } from '../account-health';
import { growthApi } from '../growth-api';
import { isProxyUrl } from '../settings/growth-credential-row';

const text = copy.telegram.remote;
const fingerprintText = copy.telegram.fingerprint;

export const fingerprintSummary = (fingerprint: GrowthFingerprint) =>
    fingerprintText.auto(
        fingerprint.os ?? copy.common.dash,
        fingerprint.language ?? copy.common.dash,
        fingerprint.timezone ?? copy.common.dash,
    );

interface RemoteLoginDialogProps {
    account?: GrowthAccount | null;
    onClose: () => void;
    onSignedIn: (account: GrowthAccount) => void;
    open: boolean;
    platform: 'facebook' | 'instagram' | 'tiktok';
}

// Signing in happens inside a browser running on the server, streamed into the frame; the session cookie
// never passes through this page. Closing without "I have signed in" discards that browser.
function RemoteLoginDialog({ account, onClose, onSignedIn, open, platform }: RemoteLoginDialogProps) {
    const platformLabel = copy.telegram.health.platform[platform] ?? platform;

    const [proxy, setProxy] = useState('');
    const [externalWs, setExternalWs] = useState(account?.external_browser_ws ?? '');
    const [session, setSession] = useState<GrowthRemoteLogin | null>(null);
    const [error, setError] = useState<null | string>(null);
    const [isBusy, setIsBusy] = useState(false);

    const fingerprint = session?.fingerprint ?? account?.fingerprint ?? null;

    const handleOpen = async () => {
        if (proxy.trim() && !isProxyUrl(proxy.trim())) {
            setError(copy.credentials.fieldInvalid.url ?? '');

            return;
        }

        setIsBusy(true);
        setError(null);

        try {
            setSession(
                await growthApi.startRemoteLogin({
                    account_id: account?.id,
                    external_browser_ws: externalWs.trim() || null,
                    platform,
                    proxy: proxy.trim() || null,
                }),
            );
        } catch (openError) {
            setError(getApiErrorMessage(openError, text.unavailable));
        } finally {
            setIsBusy(false);
        }
    };

    const handleDone = async () => {
        if (!session) {
            return;
        }

        setIsBusy(true);
        setError(null);

        try {
            const signedIn = await growthApi.finishRemoteLogin(session.session_id);

            onSignedIn(signedIn);
            toast.success(copy.telegram.login.done(accountName(signedIn)));
            onClose();
        } catch (doneError) {
            setError(getApiErrorMessage(doneError, text.failed));
        } finally {
            setIsBusy(false);
        }
    };

    const handleClose = () => {
        if (session) {
            void growthApi.cancelRemoteLogin(session.session_id).catch(() => {});
        }

        onClose();
    };

    return (
        <Dialog
            onOpenChange={(isOpen) => !isOpen && handleClose()}
            open={open}
        >
            <DialogContent className={session ? 'sm:max-w-4xl' : 'sm:max-w-md'}>
                <DialogHeader>
                    <DialogTitle>{text.title(platformLabel)}</DialogTitle>
                    <DialogDescription>{text.hint}</DialogDescription>
                </DialogHeader>

                {!session ? (
                    <div className="flex flex-col gap-4">
                        <div className="flex flex-col gap-1.5">
                            <Label htmlFor="remote-login-proxy">{copy.telegram.proxy.field}</Label>
                            <Input
                                autoComplete="off"
                                disabled={isBusy || !!externalWs.trim()}
                                id="remote-login-proxy"
                                onChange={(event) => setProxy(event.target.value)}
                                placeholder={copy.telegram.proxy.placeholder}
                                value={proxy}
                            />
                            <p className="text-muted-foreground text-xs">{copy.telegram.proxy.hint}</p>
                        </div>
                        <Collapsible defaultOpen={!!externalWs}>
                            <CollapsibleTrigger className="text-muted-foreground flex items-center gap-1 text-sm hover:underline">
                                <ChevronDown className="size-4" />
                                {fingerprintText.advanced}
                            </CollapsibleTrigger>
                            <CollapsibleContent className="mt-3 flex flex-col gap-1.5">
                                <Label htmlFor="remote-login-ws">{fingerprintText.externalWs}</Label>
                                <Input
                                    autoComplete="off"
                                    className="font-mono"
                                    disabled={isBusy}
                                    id="remote-login-ws"
                                    onChange={(event) => setExternalWs(event.target.value)}
                                    placeholder={fingerprintText.externalPlaceholder}
                                    value={externalWs}
                                />
                                <p className="text-muted-foreground text-xs">{fingerprintText.externalHint}</p>
                            </CollapsibleContent>
                        </Collapsible>
                        {fingerprint && !externalWs.trim() && (
                            <p className="text-muted-foreground text-xs">{fingerprintSummary(fingerprint)}</p>
                        )}
                    </div>
                ) : (
                    <div className="flex flex-col gap-2">
                        {session.view_url ? (
                            <iframe
                                className="h-[60vh] w-full rounded-md border"
                                src={session.view_url}
                                title={text.title(platformLabel)}
                            />
                        ) : (
                            <div className="bg-muted text-muted-foreground flex h-[60vh] w-full flex-col items-center justify-center gap-3 rounded-md border text-sm">
                                <Monitor className="size-10" />
                                {text.placeholder}
                            </div>
                        )}
                        <p className="text-muted-foreground text-xs">
                            {externalWs.trim()
                                ? fingerprintText.usingExternal
                                : fingerprint
                                  ? fingerprintSummary(fingerprint)
                                  : null}
                        </p>
                    </div>
                )}

                {error && <p className="text-destructive text-sm">{error}</p>}

                <DialogFooter>
                    {!session ? (
                        <Button
                            disabled={isBusy}
                            onClick={handleOpen}
                        >
                            {isBusy && <Spinner variant="circle" />}
                            {isBusy ? text.loading : copy.telegram.remoteLogin}
                        </Button>
                    ) : (
                        <Button
                            disabled={isBusy}
                            onClick={handleDone}
                        >
                            {isBusy && <Spinner variant="circle" />}
                            {text.done}
                        </Button>
                    )}
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

export default RemoteLoginDialog;
