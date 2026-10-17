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
import { Textarea } from '@/components/ui/textarea';
import { getApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';

import type { GrowthSource } from '../growth-types';

import { growthApi } from '../growth-api';

const text = copy.sources;

interface SourceEditDialogProps {
    onClose: () => void;
    onSaved: () => Promise<void>;
    source: GrowthSource;
}

function SourceEditDialog({ onClose, onSaved, source }: SourceEditDialogProps) {
    const [title, setTitle] = useState(source.title);
    const [note, setNote] = useState(source.note);
    const [error, setError] = useState<null | string>(null);
    const [isSaving, setIsSaving] = useState(false);

    const handleSave = async () => {
        setIsSaving(true);
        setError(null);

        try {
            await growthApi.updateSource(source.id, { note: note.trim(), title: title.trim() });
            toast.success(text.saved);
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
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>{text.edit}</DialogTitle>
                    <DialogDescription className="break-all">{source.link}</DialogDescription>
                </DialogHeader>
                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="source-title">{text.form.title}</Label>
                    <Input
                        autoComplete="off"
                        id="source-title"
                        onChange={(event) => setTitle(event.target.value)}
                        value={title}
                    />
                </div>
                <div className="flex flex-col gap-1.5">
                    <Label htmlFor="source-note">{text.form.note}</Label>
                    <Textarea
                        id="source-note"
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
                        disabled={isSaving || !title.trim()}
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

export default SourceEditDialog;
