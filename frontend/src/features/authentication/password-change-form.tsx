import { type ComponentProps, useState } from 'react';
import { toast } from 'sonner';
import * as z from 'zod';

import { Button } from '@/components/ui/button';
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form';
import { FormSubmitButton } from '@/components/ui/form-submit-button';
import { InputPassword } from '@/components/ui/input-password';
import { useAppForm } from '@/hooks/use-app-form';
import { api, resolveApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';
import { cn } from '@/lib/utils';

// The password policy of every form that sets a password; backend/pkg/password/policy.go enforces the same.
export const newPasswordSchema = z
    .string()
    .min(8, { message: copy.auth.passwordTooShort })
    // bcrypt, which hashes it server-side, refuses anything longer than 72 bytes.
    .refine((password) => new TextEncoder().encode(password).length <= 72, {
        message: copy.auth.passwordTooLong,
    })
    .refine(
        (password) => {
            const length = [...password].length;

            if (length > 15) {
                return true;
            }

            return (
                length >= 8 &&
                /[0-9]/.test(password) &&
                /[a-z]/.test(password) &&
                /[A-Z]/.test(password) &&
                /[!@#$&*]/.test(password)
            );
        },
        { message: copy.auth.passwordPolicy },
    );

const passwordChangeFormSchema = z
    .object({
        confirmPassword: z.string().min(1, { message: copy.auth.confirmPasswordRequired }),
        currentPassword: z.string().min(1, { message: copy.auth.currentPasswordRequired }),
        newPassword: newPasswordSchema,
    })
    .refine((data) => data.newPassword === data.confirmPassword, {
        message: copy.auth.passwordMismatch,
        path: ['confirmPassword'],
    })
    .refine((data) => data.currentPassword !== data.newPassword, {
        message: copy.auth.passwordSame,
        path: ['newPassword'],
    });

const ERROR_BY_CODE: Record<string, string> = {
    'Users.ChangePasswordCurrentUser.InvalidCurrentPassword': copy.auth.currentPasswordIncorrect,
    'Users.ChangePasswordCurrentUser.InvalidNewPassword': copy.auth.newPasswordRejected,
    'Users.ChangePasswordCurrentUser.InvalidPassword': copy.auth.passwordValidationFailed,
    'Users.NotFound': copy.auth.userNotFound,
};

interface PasswordChangeFormProps {
    buttonSize?: ComponentProps<typeof Button>['size'];
    layout?: 'horizontal' | 'vertical';
    onCancel?: () => void;
    onSkip?: () => void;
    onSuccess?: () => void;
}

type PasswordChangeFormValues = z.infer<typeof passwordChangeFormSchema>;

export function PasswordChangeForm({
    buttonSize = 'default',
    layout = 'horizontal',
    onCancel,
    onSkip,
    onSuccess,
}: PasswordChangeFormProps) {
    const [error, setError] = useState<null | string>(null);

    const form = useAppForm<PasswordChangeFormValues>({
        defaultValues: {
            confirmPassword: '',
            currentPassword: '',
            newPassword: '',
        },
        schema: passwordChangeFormSchema,
    });

    const handleSubmit = async (values: PasswordChangeFormValues) => {
        setError(null);

        try {
            await api.put('/user/password', {
                confirm_password: values.confirmPassword,
                current_password: values.currentPassword,
                password: values.newPassword,
            });

            form.reset();
            toast.success(copy.auth.passwordUpdated);

            onSuccess?.();
        } catch (err: unknown) {
            setError(resolveApiErrorMessage(err, ERROR_BY_CODE, copy.auth.passwordFailed));
        }
    };

    const isVertical = layout === 'vertical';

    const skipButton = onSkip && (
        <Button
            className={cn('text-muted-foreground', isVertical && 'w-full')}
            onClick={onSkip}
            size={buttonSize}
            type="button"
            variant="ghost"
        >
            {copy.auth.skip}
        </Button>
    );
    const cancelButton = onCancel && (
        <Button
            className={cn(isVertical && 'w-full')}
            onClick={onCancel}
            size={buttonSize}
            type="button"
            variant="outline"
        >
            {copy.action.cancel}
        </Button>
    );
    const submitButton = (
        <FormSubmitButton
            className={cn(isVertical && 'w-full')}
            size={buttonSize}
        >
            <span>{copy.auth.updatePassword}</span>
        </FormSubmitButton>
    );

    return (
        <Form {...form}>
            <form
                className="flex flex-col gap-4"
                noValidate
                onSubmit={form.handleSubmit(handleSubmit)}
            >
                <FormField
                    control={form.control}
                    name="currentPassword"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>{copy.auth.currentPassword}</FormLabel>
                            <FormControl>
                                <InputPassword
                                    {...field}
                                    placeholder={copy.auth.currentPasswordPlaceholder}
                                />
                            </FormControl>
                            <FormMessage />
                        </FormItem>
                    )}
                />

                <FormField
                    control={form.control}
                    name="newPassword"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>{copy.auth.newPassword}</FormLabel>
                            <FormControl>
                                <InputPassword
                                    {...field}
                                    placeholder={copy.auth.newPasswordPlaceholder}
                                />
                            </FormControl>
                            <FormDescription className="text-xs">{copy.auth.passwordHint}</FormDescription>
                            <FormMessage />
                        </FormItem>
                    )}
                />

                <FormField
                    control={form.control}
                    name="confirmPassword"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>{copy.auth.confirmNewPassword}</FormLabel>
                            <FormControl>
                                <InputPassword
                                    {...field}
                                    placeholder={copy.auth.confirmNewPasswordPlaceholder}
                                />
                            </FormControl>
                            <FormMessage />
                        </FormItem>
                    )}
                />

                {error && <div className="text-destructive text-sm">{error}</div>}

                {isVertical ? (
                    <div className="flex flex-col gap-2 pt-2">
                        {submitButton}
                        {cancelButton}
                        {skipButton}
                    </div>
                ) : (
                    <div className="flex justify-end gap-2 pt-2">
                        {skipButton}
                        {cancelButton}
                        {submitButton}
                    </div>
                )}
            </form>
        </Form>
    );
}
