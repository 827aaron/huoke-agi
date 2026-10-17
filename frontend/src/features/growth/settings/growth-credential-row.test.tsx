import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import type { GrowthSetting } from '../growth-types';

import { growthApi } from '../growth-api';
import { GrowthCredentialCard } from './growth-credential-row';

vi.mock('../growth-api', () => ({
    growthApi: {
        listModels: vi.fn(async () => [{ id: '~typesafe/jev-latest' }, { id: 'typesafe/jev-2026-09' }]),
        saveSetting: vi.fn(async () => savedProxy),
        testEmail: vi.fn(async () => ({ sent: true, to: ['ops@example.com'] })),
        testSetting: vi.fn(async () => savedProxy),
    },
}));

const savedProxy: GrowthSetting = {
    fields: { url: 'http://••••@203.0.113.24:8080' },
    kind: 'proxy',
    source: 'database',
    status: 'connected',
    tested_at: '2026-10-16T00:00:00Z',
    updated_at: '2026-10-16T00:00:00Z',
};

const setting = (overrides: Partial<GrowthSetting>): GrowthSetting => ({
    ...savedProxy,
    fields: {},
    provider: null,
    source: 'none',
    status: 'untested',
    tested_at: null,
    updated_at: null,
    ...overrides,
});

const renderCard = (value: GrowthSetting) =>
    render(
        <MemoryRouter>
            <GrowthCredentialCard
                kind={value.kind}
                onChange={() => {}}
                setting={value}
            />
        </MemoryRouter>,
    );

