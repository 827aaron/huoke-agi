import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { createMemoryRouter, RouterProvider } from 'react-router-dom';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import RouteErrorBoundary from './route-error-boundary';

const renderWithRouteError = (error: unknown) => {
    const router = createMemoryRouter(
        [
            {
                element: <div>page</div>,
                errorElement: <RouteErrorBoundary />,
                loader: () => {
                    throw error;
                },
                path: '/',
            },
        ],
        { initialEntries: ['/'] },
    );

    return render(<RouterProvider router={router} />);
};

const chunkError = () =>
    new Error('Failed to fetch dynamically imported module: https://app/assets/templates-D01Ouz7C.js');
const renderError = () => new TypeError('Cannot read properties of undefined (reading length)');
const domDesyncError = () =>
    new DOMException(
        "Failed to execute 'removeChild' on 'Node': The node to be removed is not a child of this node.",
        'NotFoundError',
    );

describe('RouteErrorBoundary', () => {
    const originalLocation = window.location;
    let reloadSpy: ReturnType<typeof vi.fn>;

    beforeEach(() => {
        sessionStorage.clear();
        reloadSpy = vi.fn();
        // jsdom's `location.reload` is non-configurable, so swap the whole
        // `location` object — the same workaround chunk-reload.test.ts uses.
        Object.defineProperty(window, 'location', {
            configurable: true,
            value: { ...originalLocation, reload: reloadSpy },
            writable: true,
        });
        // react-router logs loader errors it routes to the boundary; keep the
        // test output clean without hiding real assertion failures.
        vi.spyOn(console, 'error').mockImplementation(() => {});
    });

    afterEach(() => {
        vi.restoreAllMocks();
        Object.defineProperty(window, 'location', {
            configurable: true,
            value: originalLocation,
            writable: true,
        });
        sessionStorage.clear();
    });

    it('shows the redeploy message and auto-reloads once for a chunk-load error', async () => {
        renderWithRouteError(chunkError());

        expect(await screen.findByText('系统刚刚更新了版本，重新加载即可使用最新版。')).toBeInTheDocument();
        expect(screen.getByRole('alert')).toBeInTheDocument();
        await waitFor(() => expect(reloadSpy).toHaveBeenCalledTimes(1));
    });

    it('shows the glitch message and auto-reloads once for a DOM-desync (removeChild) error', async () => {
        renderWithRouteError(domDesyncError());

        expect(await screen.findByText('页面显示出了点小问题，重新加载通常就能恢复。')).toBeInTheDocument();
        await waitFor(() => expect(reloadSpy).toHaveBeenCalledTimes(1));
    });

    it('shows the generic message and does not auto-reload for a non-chunk error', async () => {
        renderWithRouteError(renderError());

        expect(await screen.findByText('页面遇到了意外错误，重新加载通常就能恢复。')).toBeInTheDocument();
        expect(reloadSpy).not.toHaveBeenCalled();
    });

    it('waits for the connection instead of reloading a chunk that failed offline', async () => {
        let isOnline = false;
        vi.spyOn(navigator, 'onLine', 'get').mockImplementation(() => isOnline);

        renderWithRouteError(chunkError());

        expect(await screen.findByText(/网络已断开/)).toBeInTheDocument();
        expect(reloadSpy).not.toHaveBeenCalled();

        isOnline = true;
        act(() => {
            window.dispatchEvent(new Event('online'));
        });

        await waitFor(() => expect(reloadSpy).toHaveBeenCalledTimes(1));
    });

    it('offers no reload while the connection the chunk needs is still down', async () => {
        let isOnline = false;
        vi.spyOn(navigator, 'onLine', 'get').mockImplementation(() => isOnline);

        renderWithRouteError(chunkError());

        const reload = await screen.findByRole('button', { name: /重新加载/ });
        expect(reload).toBeDisabled();
        await userEvent.click(reload);
        expect(reloadSpy).not.toHaveBeenCalled();

        isOnline = true;
        act(() => {
            window.dispatchEvent(new Event('online'));
        });

        await waitFor(() => expect(reload).toBeEnabled());
    });

    it('reloads when the user clicks Reload', async () => {
        renderWithRouteError(renderError());

        await userEvent.click(await screen.findByRole('button', { name: /重新加载/ }));

        expect(reloadSpy).toHaveBeenCalledTimes(1);
    });

    it('shows what was thrown behind the details toggle', async () => {
        renderWithRouteError(renderError());

        await userEvent.click(await screen.findByText('错误详情（截图发给我们便于定位）'));

        expect(screen.getByText(/TypeError: Cannot read properties of undefined \(reading length\)/)).toBeVisible();
    });
});
