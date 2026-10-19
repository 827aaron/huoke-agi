import { describe, expect, it } from 'vitest';

import { parseScoring, signalNames } from './profile-scoring';

const ok = (text: string) => {
    const parsed = parseScoring(text);

    if ('error' in parsed) {
        throw new Error(parsed.error.kind);
    }

    return parsed.scoring;
};

describe('parseScoring', () => {
    it('reads a three-part line, a short line and the thresholds', () => {
        const scoring = ok(
            [
                'player = 3 | Audience | The sender uses this kind of product.',
                'pain = 3 | unhappy with what they use',
                'agency = 5 | 代理经验 | Has run a betting agency.',
                'recent = 2',
                'qualified >= 7',
                'watchlist >= 4',
                'unwanted = -3',
            ].join('\n'),
        );

        expect(scoring.dimensions.map(({ key, name }) => [key, name])).toEqual([
            ['player', 'Audience'],
            ['pain', 'pain'],
            ['agency', '代理经验'],
        ]);
        expect(scoring.qualified).toBe(7);
        expect(scoring.watchlist).toBe(4);
        expect(scoring.recent).toBe(2);
    });

    it('fills a bare built-in key and slugs a name with no latin letters to dN', () => {
        const scoring = ok('player = 3\n代理 = 5\nqualified >= 5\nwatchlist >= 1');

        expect(scoring.dimensions).toEqual([
            { key: 'player', name: 'Audience' },
            { key: 'd2', name: '代理' },
        ]);
    });

    it('ignores comments and blank lines', () => {
        expect(ok('# note\n\nplayer = 3\nqualified >= 2\nwatchlist >= 1').dimensions).toEqual([
            { key: 'player', name: 'Audience' },
        ]);
    });

    it('rejects a line that is not a rule, points outside 0 to 20, and a watchlist that meets the qualified line', () => {
        expect(parseScoring('nope')).toEqual({ error: { kind: 'line', line: 1 } });
        expect(parseScoring('player = 21\nqualified >= 7\nwatchlist >= 4')).toEqual({
            error: { kind: 'points', line: 1 },
        });
        expect(parseScoring('player = 3\nqualified >= 4\nwatchlist >= 4')).toEqual({ error: { kind: 'watchlist' } });
        expect(parseScoring('player = 3\nqualified >= 9\nwatchlist >= 1')).toEqual({ error: { kind: 'unreachable' } });
        expect(parseScoring('')).toEqual({ error: { kind: 'no_dimension' } });
        expect(parseScoring('player = 3 |  | desc\nqualified >= 2\nwatchlist >= 1')).toEqual({
            error: { kind: 'name' },
        });
    });

    it('maps a signal key to the name the list shows', () => {
        expect(
            signalNames('agency = 5 | 代理经验 | Has run a betting agency.\nqualified >= 5\nwatchlist >= 1').get(
                'agency',
            ),
        ).toBe('代理经验');
        expect(signalNames('not a rule').size).toBe(0);
    });
});
