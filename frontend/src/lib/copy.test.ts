import { describe, expect, it } from 'vitest';

import { agentLabel, dictionaries } from './copy';

const keysOf = (node: unknown, prefix = ''): string[] =>
    node && typeof node === 'object' && !Array.isArray(node)
        ? Object.entries(node).flatMap(([key, value]) => keysOf(value, prefix ? `${prefix}.${key}` : key))
        : [prefix];

describe('dictionaries', () => {
    it('carry the same keys in both languages', () => {
        expect(keysOf(dictionaries.en).sort()).toEqual(keysOf(dictionaries.zh).sort());
    });
});

describe('agentLabel', () => {
    it.each([
        ['pentester', '潜客研究员'],
        ['primary_agent', '主控'],
        ['primaryAgent', '主控'],
        ['toolCallFixer', '工具修复员'],
    ])('%s → %s', (key, expected) => {
        expect(agentLabel(key)).toBe(expected);
    });

    it('falls back for a config with no agent behind it', () => {
        expect(agentLabel('simpleJson', 'Simple Json')).toBe('Simple Json');
    });
});
