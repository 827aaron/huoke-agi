import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { VersionInfoFragmentFragment } from '@/graphql/types';

import { UpdateState } from '@/graphql/types';

const provider = vi.hoisted(() => ({
    value: { isLoading: false, versionInfo: null as null | VersionInfoFragmentFragment },
}));

vi.mock('@/providers/version-info-provider', () => ({ useVersionInfo: () => provider.value }));

import { SidebarProvider } from '@/components/ui/sidebar';

import {
    describeTiming,
    describeVersion,
    formatVersionLabel,
    PENTAGI_RELEASES_URL,
    releaseNotesUrl,
    splitVersion,
    UPDATE_INSTRUCTION,
    VersionPanel,
} from './version-panel';

function info(overrides: Partial<VersionInfoFragmentFragment> = {}): VersionInfoFragmentFragment {
    return {
        build: 'b1d7c0de',
        checkedAt: null,
        current: '2.1.0',
        failedAt: null,
        latest: null,
        state: UpdateState.UpToDate,
        strategy: 'preview',
        ...overrides,
    };
}

const trigger = () => screen.getByRole('button', { name: /客来 AGI v2\.1\.0/ });

function renderPanel({ isOpen = true, title = '客来 AGI' } = {}) {
    return render(
        <SidebarProvider defaultOpen={isOpen}>
            <VersionPanel
                icon={<span data-slot="probe-icon" />}
                title={title}
            />
        </SidebarProvider>,
    );
}

