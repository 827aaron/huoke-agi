import { describe, expect, it } from 'vitest';

import { splitList } from './growth-profile-form';

describe('splitList', () => {
    it('splits on newlines and on ASCII and full-width separators', () => {
        expect(splitList('星穹纪元，Star Era、Nova\nOrion;Zenith；Apex')).toEqual([
            '星穹纪元',
            'Star Era',
            'Nova',
            'Orion',
            'Zenith',
            'Apex',
        ]);
    });

    it('trims entries, drops blanks and removes duplicates, keeping the first spelling', () => {
        expect(splitList('  Nova ,, Nova ,\n\n Orion  ')).toEqual(['Nova', 'Orion']);
    });

    it('treats case as significant', () => {
        expect(splitList('Nova,nova')).toEqual(['Nova', 'nova']);
    });

    it('returns an empty list for blank input', () => {
        expect(splitList(' \n , ')).toEqual([]);
    });
});
