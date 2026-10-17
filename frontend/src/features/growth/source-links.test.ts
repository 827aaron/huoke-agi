import { describe, expect, it } from 'vitest';

import { isTelegramLink, splitSourceLinks } from './source-links';

describe('splitSourceLinks', () => {
    it('splits on whitespace and commas and drops duplicates', () => {
        expect(splitSourceLinks('https://t.me/a_group\n https://t.me/a_group，@other_one  t.me/+abc')).toEqual([
            'https://t.me/a_group',
            '@other_one',
            't.me/+abc',
        ]);
    });
});

describe('isTelegramLink', () => {
    it.each([
        ['https://t.me/slg_players', true],
        ['t.me/+k3QxR8sLmA1hNjVl', true],
        ['https://t.me/joinchat/AAAAAE', true],
        ['https://telegram.me/slg_players/', true],
        ['@slg_players', true],
        ['https://t.me/abc', false],
        ['https://example.com/slg_players', false],
        ['slg_players', false],
    ])('%s → %s', (link, expected) => {
        expect(isTelegramLink(link)).toBe(expected);
    });
});
