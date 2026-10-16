export const MAX_SOURCE_LINKS = 100;

const TELEGRAM_LINK =
    /^(?:https?:\/\/)?(?:www\.)?(?:t\.me|telegram\.me|telegram\.dog)\/(?:\+[\w-]+|joinchat\/[\w-]+|[a-z]\w{3,31})\/?$/i;
const TELEGRAM_HANDLE = /^@[a-z]\w{3,31}$/i;

export const splitSourceLinks = (text: string): string[] => [
    ...new Set(
        text
            .split(/[\s,，、]+/)
            .map((link) => link.trim())
            .filter(Boolean),
    ),
];

export const isTelegramLink = (link: string): boolean => TELEGRAM_LINK.test(link) || TELEGRAM_HANDLE.test(link);
