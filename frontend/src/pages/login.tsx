import { useLocation, useSearchParams } from 'react-router-dom';

import Logo from '@/components/icons/logo';
import { Spinner } from '@/components/ui/spinner';
import LoginForm from '@/features/authentication/login-form';
import { routes } from '@/lib/routes';
import { getSafeReturnUrl } from '@/lib/utils/auth';
import { useUser } from '@/providers/user-provider';

function Login() {
    const [searchParams] = useSearchParams();
    const location = useLocation();
    const { authInfo, isLoading } = useUser();
    const authProviders = authInfo?.providers || [];

    const returnUrl = getSafeReturnUrl(
        (location.state?.from as string) || searchParams.get('returnUrl'),
        routes.newFlow,
    );

    return (
        <div className="flex h-dvh w-full items-center justify-center">
            <div className="h-dvh w-full lg:grid lg:grid-cols-2">
                <div className="flex items-center justify-center px-4 py-12">
                    {!isLoading ? (
                        <LoginForm
                            providers={authProviders}
                            returnUrl={returnUrl}
                        />
                    ) : (
                        <Spinner
                            className="size-16"
                            variant="circle"
                        />
                    )}
                </div>
                <div className="from-primary/20 via-primary/10 to-background hidden bg-linear-to-br lg:flex">
                    <div className="m-auto">
                        <Logo
                            className="w-80"
                            variant="horizontal"
                        />
                    </div>
                </div>
            </div>
        </div>
    );
}

export default Login;
