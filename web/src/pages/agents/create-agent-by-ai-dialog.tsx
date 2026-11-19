import { KnowledgeBaseFormField } from '@/components/knowledge-base-item';
import { ModelTreeSelectFormField } from '@/components/model-tree-select';
import { Badge } from '@/components/ui/badge';
import { Button, ButtonLoading } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Form } from '@/components/ui/form';
import { Input } from '@/components/ui/input';
import { Textarea } from '@/components/ui/textarea';
import { AgentCategory } from '@/constants/agent';
import { useNavigatePage } from '@/hooks/logic-hooks/navigate-hooks';
import {
  useGenerateAgentByAI,
  useSetAgent,
} from '@/hooks/use-agent-request';
import { IModalProps } from '@/interfaces/common';
import {
  IAINodePlanItem,
  IGenerateAgentByAIResponse,
} from '@/interfaces/database/agent';
import { cn } from '@/lib/utils';
import {
  ArrowRight,
  Bot,
  BrainCircuit,
  CheckCircle2,
  Database,
  FileText,
  GitBranch,
  Globe,
  Layers,
  RefreshCw,
  Route,
  Sparkles,
  Wand2,
  Zap,
} from 'lucide-react';
import { useCallback, useState } from 'react';
import { useForm } from 'react-hook-form';
import { useTranslation } from 'react-i18next';

type CreateAgentByAIDialogProps = IModalProps<any>;

type AIFormValues = {
  prompt: string;
  canvas_category: 'auto' | AgentCategory.AgentCanvas | AgentCategory.DataflowCanvas;
  llm_id?: string;
  dataset_ids?: string[];
  title?: string;
  description?: string;
};

interface QuickPromptPreset {
  id: string;
  labelVi: string;
  labelEn: string;
  category: 'auto' | AgentCategory.AgentCanvas | AgentCategory.DataflowCanvas;
  promptVi: string;
  promptEn: string;
}

