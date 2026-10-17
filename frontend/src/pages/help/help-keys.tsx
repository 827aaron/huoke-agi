import { BookOpen } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';

import { AppHeader, AppHeaderContent, AppHeaderTitle } from '@/components/layouts/app/app-header';
import Markdown from '@/components/shared/markdown';
import { Spinner } from '@/components/ui/spinner';
import { copy } from '@/lib/copy';

const text = copy.helpKeys;

// The guide numbers its top-level sections; each credentials card links here with its kind as the hash.
const SECTION_OF_KIND: Record<string, string> = {
    email: '5.',
    embedding: '1.',
    llm: '1.',
    proxy: '6.',
    telegram: '2.',
    web_search: '3.',
    youtube: '4.',
};

function HelpKeys() {
    const { hash } = useLocation();
    const containerRef = useRef<HTMLDivElement>(null);
    const [guide, setGuide] = useState<null | string>(null);
    const [isFailed, setIsFailed] = useState(false);

    useEffect(() => {
        let isCancelled = false;

        fetch('/help/keys.md')
            .then((response) => (response.ok ? response.text() : Promise.reject(new Error(String(response.status)))))
            .then((body) => !isCancelled && setGuide(body))
            .catch(() => !isCancelled && setIsFailed(true));

        return () => {
            isCancelled = true;
        };
    }, []);

    useEffect(() => {
        const prefix = SECTION_OF_KIND[hash.slice(1)];

        if (!guide || !prefix) {
            return;
        }

        const heading = [...(containerRef.current?.querySelectorAll('h2') ?? [])].find((element) =>
            element.textContent?.trim().startsWith(prefix),
        );

        heading?.scrollIntoView({ block: 'start' });
    }, [guide, hash]);

    return (
        <>
            <AppHeader>
                <AppHeaderContent>
                    <AppHeaderTitle icon={<BookOpen className="size-4 shrink-0" />}>{text.title}</AppHeaderTitle>
                </AppHeaderContent>
            </AppHeader>
            <div
                className="mx-auto w-full max-w-4xl p-4"
                ref={containerRef}
            >
                {guide ? (
                    <Markdown>{guide}</Markdown>
                ) : isFailed ? (
                    <div className="text-destructive text-sm">{text.loadFailed}</div>
                ) : (
                    <div className="flex justify-center py-8">
                        <Spinner variant="circle" />
                    </div>
                )}
            </div>
        </>
    );
}

export default HelpKeys;
