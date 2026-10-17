import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { copy } from '@/lib/copy';

import { GROWTH_PLATFORMS, type GrowthPlatform } from './growth-types';

const ALL = 'all';

export function PlatformFilter({
    onChange,
    value,
}: {
    onChange: (next: GrowthPlatform | null) => void;
    value: GrowthPlatform | null;
}) {
    return (
        <Select
            onValueChange={(next) => onChange(next === ALL ? null : (next as GrowthPlatform))}
            value={value ?? ALL}
        >
            <SelectTrigger
                aria-label={copy.common.platform}
                className="w-32"
            >
                <SelectValue />
            </SelectTrigger>
            <SelectContent>
                <SelectItem value={ALL}>{copy.common.allPlatforms}</SelectItem>
                {GROWTH_PLATFORMS.map((platform) => (
                    <SelectItem
                        key={platform}
                        value={platform}
                    >
                        {copy.telegram.health.platform[platform] ?? platform}
                    </SelectItem>
                ))}
            </SelectContent>
        </Select>
    );
}