describe('VersionPanel', () => {
    beforeEach(() => {
        provider.value = { isLoading: false, versionInfo: null };
    });

    it('holds a place for the version while the server has not answered', () => {
        provider.value = { isLoading: true, versionInfo: null };
        renderPanel();

        expect(screen.getByText('客来 AGI')).toBeInTheDocument();
        expect(screen.getByTestId('probe-icon')).toBeInTheDocument();
        expect(screen.getByTestId('skeleton')).toBeInTheDocument();
        expect(screen.queryByRole('button')).not.toBeInTheDocument();
    });

    it('keeps the trigger, not a placeholder, while a poll is in flight over an answer it already has', () => {
        provider.value = { isLoading: true, versionInfo: info() };
        renderPanel();

        expect(trigger()).toBeInTheDocument();
        expect(screen.queryByTestId('skeleton')).not.toBeInTheDocument();
    });

    it('keeps the icon and the title, and no placeholder, when the query settles without an answer', () => {
        renderPanel();

        expect(screen.getByText('客来 AGI')).toBeInTheDocument();
        expect(screen.getByTestId('probe-icon')).toBeInTheDocument();
        expect(screen.queryByTestId('skeleton')).not.toBeInTheDocument();
        expect(screen.queryByRole('button')).not.toBeInTheDocument();
    });

    it('carries the icon and the title the caller passed, with the running version under them', () => {
        provider.value = { isLoading: false, versionInfo: info() };
        renderPanel({ title: 'Settings' });

        const button = screen.getByRole('button', { name: /Settings v2\.1\.0/ });

        expect(button).toHaveTextContent('Settings');
        expect(button).toHaveTextContent('v2.1.0');
        expect(screen.getByTestId('probe-icon')).toBeInTheDocument();
    });

    it('shows neither indicator nor attention dot when the build is current', () => {
        provider.value = { isLoading: false, versionInfo: info() };
        renderPanel();

        expect(document.querySelector('[data-slot^="version-indicator"]')).toBeNull();
        expect(screen.queryByTestId('version-attention-dot')).not.toBeInTheDocument();
    });

    it('shows the arrow and the attention dot when an update is offered', () => {
        provider.value = {
            isLoading: false,
            versionInfo: info({ latest: '2.4.0', state: UpdateState.UpdateAvailable }),
        };
        renderPanel();

        expect(screen.getByTestId('version-indicator-update')).toHaveClass('text-yellow-500');
        expect(screen.getByTestId('version-attention-dot')).toBeInTheDocument();
    });

    it.each([UpdateState.Unreachable, UpdateState.Unknown, UpdateState.Pending, UpdateState.Disabled])(
        'carries no indicator when the verdict is only %s',
        (state) => {
            provider.value = { isLoading: false, versionInfo: info({ state }) };
            renderPanel();

            expect(document.querySelector('[data-slot^="version-indicator"]')).toBeNull();
            expect(screen.queryByTestId('version-attention-dot')).not.toBeInTheDocument();
        },
    );

    it('explains itself on hover only while the sidebar is collapsed to icons', async () => {
        const user = userEvent.setup();
        provider.value = {
            isLoading: false,
            versionInfo: info({ latest: '2.4.0', state: UpdateState.UpdateAvailable }),
        };
        const { unmount } = renderPanel();

        await user.hover(trigger());

        expect(screen.queryByRole('tooltip')).not.toBeInTheDocument();

        unmount();
        renderPanel({ isOpen: false });
        await user.hover(trigger());

        expect(await screen.findByRole('tooltip')).toHaveTextContent('新版本 v2.4.0 可用，运行安装工具即可更新。');
    });

    it('opens the details with the build, the channel, the instruction and the release links', async () => {
        const user = userEvent.setup();
        provider.value = {
            isLoading: false,
            versionInfo: info({
                checkedAt: new Date(Date.now() - 10 * 60 * 1000).toISOString(),
                latest: '2.4.0',
                state: UpdateState.UpdateAvailable,
                strategy: 'stable',
            }),
        };
        renderPanel();

        await user.click(trigger());

        const dialog = await screen.findByRole('menu');

        expect(dialog).toHaveTextContent('客来 AGI v2.1.0');
        expect(dialog).toHaveTextContent('构建 b1d7c0de');
        expect(dialog).toHaveTextContent('stable');
        expect(dialog).toHaveTextContent('新版本 v2.4.0 可用');
        expect(dialog).toHaveTextContent('在部署客来 AGI 的服务器上运行安装工具');
        expect(dialog).toHaveTextContent('当前版本');
        expect(dialog).toHaveTextContent('可更新到');
        expect(dialog).toHaveTextContent('上次检查');
        expect(dialog).toHaveTextContent('10 分钟前');
        expect(screen.getByRole('menuitem', { name: /v2\.4\.0 更新说明/ })).toHaveAttribute(
            'href',
            'https://github.com/vxcontrol/pentagi/releases/tag/v2.4.0',
        );
        expect(screen.getByRole('menuitem', { name: /所有版本/ })).toHaveAttribute(
            'href',
            'https://github.com/vxcontrol/pentagi/releases',
        );
    });

    it('names the newest published version in prose without offering it as an update', async () => {
        const user = userEvent.setup();
        provider.value = {
            isLoading: false,
            versionInfo: info({ latest: '2.4.0', state: UpdateState.Unknown }),
        };
        renderPanel();

        await user.click(trigger());

        const dialog = await screen.findByRole('menu');

        expect(dialog).toHaveTextContent('更新状态未知');
        expect(dialog).toHaveTextContent('已发布的最新版本是 v2.4.0。');
        expect(dialog).not.toHaveTextContent('可更新到');
        expect(screen.queryByRole('menuitem', { name: /更新说明/ })).not.toBeInTheDocument();
    });

    it('says what the channel badge means, in its name and on hover', async () => {
        const user = userEvent.setup();
        provider.value = { isLoading: false, versionInfo: info({ strategy: 'stable' }) };
        renderPanel();

        await user.click(trigger());

        const badge = await screen.findByLabelText('更新渠道：stable');

        await user.hover(badge);

        expect(await screen.findByRole('tooltip')).toHaveTextContent('更新来自 stable 渠道');
    });

    it('names the base version of a build cut from a branch beside its build', async () => {
        const user = userEvent.setup();
        provider.value = {
            isLoading: false,
            versionInfo: info({ current: '2.1.0-ce.h93e99748' }),
        };
        renderPanel();

        await user.click(screen.getByRole('button', { name: /客来 AGI v2\.1\.0-ce/ }));

        expect(await screen.findByRole('menu')).toHaveTextContent('构建 b1d7c0de');
    });

    it('shows the build of an edition that carries no version number', async () => {
        const user = userEvent.setup();
        provider.value = { isLoading: false, versionInfo: info({ current: 'ce.h93e99748' }) };
        renderPanel();

        await user.click(screen.getByRole('button', { name: /客来 AGI ce/ }));

        expect(await screen.findByRole('menu')).toHaveTextContent('构建 b1d7c0de');
    });

    it('offers no release notes link when there is no release to point at', async () => {
        const user = userEvent.setup();
        provider.value = { isLoading: false, versionInfo: info({ state: UpdateState.Unreachable }) };
        renderPanel();

        await user.click(trigger());

        expect(await screen.findByRole('menu')).toHaveTextContent('更新状态未知');
        expect(screen.queryByRole('menuitem', { name: /更新说明/ })).not.toBeInTheDocument();
        expect(screen.getByRole('menuitem', { name: /所有版本/ })).toBeInTheDocument();
    });
});