const QUICK_PROMPT_PRESETS: QuickPromptPreset[] = [
  {
    id: 'cskh_routing',
    labelVi: 'CSKH Phân loại & Tra cứu Chính sách',
    labelEn: 'Customer Support Triage & Policy RAG',
    category: AgentCategory.AgentCanvas,
    promptVi:
      'Xây dựng Trợ lý CSKH thông minh: tự động phân loại câu hỏi của khách hàng thành 2 nhánh (Hỏi đáp sản phẩm/chính sách bảo hành vs Khiếu nại dịch vụ khẩn cấp). Nhánh hỏi đáp sẽ tra cứu Kho tri thức (Dataset) để trả lời chính xác; nhánh khiếu nại sẽ phân tích mức độ nghiêm trọng và soạn phản hồi xử lý ưu tiên.',
    promptEn:
      'Build a smart customer support assistant that classifies user queries into Product/Policy Q&A (using Dataset Retrieval + RAG Agent) vs Urgent Service Complaints (dedicated escalation Agent).',
  },
  {
    id: 'hybrid_research',
    labelVi: 'Nghiên cứu Đa nguồn (RAG + Web Search)',
    labelEn: 'Hybrid Deep Research (RAG + Live Web)',
    category: AgentCategory.AgentCanvas,
    promptVi:
      'Thiết kế Trợ lý Nghiên cứu Thị trường & Đối thủ: kết hợp đồng thời tra cứu báo cáo nội bộ trong Dataset (Retrieval) với công cụ tìm kiếm Internet thời gian thực (TavilySearch, DuckDuckGo, Wikipedia) để tổng hợp báo cáo phân tích đa chiều có trích dẫn nguồn.',
    promptEn:
      'Design a Market & Competitor Research Assistant combining internal Dataset Retrieval with live web search tools (TavilySearch, DuckDuckGo, Wikipedia) to synthesize a cited report.',
  },
  {
    id: 'web_crawler_cms',
    labelVi: 'Cào Web & Khai thác Cổng CMS (Web Crawler + RAG)',
    labelEn: 'Web Crawler & CMS Portal Extractor (Crawler + RAG)',
    category: AgentCategory.AgentCanvas,
    promptVi:
      'Xây dựng Trợ lý AI Thu thập & Khai thác Cổng thông tin Website / CMS (ví dụ: https://cms.demego.vn/): sử dụng Action Node Crawler để cào dữ liệu bài viết sạch dạng Markdown (tự động lọc bỏ menu/sidebar nhiễu và phát hiện liên kết SSR/SPA), kết hợp tra cứu Kho tri thức (Retrieval) và trang bị công cụ Web Crawler đệ quy cho Agent để giải đáp chi tiết.',
    promptEn:
      'Build an AI Web Crawler & CMS Knowledge Assistant (e.g., https://cms.demego.vn/): use the Crawler action node to extract clean Markdown articles and discover same-domain links, combined with Dataset Retrieval and an Agent equipped with the recursive Crawler tool.',
  },
  {
    id: 'text2sql_bi',
    labelVi: 'Chuyên gia Text-to-SQL & Báo cáo BI',
    labelEn: 'Text-to-SQL & BI Data Analyst',
    category: AgentCategory.AgentCanvas,
    promptVi:
      'Tạo Chuyên gia Phân tích Dữ liệu Text-to-SQL: tra cứu cấu trúc bảng (Schema/DDL) từ Dataset, tự động sinh câu lệnh truy vấn SQL chuẩn xác, thực thi trên cơ sở dữ liệu qua Node ExeSQL và diễn giải bảng kết quả thành báo cáo BI trực quan.',
    promptEn:
      'Create a Text-to-SQL BI Analyst workflow: retrieve table schemas from Dataset, generate SQL SELECT queries, execute via ExeSQL node, and interpret results into an executive BI report.',
  },
  {
    id: 'enterprise_rag',
    labelVi: 'Trợ lý Hỏi đáp Tri thức Nội bộ Chuẩn xác',
    labelEn: 'Enterprise Knowledge Q&A Assistant',
    category: AgentCategory.AgentCanvas,
    promptVi:
      'Xây dựng Trợ lý Hỏi đáp Tri thức Doanh nghiệp chống ảo giác (Anti-Hallucination): truy hồi ngữ nghĩa kết hợp từ khóa (Hybrid Search) trên Dataset nội bộ và tổng hợp câu trả lời chi tiết kèm trích dẫn.',
    promptEn:
      'Build an anti-hallucination Enterprise Knowledge Q&A workflow using Hybrid Retrieval over internal Datasets and a grounded synthesis Agent.',
  },
  {
    id: 'pipeline_legal_sop',
    labelVi: 'Pipeline Bóc tách Hợp đồng / SOP Phân cấp',
    labelEn: 'Hierarchical Legal / SOP Ingestion Pipeline',
    category: AgentCategory.DataflowCanvas,
    promptVi:
      'Thiết kế Ingestion Pipeline nạp tài liệu Hợp đồng pháp lý, Quy chế và Sổ tay SOP: bóc tách bố cục đa định dạng (Parser), cắt đoạn theo cấu trúc tiêu đề Chương/Mục/Điều khoản H1-H3 (TitleChunker), dùng AI trích xuất Tóm tắt + Câu hỏi giả định HyDE (Extractor) và lập chỉ mục Hybrid (Tokenizer).',
    promptEn:
      'Design a hierarchical document ingestion pipeline for legal contracts and SOP manuals: Parser -> TitleChunker (H1-H3 hierarchy) -> AI Context Extractor (Summary, Keywords, HyDE Questions) -> Hybrid Tokenizer.',
  },
  {
    id: 'pipeline_advanced_etl',
    labelVi: 'Pipeline Nạp & Làm giàu Đa phương thức',
    labelEn: 'Multimodal Enriched Ingestion Pipeline',
    category: AgentCategory.DataflowCanvas,
    promptVi:
      'Xây dựng Dataflow Pipeline xử lý tài liệu PDF phức tạp (DeepDOC), bảng tính Excel và hình ảnh OCR: cắt đoạn 512 tokens có gối đầu 10% (TokenChunker), làm giàu siêu dữ liệu bằng LLM (Extractor) và đánh chỉ mục Vector + Full-text (Tokenizer).',
    promptEn:
      'Build an advanced multimodal ingestion pipeline for PDF (DeepDOC), Excel, and OCR images: Parser -> TokenChunker (512 tokens, 10% overlap) -> AI Extractor -> Hybrid Tokenizer.',
  },
];

function getNodeIcon(componentName: string) {
  switch (componentName) {
    case 'Begin':
    case 'File':
      return <Zap className="size-4 text-emerald-500" />;
    case 'Retrieval':
    case 'ExeSQL':
      return <Database className="size-4 text-blue-500" />;
    case 'Categorize':
      return <GitBranch className="size-4 text-amber-500" />;
    case 'Agent':
      return <Bot className="size-4 text-purple-500" />;
    case 'Crawler':
      return <Globe className="size-4 text-emerald-500" />;
    case 'Parser':
    case 'TokenChunker':
    case 'TitleChunker':
      return <FileText className="size-4 text-cyan-500" />;
    case 'Extractor':
      return <Sparkles className="size-4 text-pink-500" />;
    case 'Tokenizer':
      return <Layers className="size-4 text-indigo-500" />;
    default:
      return <Globe className="size-4 text-teal-500" />;
  }
}

