import { Outlet } from 'react-router-dom';

import { SettingsSidebar } from '@/components/layouts/settings/settings-sidebar';
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar';
import GrowthGlobalAlerts from '@/features/growth/growth-global-alerts';

function SettingsLayout() {
    return (
        <SidebarProvider>
            <SettingsSidebar />
            <SidebarInset>
                <GrowthGlobalAlerts />
                <Outlet />
            </SidebarInset>
        </SidebarProvider>
    );
}

export default SettingsLayout;