describe('GrowthCredentialCard', () => {
    beforeEach(() => vi.clearAllMocks());

    it('starts a masked proxy the server sent without providers empty and keeps it by not resending the mask', async () => {
        render(
            <MemoryRouter>
                <GrowthCredentialCard
                    kind="proxy"
                    onChange={() => {}}
                    setting={savedProxy}
                />
            </MemoryRouter>,
        );

        fireEvent.click(screen.getByRole('button', { name: '更换密钥' }));

        expect(screen.getByLabelText(/代理地址/)).toHaveValue('');
        expect(screen.getByLabelText(/代理地址/)).toHaveAttribute('placeholder', '留空则保留已保存的值');

        fireEvent.click(screen.getByRole('button', { name: /保存/ }));

        await waitFor(() =>
            expect(growthApi.saveSetting).toHaveBeenCalledWith('proxy', { fields: {}, provider: null }),
        );
    });

    it('sends an empty host with the addresses when the platform sends the email', async () => {
        renderCard(
            setting({
                fields: { from: '', host: '', notify_emails: '', password: '', port: '', tls: '', username: '' },
                kind: 'email',
                platform_smtp: true,
            }),
        );

        expect(screen.getByText('由平台代发，无需配置发信服务器，填好接收邮箱即可。')).toBeInTheDocument();

        fireEvent.change(screen.getByLabelText('接收名单的邮箱'), { target: { value: 'ops@example.com' } });
        fireEvent.click(screen.getByRole('button', { name: /保存/ }));

        await waitFor(() =>
            expect(growthApi.saveSetting).toHaveBeenCalledWith('email', {
                fields: { host: '', notify_emails: 'ops@example.com' },
                provider: null,
            }),
        );
    });

    it('asks for the test email in the language of the page', async () => {
        renderCard(
            setting({
                fields: { notify_emails: 'ops@example.com' },
                kind: 'email',
                platform_smtp: true,
                source: 'database',
                status: 'connected',
            }),
        );

        fireEvent.click(screen.getByRole('button', { name: '发送测试邮件' }));

        await waitFor(() => expect(growthApi.testEmail).toHaveBeenCalledWith('Chinese'));
    });

    it('asks a relay for its models with the saved key and offers them to pick', async () => {
        renderCard(
            setting({
                fields: { api_key: '••••7c1e', api_url: '', model: '' },
                kind: 'screening',
                provider: 'openrouter',
                providers: ['typesafe', 'openrouter', 'custom'],
                source: 'database',
                status: 'connected',
            }),
        );

        fireEvent.click(screen.getByRole('button', { name: '更换密钥' }));
        fireEvent.click(screen.getByRole('button', { name: '获取模型' }));

        await waitFor(() =>
            expect(growthApi.listModels).toHaveBeenCalledWith('screening', {
                api_key: '',
                api_url: '',
                provider: 'openrouter',
            }),
        );
        expect(await screen.findByRole('combobox', { name: '模型' })).toHaveTextContent('选择模型');
        expect(screen.getByLabelText('OpenRouter 密钥')).toHaveValue('');
    });

    it('lists OpenRouter embedding models with the key the LLM card lends', async () => {
        renderCard(
            setting({ kind: 'embedding', provider: 'openrouter', providers: ['openai', 'openrouter', 'ollama'] }),
        );

        expect(screen.getByLabelText('OpenRouter 密钥')).toHaveAttribute(
            'placeholder',
            '留空则复用大模型的 OpenRouter 密钥',
        );

        fireEvent.click(screen.getByRole('button', { name: '获取模型' }));

        await waitFor(() =>
            expect(growthApi.listModels).toHaveBeenCalledWith('embedding', {
                api_key: '',
                provider: 'openrouter',
                server_url: '',
            }),
        );
    });

    it('saves the newest Jev alias in place of a pinned model', async () => {
        renderCard(
            setting({
                fields: { api_key: '••••7c1e', api_url: '', model: 'typesafe/jev-1.13' },
                kind: 'screening',
                provider: 'openrouter',
                providers: ['typesafe', 'openrouter', 'custom'],
                source: 'database',
                status: 'connected',
            }),
        );

        fireEvent.click(screen.getByRole('button', { name: '更换密钥' }));
        fireEvent.click(screen.getByRole('button', { name: '改回最新' }));

        expect(screen.getByLabelText('模型')).toHaveValue('~typesafe/jev-latest');

        fireEvent.click(screen.getByRole('button', { name: /保存/ }));

        await waitFor(() =>
            expect(growthApi.saveSetting).toHaveBeenCalledWith('screening', {
                fields: expect.objectContaining({ model: '~typesafe/jev-latest' }),
                provider: 'openrouter',
            }),
        );
    });

    it('sends neither a model nor a threshold to a server whose Jev setting still reports one', async () => {
        renderCard(setting({ fields: { api_key: '', threshold: '0.6' }, kind: 'screening' }));

        expect(screen.getByText('未配置判断模型 Jev，任务无法筛选和打分。')).toHaveClass('text-destructive');
        expect(screen.queryByRole('button', { name: '获取模型' })).not.toBeInTheDocument();

        fireEvent.change(screen.getByLabelText('TypeSafe 密钥'), { target: { value: 'ts-0123456789' } });
        fireEvent.click(screen.getByRole('button', { name: /保存/ }));

        await waitFor(() =>
            expect(growthApi.saveSetting).toHaveBeenCalledWith('screening', {
                fields: { api_key: 'ts-0123456789' },
                provider: null,
            }),
        );
    });

    it('refuses a join interval that is not a number or a range', async () => {
        renderCard(
            setting({
                fields: { api_hash: '', api_id: '', join_interval_minutes: '3-10' },
                kind: 'telegram',
            }),
        );

        fireEvent.change(screen.getByLabelText('api_id'), { target: { value: '1234567' } });
        fireEvent.change(screen.getByLabelText('api_hash'), { target: { value: 'a'.repeat(32) } });
        fireEvent.click(screen.getByText('高级：加群节奏（建议值）'));
        fireEvent.change(screen.getByLabelText('加群间隔（分钟）'), { target: { value: 'ten' } });
        fireEvent.click(screen.getByRole('button', { name: /保存/ }));

        expect(await screen.findByText('请填写一个数字或范围，例如 3-10')).toBeInTheDocument();
        expect(growthApi.saveSetting).not.toHaveBeenCalled();
    });
});
