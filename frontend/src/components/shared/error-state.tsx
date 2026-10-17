import { AlertCircle, RefreshCw } from 'lucide-react';

import { Button } from '@/components/ui/button';
import { Empty, EmptyContent, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { copy } from '@/lib/copy';

interface ErrorStateProps {
    message?: null | string;
    onRetry?: () => unknown;
    title: string;
}

export function ErrorState({ message, onRetry, title }: ErrorStateProps) {
    return (
        <Empty role="alert">
            <EmptyHeader>
                <EmptyMedia>
                    <AlertCircle className="text-destructive size-12" />
                </EmptyMedia>
                <EmptyTitle>{title}</EmptyTitle>
                {message ? <EmptyDescription>{message}</EmptyDescription> : null}
            </EmptyHeader>
            {onRetry ? (
                <EmptyContent>
                    <Button
                        onClick={() => onRetry()}
                        variant="secondary"
                    >
                        <RefreshCw />
                        {copy.action.retry}
                    </Button>
                </EmptyContent>
            ) : null}
        </Empty>
    );
}
