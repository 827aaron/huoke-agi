import type { ReactNode } from 'react';

import {
    ArrowLeft,
    Briefcase,
    FileText,
    Key,
    KeyRound,
    Plug,
    Send,
    Settings as SettingsIcon,
    User,
} from 'lucide-react';
import { useState } from 'react';
import { NavLink, useLocation } from 'react-router-dom';

import { VersionPanel } from '@/components/shared/version-panel';
import {
    Sidebar,
    SidebarContent,
    SidebarFooter,
    SidebarGroup,
    SidebarGroupContent,
    SidebarHeader,
    SidebarMenu,
    SidebarMenuButton,
    SidebarMenuItem,
} from '@/components/ui/sidebar';
import { copy } from '@/lib/copy';
import { routes } from '@/lib/routes';
import { getSafeReturnUrl } from '@/lib/utils/auth';

interface MenuItem {
    icon?: ReactNode;
    id: string;
    path: string;
    title: string;
}

interface SettingsSidebarMenuItemProps {
    item: MenuItem;
}

const menuItems: readonly MenuItem[] = [
    {
        icon: <User className="size-4" />,
        id: 'account',
        path: routes.settings.account,
        title: copy.settingsNav.account,
    },
    {
        icon: <KeyRound className="size-4" />,
        id: 'credentials',
        path: routes.settings.credentials,
        title: copy.settingsNav.credentials,
    },
    {
        icon: <Briefcase className="size-4" />,
        id: 'business-profile',
        path: routes.settings.growthProfile,
        title: copy.settingsNav.growthProfile,
    },
    {
        icon: <Send className="size-4" />,
        id: 'telegram',
        path: routes.settings.telegram,
        title: copy.settingsNav.telegram,
    },
    {
        icon: <Plug className="size-4" />,
        id: 'providers',
        path: routes.settings.providers,
        title: copy.settingsNav.providers,
    },
    {
        icon: <FileText className="size-4" />,
        id: 'prompts',
        path: routes.settings.prompts,
        title: copy.settingsNav.prompts,
    },
    {
        icon: <Key className="size-4" />,
        id: 'api-tokens',
        path: routes.settings.apiTokens,
        title: copy.settingsNav.apiTokens,
    },
] as const;

export function SettingsSidebar() {
    const location = useLocation();
    const [returnUrl] = useState(() =>
        getSafeReturnUrl((location.state as null | { from?: string })?.from ?? null, routes.flows),
    );

    return (
        <Sidebar collapsible="icon">
            <SidebarHeader>
                <SidebarMenu>
                    <SidebarMenuItem>
                        <VersionPanel
                            icon={
                                <SettingsIcon
                                    className="size-8"
                                    strokeWidth={4 / 3}
                                />
                            }
                            title={copy.settingsNav.settings}
                        />
                    </SidebarMenuItem>
                </SidebarMenu>
            </SidebarHeader>
            <SidebarContent>
                <SidebarGroup>
                    <SidebarGroupContent>
                        <SidebarMenu>
                            {menuItems.map((item) => (
                                <SettingsSidebarMenuItem
                                    item={item}
                                    key={item.id}
                                />
                            ))}
                        </SidebarMenu>
                    </SidebarGroupContent>
                </SidebarGroup>
            </SidebarContent>
            <SidebarFooter>
                <SidebarMenuButton asChild>
                    <NavLink to={returnUrl}>
                        <ArrowLeft />
                        Back to App
                    </NavLink>
                </SidebarMenuButton>
            </SidebarFooter>
        </Sidebar>
    );
}

function SettingsSidebarMenuItem({ item }: SettingsSidebarMenuItemProps) {
    const location = useLocation();
    const isActive = location.pathname.startsWith(item.path);

    return (
        <SidebarMenuItem>
            <SidebarMenuButton
                asChild
                isActive={isActive}
            >
                <NavLink to={item.path}>
                    {item.icon}
                    {item.title}
                </NavLink>
            </SidebarMenuButton>
        </SidebarMenuItem>
    );
}