export function CreateAgentByAIDialog({ hideModal }: CreateAgentByAIDialogProps) {
  const { t, i18n } = useTranslation();
  const isVi = (i18n.language || 'en').toLowerCase().startsWith('vi');
  const { navigateToAgent } = useNavigatePage();

  const { loading: generating, generateAgentByAI } = useGenerateAgentByAI();
  const { loading: creating, setAgent } = useSetAgent();

  const [previewResult, setPreviewResult] =
    useState<IGenerateAgentByAIResponse | null>(null);
  const [promptError, setPromptError] = useState<string>('');

  const form = useForm<AIFormValues>({
    defaultValues: {
      prompt: '',
      canvas_category: 'auto',
      llm_id: '',
      dataset_ids: [],
      title: '',
      description: '',
    },
  });

  const watchedPrompt = form.watch('prompt');
  const watchedCategory = form.watch('canvas_category');
  const watchedTitle = form.watch('title');
  const watchedDescription = form.watch('description');

  const handleSelectPreset = useCallback(
    (preset: QuickPromptPreset) => {
      form.setValue('prompt', isVi ? preset.promptVi : preset.promptEn, {
        shouldDirty: true,
      });
      form.setValue('canvas_category', preset.category, {
        shouldDirty: true,
      });
      setPromptError('');
    },
    [form, isVi],
  );

  const validatePromptInput = useCallback(() => {
    const trimmed = (form.getValues('prompt') || '').trim();
    if (trimmed.length < 6) {
      setPromptError(t('flow.aiPromptRequired'));
      return null;
    }
    setPromptError('');
    return trimmed;
  }, [form, t]);

  // Step 1 -> Step 2: Generate Architecture Preview
  const handleGeneratePreview = useCallback(async () => {
    const validPrompt = validatePromptInput();
    if (!validPrompt) return;

    const values = form.getValues();
    const res = await generateAgentByAI({
      prompt: validPrompt,
      canvas_category: values.canvas_category,
      llm_id: values.llm_id || undefined,
      dataset_ids: values.dataset_ids?.length ? values.dataset_ids : undefined,
      auto_create: false,
    });

    if (res?.code === 0 && res.data) {
      setPreviewResult(res.data);
      form.setValue('title', res.data.title);
      form.setValue('description', res.data.description);
    }
  }, [form, generateAgentByAI, validatePromptInput]);

  // 1-Click Direct Create & Open on Canvas (DEC-01: OPT-1)
  const handleDirectCreate = useCallback(async () => {
    const validPrompt = validatePromptInput();
    if (!validPrompt) return;

    const values = form.getValues();
    const res = await generateAgentByAI({
      prompt: validPrompt,
      canvas_category: values.canvas_category,
      llm_id: values.llm_id || undefined,
      dataset_ids: values.dataset_ids?.length ? values.dataset_ids : undefined,
      auto_create: true,
    });

    if (res?.code === 0 && res.data?.agent?.id) {
      const targetCategory =
        (res.data.canvas_category as AgentCategory) ||
        AgentCategory.AgentCanvas;
      hideModal?.();
      navigateToAgent(res.data.agent.id, targetCategory)();
    }
  }, [form, generateAgentByAI, hideModal, navigateToAgent, validatePromptInput]);

  // Confirm & Create from Step 2 Preview (allows user to edit title & description before saving)
  const handleConfirmPreviewCreate = useCallback(async () => {
    if (!previewResult) return;
    const finalTitle = (watchedTitle || previewResult.title).trim();
    const finalDescription = (
      watchedDescription ??
      previewResult.description ??
      ''
    ).trim();
    const targetCategory =
      (previewResult.canvas_category as AgentCategory) ||
      AgentCategory.AgentCanvas;

    const ret = await setAgent({
      title: finalTitle,
      description: finalDescription,
      canvas_category: targetCategory,
      dsl: previewResult.dsl as unknown as Record<string, any>,
    });

    if (ret?.code === 0 && ret?.data?.id) {
      hideModal?.();
      navigateToAgent(ret.data.id, targetCategory)();
    }
  }, [
    hideModal,
    navigateToAgent,
    previewResult,
    setAgent,
    watchedDescription,
    watchedTitle,
  ]);

  return (
    <Dialog open onOpenChange={hideModal}>
      <DialogContent
        data-testid="agent-create-by-ai-modal"
        className="max-w-[880px] max-h-[90vh] overflow-y-auto"
      >
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2.5 text-lg">
            <span className="inline-flex items-center justify-center size-8 rounded-lg bg-primary/15 text-primary">
              <Sparkles className="size-4" />
            </span>
            {t('flow.createByAITitle')}
          </DialogTitle>
          <DialogDescription className="text-sm text-text-secondary leading-relaxed">
            {t('flow.createByAIDescription')}
          </DialogDescription>
        </DialogHeader>

        <Form {...form}>
          <div className="space-y-5 py-2">
            {/* Quick Prompt Presets */}
            <div className="space-y-2">
              <label className="text-xs font-medium uppercase tracking-wider text-text-secondary flex items-center gap-1.5">
                <Wand2 className="size-3.5 text-primary" />
                {t('flow.aiQuickPromptsLabel')}
              </label>
              <div className="flex flex-wrap gap-2">
                {QUICK_PROMPT_PRESETS.map((preset) => (
                  <button
                    key={preset.id}
                    type="button"
                    data-testid={`ai-preset-${preset.id}`}
                    onClick={() => handleSelectPreset(preset)}
                    className="text-xs px-3 py-1.5 rounded-full border border-border-default bg-bg-card hover:border-primary hover:text-primary transition-colors text-left flex items-center gap-1.5"
                  >
                    {preset.category === AgentCategory.DataflowCanvas ? (
                      <Route className="size-3 shrink-0 text-cyan-500" />
                    ) : (
                      <BrainCircuit className="size-3 shrink-0 text-purple-500" />
                    )}
                    <span>{isVi ? preset.labelVi : preset.labelEn}</span>
                  </button>
                ))}
              </div>
            </div>

            {/* Natural Language Prompt Textarea */}
            <div className="space-y-2">
              <label className="text-sm font-medium text-text-primary">
                {t('flow.aiPromptLabel')} <span className="text-red-500">*</span>
              </label>
              <Textarea
                data-testid="ai-prompt-textarea"
                rows={4}
                value={watchedPrompt}
                onChange={(e) => {
                  form.setValue('prompt', e.target.value);
                  if (promptError) setPromptError('');
                }}
                placeholder={t('flow.aiPromptPlaceholder')}
                className="resize-y min-h-[104px]"
              />
              {promptError && (
                <p className="text-xs text-red-500">{promptError}</p>
              )}
            </div>

            {/* Target Flow Category Selector */}
            <div className="space-y-2">
              <label className="text-sm font-medium text-text-primary">
                {t('flow.aiCategoryLabel')}
              </label>
              <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
                {[
                  {
                    value: 'auto' as const,
                    label: t('flow.aiCategoryAuto'),
                    icon: Sparkles,
                  },
                  {
                    value: AgentCategory.AgentCanvas,
                    label: t('flow.aiCategoryAgent'),
                    icon: BrainCircuit,
                  },
                  {
                    value: AgentCategory.DataflowCanvas,
                    label: t('flow.aiCategoryDataflow'),
                    icon: Route,
                  },
                ].map((opt) => {
                  const Icon = opt.icon;
                  const active = watchedCategory === opt.value;
                  return (
                    <button
                      key={opt.value}
                      type="button"
                      onClick={() => form.setValue('canvas_category', opt.value)}
                      className={cn(
                        'flex items-center gap-2.5 p-3 rounded-lg border text-left text-xs font-medium transition-all',
                        active
                          ? 'border-primary bg-primary/10 text-text-primary shadow-sm'
                          : 'border-border-default bg-transparent text-text-secondary hover:border-text-secondary',
                      )}
                    >
                      <Icon className="size-4 shrink-0" />
                      <span className="line-clamp-2">{opt.label}</span>
                    </button>
                  );
                })}
              </div>
            </div>

            {/* Optional Model & Dataset Binding */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pt-1 border-t border-border-default">
              <div className="space-y-1.5">
                <ModelTreeSelectFormField
                  name="llm_id"
                  label={t('chat.model')}
                />
                <p className="text-[11px] text-text-secondary">
                  {t('flow.aiModelOptionalHint')}
                </p>
              </div>
              <div className="space-y-1.5">
                <KnowledgeBaseFormField
                  name="dataset_ids"
                  showVariable={false}
                />
                <p className="text-[11px] text-text-secondary">
                  {t('flow.aiDatasetOptionalHint')}
                </p>
              </div>
            </div>

            {/* Step 2: Architecture Preview Panel */}
            {previewResult && (
              <div
                data-testid="ai-architecture-preview"
                className="rounded-xl border border-primary/40 bg-bg-card/70 p-4 space-y-4"
              >
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex items-center gap-2">
                    <CheckCircle2 className="size-4 text-emerald-500" />
                    <h4 className="text-sm font-semibold text-text-primary">
                      {t('flow.aiPreviewSectionTitle')}
                    </h4>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge variant="secondary" className="text-xs">
                      {previewResult.canvas_category ===
                      AgentCategory.DataflowCanvas
                        ? t('flow.ingestionPipeline')
                        : t('flow.ceateAgent')}
                    </Badge>
                    <Badge variant="outline" className="text-xs">
                      {t('flow.aiPreviewNodesCount', {
                        count: previewResult.node_plan?.length ?? 0,
                      })}
                    </Badge>
                  </div>
                </div>

                {/* Editable Title & Description */}
                <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
                  <div className="space-y-1">
                    <label className="text-xs font-medium text-text-secondary">
                      {t('flow.name')}
                    </label>
                    <Input
                      data-testid="ai-preview-title-input"
                      value={watchedTitle}
                      onChange={(e) => form.setValue('title', e.target.value)}
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-xs font-medium text-text-secondary">
                      {t('flow.description')}
                    </label>
                    <Input
                      value={watchedDescription}
                      onChange={(e) =>
                        form.setValue('description', e.target.value)
                      }
                    />
                  </div>
                </div>

                {/* Architectural Explanation */}
                <div className="text-xs text-text-secondary bg-background/60 rounded-lg p-3 border border-border-default leading-relaxed">
                  {previewResult.explanation}
                </div>

                {/* Node Topology Cards */}
                <div className="space-y-2">
                  <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-2.5">
                    {(previewResult.node_plan || []).map(
                      (node: IAINodePlanItem, index: number) => (
                        <div
                          key={node.id}
                          className="p-2.5 rounded-lg border border-border-default bg-background/80 flex flex-col gap-1.5"
                        >
                          <div className="flex items-center justify-between gap-1.5">
                            <span className="inline-flex items-center gap-1.5 text-xs font-semibold text-text-primary truncate">
                              {getNodeIcon(node.component_name)}
                              <span className="truncate">{node.name}</span>
                            </span>
                            <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-bg-card text-text-secondary shrink-0">
                              #{index + 1} {node.component_name}
                            </span>
                          </div>
                          <p className="text-[11px] text-text-secondary line-clamp-2 leading-snug">
                            {node.role}
                          </p>
                        </div>
                      ),
                    )}
                  </div>
                </div>
              </div>
            )}
          </div>
        </Form>

        <DialogFooter className="flex flex-wrap items-center justify-between gap-2 sm:justify-end">
          <Button type="button" variant="outline" onClick={hideModal}>
            {t('common.cancel')}
          </Button>

          {!previewResult ? (
            <>
              <ButtonLoading
                type="button"
                variant="secondary"
                data-testid="ai-generate-preview-btn"
                loading={generating}
                onClick={handleGeneratePreview}
              >
                <Sparkles className="size-4 mr-1.5" />
                {generating
                  ? t('flow.aiGeneratingText')
                  : t('flow.aiGeneratePreviewBtn')}
              </ButtonLoading>
              <ButtonLoading
                type="button"
                data-testid="ai-direct-create-btn"
                loading={generating}
                onClick={handleDirectCreate}
              >
                <Zap className="size-4 mr-1.5" />
                {t('flow.aiDirectCreateBtn')}
              </ButtonLoading>
            </>
          ) : (
            <>
              <ButtonLoading
                type="button"
                variant="outline"
                loading={generating}
                onClick={handleGeneratePreview}
              >
                <RefreshCw className="size-4 mr-1.5" />
                {t('flow.aiReGenerateBtn')}
              </ButtonLoading>
              <ButtonLoading
                type="button"
                data-testid="ai-confirm-create-btn"
                loading={creating}
                onClick={handleConfirmPreviewCreate}
              >
                {t('flow.aiConfirmCreateBtn')}
                <ArrowRight className="size-4 ml-1.5" />
              </ButtonLoading>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
