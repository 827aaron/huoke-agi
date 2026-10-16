import { Outlet } from 'react-router-dom';

import { MainSidebar } from '@/components/layouts/main/main-sidebar';
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar';
import GrowthGlobalAlerts from '@/features/growth/growth-global-alerts';

function MainLayout() {
    return (
        <SidebarProvider>
            <MainSidebar />
            <SidebarInset>
                <GrowthGlobalAlerts />
                <Outlet />
            </SidebarInset>
        </SidebarProvider>
    );
}

export default MainLayout;