describe('splitVersion', () => {
    it('separates a trailing git revision from a release number', () => {
        expect(splitVersion('2.1.0-93e99748')).toEqual({ revision: '93e99748', version: '2.1.0' });
    });

    it('leaves a prerelease suffix in the version, since it is not a revision', () => {
        expect(splitVersion('2.2.0-rc1')).toEqual({ revision: null, version: '2.2.0-rc1' });
    });

    it('leaves a bare version and a name that is not one alone', () => {
        expect(splitVersion('2.1.0')).toEqual({ revision: null, version: '2.1.0' });
        expect(splitVersion('ce')).toEqual({ revision: null, version: 'ce' });
    });

    // The hyphen form is not what PentAGI stamps; it is what a binary built by
    // hand with only -X PackageRev can carry, and the panel still splits it.
    it('separates a revision joined by a hyphen', () => {
        expect(splitVersion('2.1.0-93e99748')).toEqual({ revision: '93e99748', version: '2.1.0' });
    });

    it('splits the revision off an edition build and drops the h that keeps it semver', () => {
        expect(splitVersion('2.1.0-ce.h93e99748')).toEqual({ revision: '93e99748', version: '2.1.0-ce' });
        expect(splitVersion('2.1.0-ee.h93e99748')).toEqual({ revision: '93e99748', version: '2.1.0-ee' });
        expect(splitVersion('ce.h93e99748')).toEqual({ revision: '93e99748', version: 'ce' });
        expect(splitVersion('ee.h93e99748')).toEqual({ revision: '93e99748', version: 'ee' });
    });

    it('splits an all-digit revision, the case the h exists for', () => {
        expect(splitVersion('2.1.0-ce.h0123456')).toEqual({ revision: '0123456', version: '2.1.0-ce' });
    });

    it('leaves an edition build with no revision whole', () => {
        expect(splitVersion('2.1.0-ce')).toEqual({ revision: null, version: '2.1.0-ce' });
        expect(splitVersion('ce')).toEqual({ revision: null, version: 'ce' });
    });
});

describe('formatVersionLabel', () => {
    it('prefixes a numeric version with v and leaves a branch name alone', () => {
        expect(formatVersionLabel('2.1.0')).toBe('v2.1.0');
        expect(formatVersionLabel('2.1.0-ce')).toBe('v2.1.0-ce');
        expect(formatVersionLabel('ce')).toBe('ce');
    });
});

describe('releaseNotesUrl', () => {
    it('points at the tagged release, whether or not the version already carries the v', () => {
        expect(releaseNotesUrl('2.4.0')).toBe(`${PENTAGI_RELEASES_URL}/tag/v2.4.0`);
        expect(releaseNotesUrl('v2.4.0')).toBe(`${PENTAGI_RELEASES_URL}/tag/v2.4.0`);
    });

    it('builds no link from a value that does not name a release', () => {
        expect(releaseNotesUrl('latest')).toBeNull();
        expect(releaseNotesUrl('ce')).toBeNull();
        expect(releaseNotesUrl('ee')).toBeNull();
    });

    it('drops the edition, because the tag is the release and not the build', () => {
        expect(releaseNotesUrl('2.4.0-ce')).toBe(`${PENTAGI_RELEASES_URL}/tag/v2.4.0`);
        expect(releaseNotesUrl('2.4.0-ee')).toBe(`${PENTAGI_RELEASES_URL}/tag/v2.4.0`);
        expect(releaseNotesUrl('v2.4.0-ce')).toBe(`${PENTAGI_RELEASES_URL}/tag/v2.4.0`);
    });
});

