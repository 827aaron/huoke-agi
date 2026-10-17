import type { ReactNode } from 'react';

import { skipToken, useQuery } from '@apollo/client/react';
import { ChevronDown, Ellipsis, FileSymlink, FileText, LayoutTemplate, Pencil, Save, Trash } from 'lucide-react';
import { useCallback, useMemo, useState } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { toast } from 'sonner';
import { z } from 'zod';

import { AppHeader, AppHeaderAction, AppHeaderActions, AppHeaderContent } from '@/components/layouts/app/app-header';
import ConfirmationDialog from '@/components/shared/confirmation-dialog';
import {
    DetailNavigationButtons,
    DetailNavigationSheet,
    DetailNavigationToolbar,
} from '@/components/shared/detail-navigation';
import { DetailSplitLayout } from '@/components/shared/detail-split-layout';
import { ErrorState } from '@/components/shared/error-state';
import { InlineEditInput, useInlineEdit } from '@/components/shared/inline-edit';
import { type EditorViewMode, EditorViewModeToggle, MarkdownEditorField } from '@/components/shared/markdown-editor';
import { UnsavedChangesDialog, useUnsavedChangesGuard } from '@/components/shared/unsaved-changes';
import { Badge } from '@/components/ui/badge';
import { Breadcrumb, BreadcrumbItem, BreadcrumbList, BreadcrumbPage } from '@/components/ui/breadcrumb';
import { Button } from '@/components/ui/button';
import { Card, CardContent } from '@/components/ui/card';
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from '@/components/ui/collapsible';
import {
    DropdownMenu,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuSeparator,
    DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu';
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover';
import { Spinner } from '@/components/ui/spinner';
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip';
import { useTemplateDetailNavigation } from '@/features/templates/use-template-detail-navigation';
import { FlowTemplateDocument } from '@/graphql/types';
import { useAppForm } from '@/hooks/use-app-form';
import { useBreakpoint } from '@/hooks/use-breakpoint';
import { copy } from '@/lib/copy';
import { isNotFoundError } from '@/lib/errors';
import { routes } from '@/lib/routes';
import { cn } from '@/lib/utils';
import { type Template, useTemplates } from '@/providers/templates-provider';

// Mirrors the limits in backend/pkg/graph/context.go.
const MAX_TEMPLATE_TITLE_LENGTH = 255;
const MAX_TEMPLATE_TEXT_LENGTH = 65536;

export const formSchema = z.object({
    // The text is a document: it is sent as it was written, the line break it ends with included, and the
    // limit counts all of it, as the mutation does.
    text: z
        .string()
        .refine((value) => value.trim() !== '', { message: copy.templatePage.textRequired })
        .refine((value) => [...value].length <= MAX_TEMPLATE_TEXT_LENGTH, {
            message: copy.templatePage.textTooLong(MAX_TEMPLATE_TEXT_LENGTH),
        }),
    title: z
        .string()
        .trim()
        .min(1, { message: copy.templatePage.titleRequired })
        .refine((value) => [...value].length <= MAX_TEMPLATE_TITLE_LENGTH, {
            message: copy.templatePage.titleTooLong(MAX_TEMPLATE_TITLE_LENGTH),
        }),
});

type FormValues = z.infer<typeof formSchema>;

const PRESETS_TITLE = copy.templates.presetsTitle;

export const PRESET_TEMPLATES: { text: string; title: string }[] = [
    {
        text: `在 Telegram 上为我寻找 {{目标数量}} 个潜在玩家客户。

工作步骤：
1. 来源发现：结合业务档案里的目标语言、目标地区和信号词，搜索相关的公开群组、频道和话题，整理成来源池（只收集公开可见的信息）。
2. 加入来源：在授权账号下加入最有价值的公开群组，按平台节奏慢速操作，遇到验证就停下并标记需要人工登录。
3. 识别意向：在群里浏览公开发言，找出正在玩同类游戏、表达相关需求或付费意愿的人。
4. 线索打分：按玩家身份、需求痛点、付费意愿、近期是否活跃、与业务档案的匹配度打分；排除卖号、代练、竞品和机器人。
5. 入库去重：把合格线索（评分达标）写入线索库，和历史线索自动去重。
6. 交付：整理一份带证据（发言截图/链接、匹配理由、评分）的潜客清单。`,
        title: 'Telegram 群组找玩家',
    },
    {
        text: `在 Facebook 上为我寻找 {{目标数量}} 个潜在玩家客户。

工作步骤：
1. 来源发现：根据业务档案的目标语言、地区和信号词，找到相关的公开小组和公开主页，整理成来源池。
2. 加入来源：在授权账号下加入值得关注的公开小组，慢速、单账号操作，遇到验证即停并标记需要人工登录。
3. 识别意向：浏览小组帖子和公开主页下的评论，找出讨论同类游戏、表达需求或付费意愿的人。
4. 线索打分：按玩家身份、需求痛点、付费意愿、近期活跃度、与业务档案的匹配度打分，淘汰卖号、代练、竞品和机器人。
5. 入库去重：合格线索写入线索库并与历史记录去重。
6. 交付：输出带证据和评分的潜客清单。`,
        title: 'Facebook 小组与主页评论找玩家',
    },
    {
        text: `在 Instagram 上为我寻找 {{目标数量}} 个潜在玩家客户。

工作步骤：
1. 来源发现：围绕业务档案里的目标游戏、信号词和目标地区，锁定相关的话题标签和公开账号，整理成来源池。
2. 浏览内容：在授权账号下查看相关话题下的公开帖子和评论（只看公开内容）。
3. 识别意向：找出在评论或帖子中表达对同类游戏兴趣、需求或付费意愿的人。
4. 线索打分：按玩家身份、痛点、付费意愿、活跃度和业务档案匹配度打分，排除卖号、代练、竞品和机器人。
5. 入库去重：合格线索写入线索库并去重。
6. 交付：输出带证据和评分的潜客清单。`,
        title: 'Instagram 话题找玩家',
    },
    {
        text: `在 YouTube 上为我寻找 {{目标数量}} 个有意向的潜在客户。

工作步骤：
1. 来源发现：根据业务档案的目标游戏和信号词，搜索相关的视频和频道，整理成来源池。
2. 浏览评论：查看这些视频评论区里的公开留言。
3. 识别意向：找出在评论中表达想玩、想换游戏、咨询或付费意愿的观众。
4. 线索打分：按玩家身份、痛点、付费意愿、近期活跃度、与业务档案的匹配度打分，淘汰卖号、代练、竞品和机器人。
5. 入库去重：合格线索写入线索库并去重。
6. 交付：输出带证据（评论链接、匹配理由、评分）的潜客清单。`,
        title: 'YouTube 评论区找意向用户',
    },
    {
        text: `在公开论坛上为我寻找 {{目标数量}} 个有付费意愿的潜在玩家。

工作步骤：
1. 来源发现：结合业务档案的目标语言、地区和信号词，用联网搜索找到相关的公开游戏论坛、贴吧和社区板块，整理成来源池。
2. 浏览内容：抓取相关版块的公开帖子和回复（只看公开信息）。
3. 识别意向：重点找讨论充值、氪金、购买道具、换区换服等话题，表现出明确付费意愿的用户。
4. 线索打分：在玩家、痛点、活跃度的基础上，对表达付费意愿的人加权；排除卖号、代练、竞品和机器人。
5. 入库去重：合格线索写入线索库并去重。
6. 交付：输出一份偏重付费意向、带证据和评分的潜客清单。`,
        title: '公开论坛找付费玩家',
    },
    {
        text: `根据我的业务档案，帮我把来源池扩充到更广的范围。

工作步骤：
1. 读取档案：读取业务档案里的目标游戏、语言、地区、信号词和排除项。
2. 横向发现：围绕这些线索跨平台（Telegram、Facebook、Instagram、YouTube、公开论坛）寻找此前没有收录的相关公开群组、主页、话题和社区。
3. 评估价值：对每个候选来源估算潜在线索规模和相关度，去掉与档案不相关或命中排除项的来源。
4. 写入来源池：把有价值的新来源补充进来源池，并和已有来源去重。
5. 交付：输出一份新增来源清单，说明每个来源的平台、类型和推荐理由，供我确认后再采集线索。`,
        title: '按业务档案扩充来源池',
    },
    {
        text: `对我已有的来源做一次同步，更新线索库。

工作步骤：
1. 选择来源：读取当前来源池里状态正常、仍然有效的来源。
2. 增量同步：在授权账号下重新浏览这些来源的最新公开内容，慢速、单账号操作，遇到验证即停并标记需要人工登录。
3. 识别新意向：找出上次采集之后新出现的、符合业务档案的潜在玩家。
4. 线索打分与去重：对新发现的人按玩家、痛点、付费意愿、活跃度和匹配度打分，和历史线索去重，只保留新增的合格线索。
5. 更新来源状态：根据本次产出刷新每个来源的有效性（有效/低产/失效）。
6. 交付：输出本次新增的潜客清单，并汇总各来源的更新情况。`,
        title: '同步已有来源并更新线索',
    },
    {
        text: `在 Telegram 上为我寻找 {{目标数量}} 个潜在推广代理（能带量的合作方）。

工作步骤：
1. 来源发现：结合业务档案里的目标语言、目标地区和产品类型，搜索推广、引流、渠道合作、代理招募相关的公开群组和频道，整理成来源池。
2. 加入来源：在授权账号下加入最有价值的公开群组，按平台节奏慢速操作，遇到验证就停下并标记需要人工登录。
3. 识别意向：找出自称有流量、带群、做推广、在找代理或分成合作项目的人，以及频道主和群主。
4. 线索打分：重点看推广能力（群规模、频道订阅、过往合作）、合作意愿、近期活跃度和与业务档案的匹配度；排除纯广告机器人和明显的刷量号。
5. 入库去重：合格线索写入线索库，和历史线索自动去重，在备注里写明对方的渠道类型和规模。
6. 交付：输出带证据（发言链接、渠道规模、匹配理由、评分）的代理候选清单。`,
        title: 'Telegram 找推广代理',
    },
    {
        text: `在 Facebook 上为我寻找 {{目标数量}} 个潜在推广代理（能带量的合作方）。

工作步骤：
1. 来源发现：根据业务档案的目标语言、地区和产品类型，找到推广合作、联盟营销、代理招募相关的公开小组和公开主页，整理成来源池。
2. 加入来源：在授权账号下加入值得关注的公开小组，慢速、单账号操作，遇到验证即停并标记需要人工登录。
3. 识别意向：浏览帖子和评论，找出在招合作、求代理项目、晒推广成绩或管理大型小组和主页的人。
4. 线索打分：重点看推广能力（粉丝或成员规模、过往合作）、合作意愿、近期活跃度和与业务档案的匹配度；排除机器人和纯广告号。
5. 入库去重：合格线索写入线索库并与历史记录去重，在备注里写明对方的渠道类型和规模。
6. 交付：输出带证据和评分的代理候选清单。`,
        title: 'Facebook 小组找推广代理',
    },
    {
        text: `在公开论坛和社区上为我寻找 {{目标数量}} 个潜在推广代理或渠道合作方。

工作步骤：
1. 来源发现：结合业务档案的目标语言、地区和产品类型，用联网搜索找到站长、联盟营销、流量交易和推广合作相关的公开论坛、版块和社区，整理成来源池。
2. 浏览内容：抓取相关版块的公开帖子和回复。
3. 识别意向：找出发帖寻找合作项目、出售流量、介绍自己推广渠道或询问代理分成的人。
4. 线索打分：重点看推广能力、合作意愿、近期活跃度和与业务档案的匹配度；排除机器人和重复刷帖的账号。
5. 入库去重：合格线索写入线索库并去重，在备注里写明对方的渠道类型。
6. 交付：输出带证据（帖子链接、渠道说明、匹配理由、评分）的合作方候选清单。`,
        title: '论坛找推广代理与渠道合作',
    },
    {
        text: `在 YouTube 和 TikTok 上为我寻找 {{目标数量}} 个适合合作推广的博主、主播和频道主。

工作步骤：
1. 来源发现：根据业务档案的目标语言、地区和产品类型，搜索同类内容的视频、直播和频道，整理成来源池。
2. 评估创作者：查看每个频道或账号的公开数据：订阅或粉丝数、近期更新频率、播放和互动水平、观众主要语言和地区。
3. 识别合作信号：优先找简介里留了商务合作联系方式、接过同类推广或在内容里提到赞助的创作者。
4. 线索打分：按受众与业务档案的匹配度、影响力、近期活跃度和合作意愿打分；排除长期停更和数据明显异常的账号。
5. 入库去重：合格线索写入线索库并去重，把公开的商务联系方式记入联系方式，在备注里写明粉丝数和内容方向。
6. 交付：输出带证据（频道链接、数据、匹配理由、评分）的创作者候选清单。`,
        title: 'YouTube 与 TikTok 找博主主播',
    },
    {
        text: `在 TikTok 上为我寻找 {{目标数量}} 个有意向的潜在玩家客户。

工作步骤：
1. 来源发现：根据业务档案的目标语言、地区和信号词，搜索相关的话题标签、视频和创作者，整理成来源池。
2. 浏览评论：查看这些视频评论区里的公开留言。
3. 识别意向：找出在评论中表达想玩、问怎么参与、求推荐或表现出付费意愿的用户。
4. 线索打分：按玩家身份、需求痛点、付费意愿、近期活跃度和与业务档案的匹配度打分；排除机器人和刷评论的账号。
5. 入库去重：合格线索写入线索库并去重。
6. 交付：输出带证据（评论链接、匹配理由、评分）的潜客清单。`,
        title: 'TikTok 评论区找意向用户',
    },
    {
        text: `在同类产品的公开社群里为我寻找 {{目标数量}} 个活跃玩家。

工作步骤：
1. 来源发现：根据业务档案的产品类型、目标语言和地区，跨平台（Telegram、Facebook、公开论坛）找到同类产品的公开玩家群、粉丝小组和讨论区，整理成来源池。
2. 加入来源：在授权账号下加入最活跃的公开群组，慢速、单账号操作，遇到验证即停并标记需要人工登录。
3. 识别意向：找出经常发言的老玩家，重点关注抱怨现有产品、想换平台、问有没有更好选择的人。
4. 线索打分：按玩家身份、需求痛点、付费意愿、近期活跃度和与业务档案的匹配度打分；排除对方的客服、管理员和机器人。
5. 入库去重：合格线索写入线索库并去重。
6. 交付：输出带证据和评分的潜客清单，标明每条线索来自哪个社群。`,
        title: '同类社群找活跃玩家',
    },
    {
        text: `跨平台为我寻找 {{目标数量}} 个高价值玩家。

工作步骤：
1. 来源发现：结合业务档案的目标语言、地区和信号词，在 Telegram、Facebook、Instagram、YouTube、TikTok 和公开论坛中找出讨论充值、大额消费、VIP 待遇和返利的公开来源，整理成来源池。
2. 浏览内容：在授权账号下查看这些来源的公开发言和评论。
3. 识别意向：找出晒消费记录、问 VIP 等级和返利政策、比较各家充值优惠，或表示经常大额消费的人。
4. 线索打分：付费意愿加权最高，同时看玩家身份、近期活跃度和与业务档案的匹配度；排除卖号、代充和机器人。
5. 入库去重：合格线索写入线索库并去重，在备注里写明付费证据。
6. 交付：输出一份按付费意愿排序、带证据和评分的高价值潜客清单。`,
        title: '跨平台找高价值玩家',
    },
    {
        text: `为我在 {{目标地区}} 开拓新来源，目标语言为 {{目标语言}}。

工作步骤：
1. 读取档案：读取业务档案里的产品类型、信号词和排除项，并以上面的地区和语言为准。
2. 本地化关键词：把信号词和产品相关词翻译成当地常用说法、俚语和话题标签。
3. 横向发现：用这些关键词跨平台（Telegram、Facebook、Instagram、YouTube、TikTok、公开论坛）寻找当地的公开群组、主页、话题和社区。
4. 评估价值：估算每个来源的规模、活跃度和相关度，去掉不相关或命中排除项的来源。
5. 写入来源池：把有价值的新来源补充进来源池，并和已有来源去重。
6. 交付：输出新增来源清单，说明每个来源的平台、规模和推荐理由。`,
        title: '按新地区和语言开拓来源',
    },
    {
        text: `只同步我指定的来源，不新增来源。

指定来源：
{{来源链接，每行一个}}

工作步骤：
1. 确认来源：在来源池里找到上面列出的来源；不在来源池里的先添加进去，除此之外不搜索、不加入任何其他来源。
2. 同步内容：在授权账号下浏览这些来源的最新公开内容，慢速、单账号操作，遇到验证即停并标记需要人工登录。
3. 识别意向：找出符合业务档案的潜在客户。
4. 线索打分与去重：按玩家身份、需求痛点、付费意愿、活跃度和匹配度打分，和历史线索去重，只保留新增的合格线索。
5. 交付：输出本次新增的潜客清单，并说明每个来源的产出情况。`,
        title: '只同步指定来源',
    },
    {
        text: `执行今天的例行获客，目标新增 {{目标数量}} 条合格线索。

工作步骤：
1. 查看现状：读取来源池和线索库，了解已有来源的产出情况。
2. 同步已有来源：优先同步有效、高产的来源，慢速、单账号操作，遇到验证即停并标记需要人工登录。
3. 补充新来源：如果已有来源的新增线索不足目标数量，按业务档案补充少量新的公开来源再同步。
4. 线索打分与去重：按业务档案打分，和历史线索去重，只保留今天新增的合格线索。
5. 更新来源状态：根据本轮产出刷新每个来源的有效性（有效/低产/失效）。
6. 交付：输出今天新增的潜客清单，并用几句话汇总各来源的产出。`,
        title: '每日例行获客（适合定时任务）',
    },
];

const renderTemplateItem = (item: Template, isCurrent: boolean): ReactNode => (
    <span className={cn('min-w-0 flex-1 truncate', isCurrent && 'font-medium')}>{item.title}</span>
);

// One React element serves every `/templates/:templateId`, so without a key the form instance — which sets
// `keepDirtyValues` so a subscription resync cannot wipe an unsaved body — carried one template's edited text
// onto the next template and Save wrote it to the wrong row. Keying by id gives each entity its own form, the
// way knowledge.tsx already keys <KnowledgeForm>. It also stops `/templates/new` inheriting an abandoned draft.
function Template() {
    const { templateId } = useParams<{ templateId?: string }>();

    return (
        <TemplateForm
            key={templateId ?? 'new'}
            templateId={templateId}
        />
    );
}

function TemplateForm({ templateId }: { templateId?: string }) {
    const navigate = useNavigate();
    const { createTemplate, deleteTemplate, updateTemplate } = useTemplates();

    const { isDesktop, isMobile } = useBreakpoint();
    const isNew = templateId === 'new';

    const templateNav = useTemplateDetailNavigation(isNew ? null : templateId);

    const [expandedPresetIndex, setExpandedPresetIndex] = useState<null | number>(null);
    const [isPresetsOpen, setIsPresetsOpen] = useState(false);
    const [isReplaceConfirmOpen, setIsReplaceConfirmOpen] = useState(false);
    const [isSaving, setIsSaving] = useState(false);
    const [pendingPreset, setPendingPreset] = useState<null | { text: string; title: string }>(null);
    const [isRenaming, setIsRenaming] = useState(false);
    const [isDeleting, setIsDeleting] = useState(false);
    const [isDeleteDialogOpen, setIsDeleteDialogOpen] = useState(false);
    const [viewMode, setViewMode] = useState<EditorViewMode>('rich');

    const {
        handleDropdownCloseAutoFocus,
        inputRef: editingInputRef,
        isEditing: isEditingTitle,
        startEdit: handleTemplateRenameStart,
        stopEdit: handleTemplateRenameCancel,
    } = useInlineEdit({ resetKey: templateId });

    const {
        data: templateData,
        error: templateError,
        loading: isLoadingTemplate,
        refetch: refetchTemplate,
    } = useQuery(FlowTemplateDocument, templateId && !isNew ? { variables: { templateId } } : skipToken);

    const template = templateData?.flowTemplate;
    // A real load failure that left nothing to show, as opposed to a genuine not-found: the page
    // renders it as an in-page ErrorState + Retry instead of the "not found" card. Mirrors flow.
    const templateLoadError = templateError && !template && !isNotFoundError(templateError) ? templateError : undefined;

    // `values` re-syncs the form whenever the cache refreshes (an inline rename, a refetch), while
    // `keepDirtyValues` preserves the user's in-flight edits — without it an external re-emit would
    // silently wipe an unsaved body. Mirrors knowledge-form.
    const initialValues = useMemo<FormValues>(
        () => ({
            text: templateData?.flowTemplate?.text ?? '',
            title: templateData?.flowTemplate?.title ?? '',
        }),
        [templateData?.flowTemplate],
    );

    const form = useAppForm<FormValues>({
        defaultValues: initialValues,
        resetOptions: { keepDirtyValues: true },
        schema: formSchema,
        values: initialValues,
    });

    const { control, formState, getValues, handleSubmit: handleFormSubmit, reset, setValue } = form;
    const { isDirty, isValid } = formState;

    const hasUnsavedChanges = isDirty;
    const templateName = templateData?.flowTemplate?.title ?? null;

    const handleTemplateRenameSave = useCallback(async () => {
        const newTitle = editingInputRef.current?.value.trim();
        const template = templateData?.flowTemplate;

        if (!templateId || !newTitle || !template) {
            return;
        }

        if (newTitle === template.title) {
            handleTemplateRenameCancel();

            return;
        }

        setIsRenaming(true);

        try {
            // Send the server's current `text`, not the form's, so renaming the title never persists the
            // user's unsaved body edits — those stay dirty in the form (kept by `keepDirtyValues`) until they save.
            await updateTemplate(templateId, { text: template.text, title: newTitle });
            toast.success(copy.templatePage.renamed);
            handleTemplateRenameCancel();
        } catch {
            // Error already handled in provider with toast
        } finally {
            setIsRenaming(false);
        }
    }, [editingInputRef, handleTemplateRenameCancel, templateId, templateData?.flowTemplate, updateTemplate]);

    const handleTemplateDelete = useCallback(async () => {
        if (!templateId) {
            return;
        }

        setIsDeleting(true);

        try {
            await deleteTemplate(templateId);
            navigate(routes.templates, { replace: true });
        } catch {
            // Error already handled in provider with toast
        } finally {
            setIsDeleting(false);
        }
    }, [templateId, deleteTemplate, navigate]);

    const performSave = useCallback(
        async (values: FormValues): Promise<boolean> => {
            setIsSaving(true);

            try {
                if (isNew) {
                    await createTemplate(values.title, values.text);
                } else if (templateId) {
                    await updateTemplate(templateId, { text: values.text, title: values.title });
                    // See knowledge-form.tsx: the form-level `resetOptions` are merged into every manual
                    // reset, so a post-save reset inherits `keepDirtyValues` unless it opts out.
                    reset(values, { keepDefaultValues: false, keepDirtyValues: false });
                }

                return true;
            } catch {
                // Error already handled in provider with toast
                return false;
            } finally {
                setIsSaving(false);
            }
        },
        [isNew, templateId, createTemplate, updateTemplate, reset],
    );

    const handleSaveFromGuard = useCallback(async (): Promise<boolean> => {
        if (isSaving || !isValid) {
            return false;
        }

        const parsed = formSchema.safeParse(getValues());

        return parsed.success ? performSave(parsed.data) : false;
    }, [getValues, isSaving, isValid, performSave]);

    const guard = useUnsavedChangesGuard({
        isDirty,
        isFormValid: isValid,
        onSave: handleSaveFromGuard,
    });

    const handleSubmit = async (values: FormValues) => {
        if (isSaving) {
            return;
        }

        if ((await performSave(values)) && isNew) {
            // A fresh template stays dirty until we leave; skip the guard's blocker so this post-save
            // navigation doesn't trap the user in the unsaved-changes dialog.
            guard.skipNextBlock();
            navigate(routes.templates);
        }
    };

    const handleApplyPreset = useCallback(
        (preset: { text: string; title: string }) => {
            const current = getValues();
            const hasContent = (current.title?.trim().length ?? 0) > 0 || (current.text?.trim().length ?? 0) > 0;

            if (hasContent) {
                setPendingPreset(preset);
                setIsReplaceConfirmOpen(true);
            } else {
                setValue('title', preset.title, { shouldDirty: true, shouldValidate: true });
                setValue('text', preset.text, { shouldDirty: true, shouldValidate: true });
            }
        },
        [getValues, setValue],
    );

    const handleConfirmReplacePreset = useCallback(() => {
        if (pendingPreset) {
            setValue('title', pendingPreset.title, { shouldDirty: true, shouldValidate: true });
            setValue('text', pendingPreset.text, { shouldDirty: true, shouldValidate: true });
            setPendingPreset(null);
        }
    }, [pendingPreset, setValue]);

    const hasTemplate = !!templateData?.flowTemplate;
    const isTemplatePending = !isNew && !hasTemplate;
    const isTemplateMissing = !isNew && !isLoadingTemplate && !hasTemplate;

    const pageHeader = (
        <>
            <AppHeader>
                <AppHeaderContent>
                    <Breadcrumb className="min-w-0 flex-1">
                        <BreadcrumbList className="min-w-0 flex-nowrap">
                            <BreadcrumbItem className="min-w-0 gap-2">
                                {isEditingTitle && hasTemplate ? (
                                    <InlineEditInput
                                        busy={isRenaming}
                                        className="w-64 max-w-full min-w-0 flex-1"
                                        defaultValue={templateName ?? ''}
                                        inputRef={editingInputRef}
                                        onCancel={handleTemplateRenameCancel}
                                        onSave={handleTemplateRenameSave}
                                        placeholder={copy.templatePage.renamePlaceholder}
                                    />
                                ) : hasTemplate ? (
                                    <Tooltip>
                                        <TooltipTrigger asChild>
                                            <BreadcrumbPage
                                                className="max-w-64 min-w-0 cursor-text truncate select-none"
                                                onDoubleClick={handleTemplateRenameStart}
                                            >
                                                {templateName ?? copy.templatePage.template}
                                            </BreadcrumbPage>
                                        </TooltipTrigger>
                                        <TooltipContent>{copy.flows.doubleClickRename}</TooltipContent>
                                    </Tooltip>
                                ) : (
                                    <BreadcrumbPage className="min-w-0 truncate">
                                        {isNew
                                            ? copy.templatePage.newTemplate
                                            : (templateName ?? copy.templatePage.template)}
                                    </BreadcrumbPage>
                                )}
                            </BreadcrumbItem>
                        </BreadcrumbList>
                    </Breadcrumb>
                </AppHeaderContent>
                {!isTemplateMissing && (
                    <AppHeaderActions>
                        {!isNew && !isMobile && (
                            <DetailNavigationToolbar<Template>
                                controller={templateNav}
                                renderItem={renderTemplateItem}
                                sheetIcon={<FileText className="size-4" />}
                                sheetTitle={copy.templatePage.titles}
                            />
                        )}
                        <AppHeaderAction
                            disabled={isTemplatePending || (!isNew && !hasUnsavedChanges)}
                            form="template-form"
                            icon={<Save />}
                            label={isNew ? copy.action.create : copy.action.save}
                            loading={isSaving}
                            type="submit"
                        />
                        <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                                <Button
                                    aria-label={copy.templatePage.actions}
                                    className="size-8 p-0"
                                    variant="ghost"
                                >
                                    <Ellipsis />
                                </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent
                                align="end"
                                className="min-w-24"
                                onCloseAutoFocus={handleDropdownCloseAutoFocus}
                            >
                                {!isNew && (
                                    <>
                                        {isMobile && (
                                            <>
                                                <DropdownMenuItem
                                                    className="cursor-default hover:bg-transparent focus:bg-transparent"
                                                    onSelect={(event) => event.preventDefault()}
                                                >
                                                    <FileText />
                                                    {copy.templatePage.titles}
                                                    <div className="-my-1.5 -mr-2 ml-auto flex items-center">
                                                        <DetailNavigationButtons<Template>
                                                            controller={templateNav}
                                                            sheetTitle={copy.templatePage.titles}
                                                            size="sm"
                                                        />
                                                    </div>
                                                </DropdownMenuItem>
                                                <DropdownMenuSeparator />
                                            </>
                                        )}
                                        <DropdownMenuItem
                                            disabled={isTemplatePending}
                                            onClick={handleTemplateRenameStart}
                                        >
                                            <Pencil />
                                            {copy.action.rename}
                                        </DropdownMenuItem>
                                        <DropdownMenuSeparator />
                                    </>
                                )}
                                <DropdownMenuItem
                                    className="cursor-default gap-4 hover:bg-transparent focus:bg-transparent"
                                    onSelect={(event) => event.preventDefault()}
                                >
                                    {copy.prompt.view}
                                    <EditorViewModeToggle
                                        className="-my-1.5 -mr-2 ml-auto"
                                        mode={viewMode}
                                        onModeChange={setViewMode}
                                        rawTooltip={copy.templatePage.editRaw}
                                    />
                                </DropdownMenuItem>
                                {!isNew && (
                                    <>
                                        <DropdownMenuSeparator />
                                        <DropdownMenuItem
                                            disabled={isDeleting || isTemplatePending}
                                            onClick={() => setIsDeleteDialogOpen(true)}
                                        >
                                            {isDeleting ? (
                                                <>
                                                    <Spinner variant="circle" />
                                                    {copy.action.deleting}
                                                </>
                                            ) : (
                                                <>
                                                    <Trash />
                                                    {copy.action.delete}
                                                </>
                                            )}
                                        </DropdownMenuItem>
                                    </>
                                )}
                            </DropdownMenuContent>
                        </DropdownMenu>
                    </AppHeaderActions>
                )}
            </AppHeader>
            {isMobile && !isNew && (
                <DetailNavigationSheet<Template>
                    controller={templateNav}
                    renderItem={renderTemplateItem}
                    sheetIcon={<FileText className="size-4" />}
                    sheetTitle={copy.templatePage.titles}
                />
            )}
        </>
    );

    const presetsList = (onApplied?: () => void) => (
        <div className="flex w-full min-w-0 flex-col gap-2 p-2">
            {PRESET_TEMPLATES.map((preset, index) => (
                <Collapsible
                    className="w-full min-w-0"
                    key={index}
                    onOpenChange={(open) => setExpandedPresetIndex(open ? index : null)}
                    open={expandedPresetIndex === index}
                >
                    <Card className="w-full min-w-0">
                        <div className="flex w-full min-w-0">
                            <Button
                                className={cn(
                                    'h-auto min-w-0 flex-1 justify-start rounded-none rounded-tl-[0.6875rem] px-3 py-2 text-left text-start',
                                    expandedPresetIndex !== index ? 'rounded-bl-[0.6875rem]' : 'whitespace-normal',
                                )}
                                onClick={() => {
                                    handleApplyPreset(preset);
                                    onApplied?.();
                                }}
                                variant="ghost"
                            >
                                <span className={cn('min-w-0', expandedPresetIndex !== index && 'truncate')}>
                                    {preset.title}
                                </span>
                            </Button>
                            <CollapsibleTrigger asChild>
                                <Button
                                    className={cn(
                                        'h-auto shrink-0 rounded-none rounded-tr-[0.6875rem] border-l px-2 py-2',
                                        expandedPresetIndex !== index && 'rounded-br-[0.6875rem]',
                                    )}
                                    variant="ghost"
                                >
                                    <ChevronDown
                                        className={cn(
                                            'transition-transform',
                                            expandedPresetIndex === index && 'rotate-180',
                                        )}
                                    />
                                </Button>
                            </CollapsibleTrigger>
                        </div>
                        <CollapsibleContent>
                            <CardContent className="border-t px-3 py-2">
                                <p className="text-muted-foreground text-sm break-words whitespace-pre-wrap">
                                    {preset.text}
                                </p>
                            </CardContent>
                        </CollapsibleContent>
                    </Card>
                </Collapsible>
            ))}
        </div>
    );

    const presetsPanel = isDesktop ? (
        <div className="bg-card overflow-hidden rounded-lg border">
            <div className="border-b px-4 py-3">
                <h4 className="flex items-center gap-2 text-sm font-medium">
                    {PRESETS_TITLE}
                    <Badge
                        className="ml-auto font-normal tabular-nums"
                        variant="secondary"
                    >
                        {PRESET_TEMPLATES.length}
                    </Badge>
                </h4>
                <p className="text-muted-foreground mt-1 text-xs">{copy.templatePage.presetsHint}</p>
            </div>
            {presetsList()}
        </div>
    ) : (
        <Popover
            onOpenChange={setIsPresetsOpen}
            open={isPresetsOpen}
        >
            <PopoverTrigger asChild>
                <Button
                    className="w-full justify-start"
                    size="sm"
                    variant="secondary"
                >
                    <LayoutTemplate />
                    {PRESETS_TITLE}
                    <Badge
                        className="ml-auto h-5 font-normal tabular-nums"
                        variant="outline"
                    >
                        {PRESET_TEMPLATES.length}
                    </Badge>
                </Button>
            </PopoverTrigger>
            <PopoverContent
                align="start"
                className="max-h-(--radix-popover-content-available-height) w-(--radix-popover-trigger-width) overflow-y-auto overscroll-contain p-0"
            >
                {presetsList(() => setIsPresetsOpen(false))}
            </PopoverContent>
        </Popover>
    );

    const introBlock = (
        <div className="flex flex-col gap-2 text-center">
            <h2 className="text-2xl font-semibold">
                {isNew ? copy.templatePage.createTitle : copy.templatePage.editTitle}
            </h2>
            <p className="text-muted-foreground">{copy.templatePage.subtitle}</p>
        </div>
    );

    const titleField = (
        <FormField
            control={control}
            name="title"
            render={({ field }) => (
                <FormItem>
                    <FormLabel>{copy.templatePage.title}</FormLabel>
                    <FormControl>
                        <Input
                            autoFocus={isNew}
                            disabled={isSaving}
                            placeholder={copy.templatePage.titlePlaceholder}
                            {...field}
                        />
                    </FormControl>
                    <FormMessage />
                </FormItem>
            )}
        />
    );

    const textEditor = (
        <FormField
            control={control}
            name="text"
            render={({ field }) => (
                <FormItem className="flex min-h-0 flex-1 flex-col">
                    <FormControl>
                        <MarkdownEditorField
                            aria-label={copy.templatePage.contentLabel}
                            disabled={isSaving}
                            mode={viewMode}
                            onBlur={field.onBlur}
                            onChange={field.onChange}
                            placeholder={copy.templatePage.contentPlaceholder}
                            ref={field.ref}
                            value={field.value}
                        />
                    </FormControl>
                    {/* Full-height field: the invalid state shows as the editor's red border (via aria-invalid),
                        not text below it (no room in the flex layout). Kept sr-only for screen readers. */}
                    <FormMessage className="sr-only" />
                </FormItem>
            )}
        />
    );

    if (!isNew && isLoadingTemplate && !template) {
        return (
            <div className={isDesktop ? 'flex h-[100dvh] min-h-0 flex-col' : 'flex min-h-[100dvh] flex-col'}>
                {pageHeader}
                <div className="flex flex-1 items-center justify-center">
                    <Spinner variant="circle" />
                </div>
            </div>
        );
    }

    if (templateLoadError) {
        return (
            <div className={isDesktop ? 'flex h-[100dvh] min-h-0 flex-col' : 'flex min-h-[100dvh] flex-col'}>
                {pageHeader}
                <div className="flex flex-1 flex-col gap-4 p-4">
                    <ErrorState
                        message={templateLoadError.message}
                        onRetry={() => refetchTemplate()}
                        title={copy.templatePage.loadFailed}
                    />
                </div>
            </div>
        );
    }

    if (!isNew && !isLoadingTemplate && !template) {
        return (
            <div className={isDesktop ? 'flex h-[100dvh] min-h-0 flex-col' : 'flex min-h-[100dvh] flex-col'}>
                {pageHeader}
                <div className="flex flex-1 items-center justify-center p-4">
                    <Card className="w-full max-w-2xl">
                        <CardContent className="flex flex-col items-center gap-4 pt-6 text-center">
                            <h2 className="text-xl font-semibold">{copy.templatePage.notFound}</h2>
                            <p className="text-muted-foreground">{copy.templatePage.notFoundHint}</p>
                            <Button onClick={() => navigate(routes.templates)}>{copy.templatePage.backToList}</Button>
                        </CardContent>
                    </Card>
                </div>
            </div>
        );
    }

    return (
        <div className={isDesktop ? 'flex h-[100dvh] min-h-0 flex-col' : 'flex min-h-[100dvh] flex-col'}>
            {pageHeader}
            <Form {...form}>
                <form
                    className="flex min-h-0 flex-1 flex-col"
                    id="template-form"
                    noValidate
                    onSubmit={handleFormSubmit(handleSubmit)}
                >
                    {isDesktop ? (
                        <DetailSplitLayout
                            content={textEditor}
                            panel={
                                <>
                                    {introBlock}
                                    {titleField}
                                    {presetsPanel}
                                </>
                            }
                        />
                    ) : (
                        <div className="flex min-h-0 flex-1 flex-col gap-4 p-4">
                            {introBlock}
                            {titleField}
                            {presetsPanel}
                            {textEditor}
                        </div>
                    )}
                </form>
            </Form>
            <ConfirmationDialog
                confirmIcon={<FileSymlink />}
                confirmText={copy.action.replace}
                confirmVariant="default"
                description={copy.templatePage.replaceConfirm}
                handleConfirm={handleConfirmReplacePreset}
                handleOpenChange={(open) => {
                    if (!open) {
                        setPendingPreset(null);
                    }

                    setIsReplaceConfirmOpen(open);
                }}
                isOpen={isReplaceConfirmOpen}
                title={copy.templatePage.replaceTitle}
            />
            <ConfirmationDialog
                cancelText={copy.action.cancel}
                confirmText={copy.action.delete}
                handleConfirm={handleTemplateDelete}
                handleOpenChange={setIsDeleteDialogOpen}
                isOpen={isDeleteDialogOpen}
                itemName={templateName ?? undefined}
                itemType={copy.itemType.template}
            />
            <UnsavedChangesDialog
                canSave={isValid}
                handleCancel={guard.handleCancel}
                handleDiscard={guard.handleDiscard}
                handleOpenChange={guard.handleOpenChange}
                handleSaveAndLeave={guard.handleSaveAndLeave}
                isOpen={guard.isOpen}
                isSavingFromDialog={guard.isSavingFromDialog}
            />
        </div>
    );
}

export default Template;
