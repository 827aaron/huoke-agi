import { Plus, X } from 'lucide-react';
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
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Spinner } from '@/components/ui/spinner';
import { Textarea } from '@/components/ui/textarea';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';

import { growthApi } from '../growth-api';
import {
    GROWTH_PLATFORMS,
    type GrowthLead,
    type GrowthLeadContact,
    type GrowthLeadTier,
    type GrowthPlatform,
} from '../growth-types';
import { CONTACT_TYPES } from '../settings/growth-profile-form';

const text = copy.leads;

const BUILT_IN_CONTACTS = CONTACT_TYPES.filter((type) => type !== 'platform_handle');

interface LeadFormDialogProps {
    flowId: null | number;
    // Without a lead the dialog adds one by hand.
    lead?: GrowthLead;
    onClose: () => void;
    onSaved: () => Promise<void>;
}

function LeadFormDialog({ flowId, lead, onClose, onSaved }: LeadFormDialogProps) {
    const [platform, setPlatform] = useState<GrowthPlatform>('telegram');
    const [handle, setHandle] = useState('');
    const [displayName, setDisplayName] = useState('');
    const [tier, setTier] = useState<GrowthLeadTier>(lead?.tier ?? 'qualified');
    const [contacts, setContacts] = useState<GrowthLeadContact[]>(lead?.contacts ?? []);
    const [note, setNote] = useState(lead?.note ?? '');
    const [error, setError] = useState<null | string>(null);
    const [isSaving, setIsSaving] = useState(false);

    const contactTypes = [
        ...BUILT_IN_CONTACTS,
        ...contacts.map(({ type }) => type).filter((type) => !BUILT_IN_CONTACTS.includes(type as never)),
    ].filter((type, index, all) => all.indexOf(type) === index);

    const changeContact = (index: number, patch: Partial<GrowthLeadContact>) =>
        setContacts((previous) => previous.map((item, at) => (at === index ? { ...item, ...patch } : item)));

    const handleSave = async () => {
        setIsSaving(true);
        setError(null);

        const filled = contacts.map((item) => ({ ...item, value: item.value.trim() })).filter((item) => item.value);

        try {
            if (lead) {
                await growthApi.updateLead(lead.id, { contacts: filled, note: note.trim(), tier });
                toast.success(text.saved);
            } else {
                await growthApi.createLead({
                    contacts: filled,
                    display_name: displayName.trim() || undefined,
                    flow_id: flowId ?? undefined,
                    handle: handle.trim(),
                    note: note.trim(),
                    platform,
                    tier,
                });
                toast.success(text.added);
            }

            await onSaved();
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
            <DialogContent className="max-h-[90dvh] overflow-y-auto sm:max-w-lg">
                <DialogHeader>
                    <DialogTitle>{lead ? text.edit : text.add}</DialogTitle>
                    <DialogDescription>{lead ? lead.display_name : text.form.manualHint}</DialogDescription>
                </DialogHeader>

                {!lead && (
                    <div className="grid gap-3 sm:grid-cols-2">
                        <div className="flex flex-col gap-1.5">
                            <Label htmlFor="lead-platform">{text.form.platform}</Label>
                            <Select
                                onValueChange={(value) => setPlatform(value as GrowthPlatform)}
                                value={platform}
                            >
                                <SelectTrigger id="lead-platform">
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    {GROWTH_PLATFORMS.map((item) => (
                                        <SelectItem
                                            key={item}
                                            value={item}
                                        >
                                            {copy.telegram.health.platform[item] ?? item}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                        </div>
                        <div className="flex flex-col gap-1.5">
                            <Label htmlFor="lead-handle">{text.form.handle}</Label>
                            <Input
                                autoComplete="off"
                                id="lead-handle"
                                onChange={(event) => setHandle(event.target.value)}
                                placeholder={text.form.handlePlaceholder}
                                value={handle}
                            />
                        </div>
                        <div className="flex flex-col gap-1.5 sm:col-span-2">
                            <Label htmlFor="lead-name">{text.form.displayName}</Label>
                            <Input
                                autoComplete="off"
                                id="lead-name"
                                onChange={(event) => setDisplayName(event.target.value)}
                                value={displayName}
                            />
                        </div>
                    </div>
                )}

                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="lead-tier">{text.form.tier}</Label>
                    <Select
                        onValueChange={(value) => setTier(value as GrowthLeadTier)}
                        value={tier}
                    >
                        <SelectTrigger
                            className="w-40"
                            id="lead-tier"
                        >
                            <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                            <SelectItem value="qualified">{text.tier.qualified}</SelectItem>
                            <SelectItem value="watch">{text.tier.watch}</SelectItem>
                        </SelectContent>
                    </Select>
                </div>

                <fieldset className="flex flex-col gap-2">
                    <legend className="mb-1.5 text-sm font-medium">{text.form.contacts}</legend>
                    {contacts.map((contact, index) => (
                        <div
                            className="flex gap-2"
                            key={index}
                        >
                            <Select
                                onValueChange={(type) => changeContact(index, { type })}
                                value={contact.type}
                            >
                                <SelectTrigger
                                    aria-label={`${text.form.contacts} ${index + 1}`}
                                    className="w-32 shrink-0"
                                >
                                    <SelectValue />
                                </SelectTrigger>
                                <SelectContent>
                                    {contactTypes.map((type) => (
                                        <SelectItem
                                            key={type}
                                            value={type}
                                        >
                                            {copy.profile.contactType[type] ?? type}
                                        </SelectItem>
                                    ))}
                                </SelectContent>
                            </Select>
                            <Input
                                aria-label={`${text.form.contactValue} ${index + 1}`}
                                autoComplete="off"
                                onChange={(event) => changeContact(index, { value: event.target.value })}
                                placeholder={text.form.contactValue}
                                value={contact.value}
                            />
                            <Button
                                aria-label={`${text.form.contactRemove} ${index + 1}`}
                                onClick={() => setContacts((previous) => previous.filter((_, at) => at !== index))}
                                size="icon"
                                variant="ghost"
                            >
                                <X />
                            </Button>
                        </div>
                    ))}
                    <Button
                        className="self-start"
                        onClick={() => setContacts((previous) => [...previous, { type: 'phone', value: '' }])}
                        size="sm"
                        variant="outline"
                    >
                        <Plus />
                        {text.form.contactAdd}
                    </Button>
                </fieldset>

                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="lead-note">{text.form.note}</Label>
                    <Textarea
                        id="lead-note"
                        onChange={(event) => setNote(event.target.value)}
                        placeholder={text.form.notePlaceholder}
                        rows={3}
                        value={note}
                    />
                    {error && <p className="text-destructive text-xs">{error}</p>}
                </div>

                <DialogFooter>
                    <Button
                        disabled={isSaving}
                        onClick={onClose}
                        variant="outline"
                    >
                        {copy.action.cancel}
                    </Button>
                    <Button
                        disabled={isSaving || (!lead && !handle.trim())}
                        onClick={handleSave}
                    >
                        {isSaving && <Spinner variant="circle" />}
                        {copy.action.save}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

export default LeadFormDialog;
