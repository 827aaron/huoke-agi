import { Label } from '@/components/ui/label';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { copy } from '@/lib/copy';

import { useGrowthProfiles } from '../use-growth-data';

interface GrowthProfileSelectProps {
    disabled?: boolean;
    id?: string;
    onChange: (profileId: string) => void;
    // Empty shows the default profile.
    value: string;
}

// Hidden while the owner has no saved profile: there is nothing to choose and the built-in one applies.
export function GrowthProfileSelect({
    disabled,
    id = 'growth-profile-select',
    onChange,
    value,
}: GrowthProfileSelectProps) {
    const { data: profiles } = useGrowthProfiles();

    if (!profiles?.length) {
        return null;
    }

    const selected = value || String(profiles.find((profile) => profile.default)?.id ?? profiles[0]?.id ?? '');

    return (
        <div className="flex items-center gap-2">
            <Label
                className="shrink-0 text-sm"
                htmlFor={id}
            >
                {copy.profile.list.pick}
            </Label>
            <Select
                disabled={disabled}
                onValueChange={onChange}
                value={selected}
            >
                <SelectTrigger
                    className="w-full"
                    id={id}
                >
                    <SelectValue />
                </SelectTrigger>
                <SelectContent>
                    {profiles.map((profile) => (
                        <SelectItem
                            key={profile.id}
                            value={String(profile.id)}
                        >
                            {profile.title || profile.name}
                            {profile.default ? `（${copy.profile.list.default}）` : ''}
                        </SelectItem>
                    ))}
                </SelectContent>
            </Select>
        </div>
    );
}
