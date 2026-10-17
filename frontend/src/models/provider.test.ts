import { describe, expect, it } from 'vitest';

import { ProviderType } from '@/graphql/types';

import { pickDefaultProvider } from './provider';

const providers = [
    { name: 'anthropic', type: ProviderType.Anthropic },
    { name: 'deepseek-team', type: ProviderType.Deepseek },
    { name: 'deepseek', type: ProviderType.Deepseek },
    { name: 'openai', type: ProviderType.Openai },
];

describe('pickDefaultProvider', () => {
    it.each([
        ['prefers the provider named after the preferred type', 'deepseek', 'deepseek'],
        ['keeps the first provider when the type is not offered', 'gemini', 'anthropic'],
        ['keeps the first provider without a preferred type', null, 'anthropic'],
    ])('%s', (_, preferredType, expected) => {
        expect(pickDefaultProvider(providers, preferredType)?.name).toBe(expected);
    });

    it('takes a lone provider of the preferred type whatever its name', () => {
        expect(pickDefaultProvider(providers.slice(0, 2), 'deepseek')?.name).toBe('deepseek-team');
    });

    it('returns null without providers', () => {
        expect(pickDefaultProvider([], 'openai')).toBeNull();
    });
});
