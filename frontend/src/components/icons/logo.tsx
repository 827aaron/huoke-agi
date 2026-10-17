import { cn } from '@/lib/utils';

interface LogoProps {
    className?: string;
    // `dark` forces the variant drawn for dark backgrounds; `auto` follows the theme.
    tone?: 'auto' | 'dark';
    variant?: 'horizontal' | 'icon';
}

function Logo({ className, tone = 'auto', variant = 'icon' }: LogoProps) {
    const alt = 'KeLai AGI';
    const dark = `/logo/${variant}-dark.svg`;
    const light = `/logo/${variant}-light.svg`;

    if (tone === 'dark') {
        return (
            <img
                alt={alt}
                className={cn('object-contain', className)}
                src={dark}
            />
        );
    }

    return (
        <>
            <img
                alt={alt}
                className={cn('object-contain dark:hidden', className)}
                src={light}
            />
            <img
                alt={alt}
                className={cn('hidden object-contain dark:block', className)}
                src={dark}
            />
        </>
    );
}

export default Logo;
