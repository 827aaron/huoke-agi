/// <reference types="vite/client" />

interface ImportMeta {
    readonly env: ImportMetaEnv;
}

interface ImportMetaEnv {
    readonly VITE_APP_API_ROOT: string;
    readonly VITE_APP_LOG_LEVEL: 'DEBUG' | 'ERROR' | 'INFO' | 'WARN';
    readonly VITE_APP_SESSION_KEY: string;
    // Unset: the growth panels use in-memory mock data in `vite dev` and the real API in a build.
    readonly VITE_GROWTH_API?: 'live' | 'mock';
}
