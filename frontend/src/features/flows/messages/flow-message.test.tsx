import { render, screen } from '@testing-library/react';
import { describe, expect, it } from 'vitest';

import type { MessageLogFragmentFragment } from '@/graphql/types';

import { TooltipProvider } from '@/components/ui/tooltip';
import { AgentType, MessageLogType, ResultFormat } from '@/graphql/types';

import FlowMessage from './flow-message';

const messageLog = (overrides: Partial<MessageLogFragmentFragment> = {}): MessageLogFragmentFragment => ({
    agent: null,
    createdAt: '2026-09-22T10:00:00Z',
    flowId: '7',
    id: '41',
    message: '已加入 3 个群',
    result: '',
    resultFormat: ResultFormat.Plain,
    subtaskId: null,
    taskId: null,
    thinking: null,
    type: MessageLogType.Answer,
    ...overrides,
});

const renderMessage = (log: MessageLogFragmentFragment) =>
    render(
        <TooltipProvider delayDuration={0}>
            <FlowMessage log={log} />
        </TooltipProvider>,
    );

describe('FlowMessage', () => {
    it('names the agent that wrote the message', () => {
        renderMessage(messageLog({ agent: AgentType.Pentester }));

        expect(screen.getByText('潜客研究员')).toBeInTheDocument();
    });

    it('shows no agent label for a message without an agent', () => {
        const { container } = renderMessage(messageLog());

        expect(container.querySelector('[data-slot="flow-message-agent"]')).toBeNull();
        expect(screen.queryByText('潜客研究员')).not.toBeInTheDocument();
    });
});
