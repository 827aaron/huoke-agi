import { copy } from '@/lib/copy';

function PageLoader() {
    return (
        <div className="grid h-screen w-full place-items-center">
            <p>{copy.action.loading}</p>
        </div>
    );
}

export default PageLoader;
