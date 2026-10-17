import type { ReactElement } from 'react';

import { Trash2 } from 'lucide-react';
import { cloneElement, isValidElement, useState } from 'react';

import { Button } from '@/components/ui/button';
import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
} from '@/components/ui/dialog';
import { Spinner } from '@/components/ui/spinner';
import { copy } from '@/lib/copy';
import { cn } from '@/lib/utils';

type ConfirmationDialogIconProps = ReactElement<React.SVGProps<SVGSVGElement>>;

interface ConfirmationDialogProps {
    cancelIcon?: ConfirmationDialogIconProps;
    cancelText?: string;
    cancelVariant?: 'default' | 'destructive' | 'ghost' | 'outline' | 'secondary';
    confirmIcon?: ConfirmationDialogIconProps;
    confirmText?: string;
    confirmVariant?: 'default' | 'destructive' | 'ghost' | 'outline' | 'secondary';
    description?: string;
    /** May be sync or async. If async, the dialog keeps itself open and shows a spinner until the promise settles. */
    handleConfirm: () => Promise<void> | void;
    handleOpenChange: (isOpen: boolean) => void;
    isOpen: boolean;
    itemName?: string;
    itemType?: string;
    title?: string;
}

function ConfirmationDialog({
    cancelIcon,
    cancelText = copy.action.cancel,
    cancelVariant = 'outline',
    confirmIcon = <Trash2 />,
    confirmText = copy.action.confirm,
    confirmVariant = 'destructive',
    description,
    handleConfirm,
    handleOpenChange,
    isOpen,
    itemName = copy.confirm.thisItem,
    itemType = copy.itemType.item,
    title,
}: ConfirmationDialogProps) {
    const [isProcessing, setIsProcessing] = useState(false);

    // The default confirmText means "no custom verb": a bare confirm gets the generic title
    // instead of "<verb> <itemType>".
    const verb = confirmText.trim();
    const isCustomVerb = verb && verb !== copy.action.confirm;
    const resolvedTitle = title ?? (isCustomVerb ? copy.confirm.title(verb, itemType) : copy.confirm.generic);
    const question = copy.confirm.question(verb || copy.action.confirm, itemType);

    const defaultDescription = description || (
        <>
            {question.before}
            <strong className="text-foreground font-semibold">{itemName}</strong>
            {question.after}
        </>
    );

    const processIcon = (icon?: ConfirmationDialogIconProps): ConfirmationDialogIconProps | null => {
        if (!icon) {
            return null;
        }

        if (isValidElement(icon)) {
            const { className = '', ...restProps } = icon.props;

            return cloneElement(icon, {
                ...restProps,
                className: cn('size-4', className),
            });
        }

        return icon;
    };

    const handleConfirmClick = async () => {
        if (isProcessing) {
            return;
        }

        setIsProcessing(true);

        try {
            await handleConfirm();
            handleOpenChange(false);
        } finally {
            setIsProcessing(false);
        }
    };

    return (
        <Dialog
            onOpenChange={(nextOpen) => {
                if (isProcessing) {
                    return;
                }

                handleOpenChange(nextOpen);
            }}
            open={isOpen}
        >
            <DialogContent className="sm:max-w-md">
                <DialogHeader>
                    <DialogTitle>{resolvedTitle}</DialogTitle>
                    <DialogDescription>{defaultDescription}</DialogDescription>
                </DialogHeader>

                <DialogFooter>
                    <Button
                        disabled={isProcessing}
                        onClick={() => handleOpenChange(false)}
                        variant={cancelVariant}
                    >
                        {processIcon(cancelIcon)}
                        {cancelText}
                    </Button>
                    <Button
                        disabled={isProcessing}
                        onClick={() => {
                            void handleConfirmClick();
                        }}
                        variant={confirmVariant}
                    >
                        {isProcessing ? <Spinner variant="circle" /> : processIcon(confirmIcon)}
                        {confirmText}
                    </Button>
                </DialogFooter>
            </DialogContent>
        </Dialog>
    );
}

export default ConfirmationDialog;
