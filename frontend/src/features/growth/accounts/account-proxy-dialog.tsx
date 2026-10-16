import { PlugZap } from 'lucide-react';
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
import { Label } from '@/components/ui/label';
import { Spinner } from '@/components/ui/spinner';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';

import type { GrowthAccount } from '../growth-types';

import { accountName } from '../account-health';
import { growthApi } from '../growth-api';
import { isProxyUrl } from '../settings/growth-credential-row';

const text = copy.telegram.proxy;

interface AccountProxyDialogProps {
    account: GrowthAccount;
    onClose: () => void;
    onSaved: (account: GrowthAccount) => void;
}

// The saved proxy comes back as host:port only, so the field starts empty; saving it empty falls back
// to the default proxy.
function AccountProxyDialog({ account, onClose, onSaved }: AccountProxyDialogProps) {
    const [proxy, setProxy] = useState('');
    const [error, setError] = useState<null | string>(null);
    const [isTesting, setIsTesting] = useState(false);
    const [isSaving, setIsSaving] = useState(false);

    const value = proxy.trim();
    const isValid = !value || isProxyUrl(value);

    const handleTest = async () => {
        setIsTesting(true);
        setError(null);

        try {
            const result = await growthApi.testAccountProxy(account.id, value || undefined);

            if (result.ok) {
                toast.success(text.testOk);
            } else {
                setError(text.testFailed(result.error ?? copy.common.dash));
            }
        } catch (testError) {
            setError(getApiErrorMessage(testError, text.testFailed(copy.common.dash)));
        } finally {
            setIsTesting(false);
        }
    };

    const handleSave = async () => {
        setIsSaving(true);
        setError(null);

        try {
            onSaved(await growthApi.setAccountProxy(account.id, value || null));
            toast.success(text.saved);
            onClose();
        } catch (saveError) {
            setError(getApiErrorMessage(saveError, copy.common.saveFailed));
        } finally {
            setIsSaving(false);
        }
    };

    return (
        <Dialog
            onOpenChange={(isOpen) => !isOpen && onClose()}
            open
        >
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>{text.title(accountName(account))}</DialogTitle>
                    <DialogDescription>{account.proxy ?? text.direct}</DialogDescription>
                </DialogHeader>
                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="account-proxy">{text.field}</Label>
                    <Input
                        aria-invalid={!isValid}
                        autoComplete="off"
                        id="account-proxy"
                        onChange={(event) => setProxy(event.target.value)}
                        placeholder={text.placeholder}
                        value={proxy}
                    />
                    <p className="text-muted-foreground text-xs">{text.hint}</p>
                    {!isValid && <p className="text-destructive text-xs">{copy.credentials.fieldInvalid.url}</p>}
                    {error && <p className="text-destructive text-xs">{error}</p>}
                </div>
                <DialogFooter>
                    <Button
                        disabled={!isValid || isTesting || isSaving}
                        onClick={handleTest}
                        variant="outline"
                    >
                        {isTesting ? <Spinner variant="circle" /> : <PlugZap />}
                        {text.test}
                    </Button>
                    <Button
                        disabled={!isValid || isTesting || isSaving}
                        onClick={handleSave}
                    >
                        {isSaving && <Spinner variant="circle" />}
                        {text.save}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

export default AccountProxyDialog;
