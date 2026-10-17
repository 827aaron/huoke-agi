import { useState } from 'react';
import { toast } from 'sonner';
import * as z from 'zod';

import { Button } from '@/components/ui/button';
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form';
import { FormSubmitButton } from '@/components/ui/form-submit-button';
import { Input } from '@/components/ui/input';
import { InputPassword } from '@/components/ui/input-password';
import { useAppForm } from '@/hooks/use-app-form';
import { api, resolveApiErrorMessage } from '@/lib/axios';
import { copy } from '@/lib/copy';
import { useUser } from '@/providers/user-provider';

const emailChangeFormSchema = z.object({
    currentPassword: z.string().min(1, { message: copy.auth.currentPasswordRequired }),
    newEmail: z
        .string()
        .trim()
        .toLowerCase()
        .min(1, { message: copy.auth.emailRequired })
        .email({ message: copy.auth.emailInvalid })
        .max(50, { message: copy.auth.emailTooLong }),
});

const ERROR_BY_CODE: Record<string, string> = {
    'Users.ChangeEmailCurrentUser.EmailAlreadyExists': copy.auth.emailInUse,
    'Users.ChangeEmailCurrentUser.InvalidCurrentPassword': copy.auth.currentPasswordIncorrect,
    'Users.ChangeEmailCurrentUser.InvalidEmail': copy.auth.emailRejected,
    'Users.NotFound': copy.auth.userNotFound,
};

interface EmailChangeFormProps {
    onCancel?: () => void;
    onSuccess?: () => void;
}

type EmailChangeFormValues = z.infer<typeof emailChangeFormSchema>;

export function EmailChangeForm({ onCancel, onSuccess }: EmailChangeFormProps) {
    const [error, setError] = useState<null | string>(null);
    const { patchUser, refreshAuthInfo } = useUser();

    const form = useAppForm<EmailChangeFormValues>({
        defaultValues: {
            currentPassword: '',
            newEmail: '',
        },
        schema: emailChangeFormSchema,
    });

    const handleSubmit = async (values: EmailChangeFormValues) => {
        setError(null);

        try {
            await api.put('/user/email', {
                current_password: values.currentPassword,
                mail: values.newEmail,
            });

            form.reset();
            toast.success(copy.auth.emailUpdated);

            patchUser({ mail: values.newEmail });
            await refreshAuthInfo();

            onSuccess?.();
        } catch (err: unknown) {
            setError(resolveApiErrorMessage(err, ERROR_BY_CODE, copy.auth.emailFailed));
        }
    };

    return (
        <Form {...form}>
            {/* noValidate: the type="email" field would otherwise fire the browser's native (locale-styled)
                validation popup on submit, pre-empting our zod message. Validation runs through zod instead. */}
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
                    name="newEmail"
                    render={({ field }) => (
                        <FormItem>
                            <FormLabel>{copy.auth.newEmail}</FormLabel>
                            <FormControl>
                                <Input
                                    {...field}
                                    placeholder={copy.auth.newEmailPlaceholder}
                                    type="email"
                                />
                            </FormControl>
                            <FormMessage />
                        </FormItem>
                    )}
                />

                {error && <div className="text-destructive text-sm">{error}</div>}

                <div className="flex justify-end gap-2 pt-2">
                    {onCancel && (
                        <Button
                            onClick={onCancel}
                            size="sm"
                            type="button"
                            variant="outline"
                        >
                            {copy.action.cancel}
                        </Button>
                    )}
                    <FormSubmitButton size="sm">
                        <span>{copy.auth.updateEmail}</span>
                    </FormSubmitButton>
                </div>
            </form>
        </Form>
    );
}