describe('describeVersion', () => {
    it('shows the yellow arrow and tells the operator to run the installer when an update is offered', () => {
        const presentation = describeVersion(info({ latest: '2.4.0', state: UpdateState.UpdateAvailable }));

        expect(presentation.indicator).toBe('update');
        expect(presentation.label).toBe('v2.1.0');
        expect(presentation.latest).toBe('v2.4.0');
        expect(presentation.headline).toBe('新版本 v2.4.0 可用');
        expect(presentation.tooltip).toBe('新版本 v2.4.0 可用，运行安装工具即可更新。');
        expect(presentation.action).toBe(UPDATE_INSTRUCTION);
    });

    it('shows no indicator when the build is current', () => {
        const presentation = describeVersion(info());

        expect(presentation.indicator).toBe('none');
        expect(presentation.tooltip).toBe('客来 AGI v2.1.0 已是最新版本');
        expect(presentation.detail).toContain('preview 渠道');
        expect(presentation.action).toBeNull();
    });

    it.each([
        [UpdateState.Pending, '正在检查更新…'],
        [UpdateState.Unreachable, '更新状态未知：无法连接更新服务。'],
        [UpdateState.Unknown, '更新状态未知：更新服务无法判断是否有新版本。'],
        [UpdateState.Disabled, '服务器配置里关闭了更新检查。'],
    ])('shows the yellow question mark for %s', (state, tooltip) => {
        const presentation = describeVersion(info({ state }));

        expect(presentation.indicator).toBe('question');
        expect(presentation.tooltip).toBe(tooltip);
    });

    it('mentions the newest published version when the build could not be matched to one', () => {
        const presentation = describeVersion(info({ latest: '2.4.0', state: UpdateState.Unknown }));

        expect(presentation.indicator).toBe('question');
        expect(presentation.detail).toContain('已发布的最新版本是 v2.4.0。');
    });

    it('treats an update that names no version as unknown rather than pointing an arrow at nothing', () => {
        const presentation = describeVersion(info({ latest: null, state: UpdateState.UpdateAvailable }));

        expect(presentation.indicator).toBe('question');
        expect(presentation.action).toBeNull();
    });

    it('treats a state it has never heard of as unknown', () => {
        const presentation = describeVersion(info({ state: 'surprise' as UpdateState }));

        expect(presentation.indicator).toBe('question');
    });

    it('treats an update named by a tag rather than a version as unknown', () => {
        const presentation = describeVersion(info({ latest: 'latest', state: UpdateState.UpdateAvailable }));

        expect(presentation.indicator).toBe('question');
        expect(presentation.action).toBeNull();
        expect(presentation.latest).toBeNull();
    });

    it('names no newest version when the service answered with a tag', () => {
        const presentation = describeVersion(info({ latest: 'latest', state: UpdateState.Unknown }));

        expect(presentation.detail).not.toContain('newest published version');
    });
});

describe('describeTiming', () => {
    const now = new Date('2026-09-02T12:00:00Z');

    it('says nothing when nothing has happened yet', () => {
        expect(describeTiming(info(), now)).toEqual([]);
    });

    it('dates the answer and the failure separately', () => {
        expect(
            describeTiming(info({ checkedAt: '2026-09-02T09:00:00Z', failedAt: '2026-09-02T11:55:00Z' }), now),
        ).toEqual([
            { label: '上次检查', value: '大约 3 小时前' },
            { label: '上次检查失败', value: '5 分钟前' },
        ]);
    });
});
