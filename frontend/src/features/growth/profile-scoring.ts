// The Jev rules as the profile stores them. The same lines are parsed in Go
// (ParseScoring); a key the list page shows has to come out identical.

const MAX_POINTS = 20;

const BUILT_IN: Record<string, string> = {
    match: 'Fit',
    pain: 'Pain or switching',
    paying: 'Paying',
    player: 'Audience',
};

const RESERVED = new Set(['active', 'unwanted']);

export interface ParsedScoring {
    dimensions: ScoringDimensionName[];
    qualified: number;
    recent: number;
    watchlist: number;
}

export interface ScoringDimensionName {
    key: string;
    name: string;
}

export type ScoringIssue =
    | { kind: 'line'; line: number }
    | { kind: 'name' }
    | { kind: 'no_dimension' }
    | { kind: 'points'; line: number }
    | { kind: 'unreachable' }
    | { kind: 'watchlist' };

interface DraftDimension {
    description: string;
    key: string;
    name: string;
    points: number;
}

const slug = (value: string) =>
    value
        .toLowerCase()
        .replaceAll(/[^a-z0-9]+/g, '_')
        .replaceAll(/^_+|_+$/g, '');

const whole = (value: string) => (/^-?\d+$/.test(value) ? Number(value) : null);

export function parseScoring(text: string): { error: ScoringIssue } | { scoring: ParsedScoring } {
    const dimensions: DraftDimension[] = [];
    let qualified = 0;
    let recent = 0;
    let watchlist = 0;

    const lines = text.split('\n');

    for (let index = 0; index < lines.length; index++) {
        const line = lines[index]?.trim() ?? '';

        if (!line || line.startsWith('#')) {
            continue;
        }

        const lineNo = index + 1;
        const thresholdAt = line.indexOf('>=');

        if (thresholdAt >= 0) {
            const label = line.slice(0, thresholdAt).trim().toLowerCase();
            const value = whole(line.slice(thresholdAt + 2).trim());

            if (value == null || value < 0 || (label !== 'qualified' && label !== 'watchlist')) {
                return { error: { kind: 'line', line: lineNo } };
            }

            if (label === 'qualified') {
                qualified = value;
            } else {
                watchlist = value;
            }

            continue;
        }

        const eq = line.indexOf('=');

        if (eq < 0) {
            return { error: { kind: 'line', line: lineNo } };
        }

        const left = line.slice(0, eq).trim();
        const right = line.slice(eq + 1).trim();
        const label = left.toLowerCase();

        if (label === 'qualified' || label === 'watchlist' || label === 'recent' || label === 'unwanted') {
            const value = whole(right);

            if (value == null) {
                return { error: { kind: 'line', line: lineNo } };
            }

            if (label === 'qualified' || label === 'watchlist') {
                if (value < 0) {
                    return { error: { kind: 'line', line: lineNo } };
                }

                if (label === 'qualified') {
                    qualified = value;
                } else {
                    watchlist = value;
                }
            } else if (label === 'recent' || label === 'unwanted') {
                const magnitude = Math.abs(value);

                if (magnitude > MAX_POINTS) {
                    return { error: { kind: 'points', line: lineNo } };
                }

                if (label === 'recent') {
                    if (value < 0) {
                        return { error: { kind: 'points', line: lineNo } };
                    }

                    recent = value;
                }
            }

            continue;
        }

        const bar = right.indexOf('|');
        const pointsText = (bar < 0 ? right : right.slice(0, bar)).trim();
        const points = whole(pointsText);

        if (points == null || points < 0 || points > MAX_POINTS) {
            return { error: { kind: 'points', line: lineNo } };
        }

        let key = '';
        let name = left;
        let description = '';

        if (bar >= 0) {
            const rest = right.slice(bar + 1);
            const second = rest.indexOf('|');

            if (second >= 0) {
                key = left;
                name = rest.slice(0, second).trim();
                description = rest.slice(second + 1).trim();

                if (!name) {
                    return { error: { kind: 'name' } };
                }
            } else {
                description = rest.trim();
            }
        }

        dimensions.push({ description, key, name, points });
    }

    const named = assignKeys(dimensions);

    if (!named.length) {
        return { error: { kind: 'no_dimension' } };
    }

    if (named.some((dimension) => !dimension.name)) {
        return { error: { kind: 'name' } };
    }

    if (watchlist < 1 || qualified <= watchlist) {
        return { error: { kind: 'watchlist' } };
    }

    const total = named.reduce((sum, dimension) => sum + dimension.points, recent);

    if (total < qualified) {
        return { error: { kind: 'unreachable' } };
    }

    return {
        scoring: {
            dimensions: named.map(({ key, name }) => ({ key, name })),
            qualified,
            recent,
            watchlist,
        },
    };
}

export function signalNames(scoring: string): Map<string, string> {
    const parsed = parseScoring(scoring);

    if ('error' in parsed) {
        return new Map();
    }

    return new Map(parsed.scoring.dimensions.map((dimension) => [dimension.key, dimension.name]));
}

function assignKeys(dimensions: DraftDimension[]): Array<DraftDimension & { points: number }> {
    const seen = new Set<string>();
    const out: DraftDimension[] = [];

    for (const dimension of dimensions) {
        const name = dimension.name.trim();
        const description = dimension.description.trim();

        if (!name && !description) {
            continue;
        }

        let key = slug(dimension.key || name);

        if (!key || RESERVED.has(key) || seen.has(key)) {
            let n = out.length + 1;

            while (seen.has(`d${n}`)) {
                n += 1;
            }

            key = `d${n}`;
        }

        seen.add(key);
        const builtIn = !description ? BUILT_IN[key] : undefined;

        out.push({
            description,
            key,
            name: builtIn ?? name,
            points: dimension.points,
        });
    }

    return out;
}
