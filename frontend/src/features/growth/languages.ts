import { copy } from '@/lib/copy';

// Profiles saved before languages were stored as codes hold the language's own name.
const LEGACY_NAMES: Record<string, string> = {
    'Bahasa Indonesia': 'id',
    English: 'en',
    'Tiếng Việt': 'vi',
    العربية: 'ar',
    ไทย: 'th',
    한국어: 'ko',
    中文: 'zh',
    日本語: 'ja',
};

export const LANGUAGE_CODES = ['zh', 'en', 'ar', 'th', 'vi', 'id', 'ms', 'ja', 'ko'];

export const languageCode = (value: string): null | string =>
    value in copy.profile.languages ? value : (LEGACY_NAMES[value] ?? null);

// Anything that is not a known code is a language the operator typed in, shown as written.
export const languageLabel = (value: string): string => {
    const code = languageCode(value);

    return code ? (copy.profile.languages[code] ?? value) : value;
};
