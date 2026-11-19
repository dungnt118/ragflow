import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { CreateAgentByAIDialog } from './create-agent-by-ai-dialog';
import { AgentCategory } from '@/constants/agent';

const mockGenerateAgentByAI = jest.fn();
const mockSetAgent = jest.fn();
const mockNavigateToAgentFn = jest.fn();
const mockNavigateToAgent = jest.fn(() => mockNavigateToAgentFn);

jest.mock('@/hooks/use-agent-request', () => ({
  useGenerateAgentByAI: () => ({
    loading: false,
    generateAgentByAI: mockGenerateAgentByAI,
  }),
  useSetAgent: () => ({
    loading: false,
    setAgent: mockSetAgent,
  }),
}));

jest.mock('@/hooks/logic-hooks/navigate-hooks', () => ({
  useNavigatePage: () => ({
    navigateToAgent: mockNavigateToAgent,
  }),
}));

jest.mock('@/components/model-tree-select', () => ({
  ModelTreeSelectFormField: () => <div data-testid="mock-model-select" />,
}));

jest.mock('@/components/knowledge-base-item', () => ({
  KnowledgeBaseFormField: () => <div data-testid="mock-kb-select" />,
}));

const wrapper = ({ children }: { children: React.ReactNode }) => (
  <QueryClientProvider client={new QueryClient()}>
    {children}
  </QueryClientProvider>
);

describe('CreateAgentByAIDialog', () => {
  const mockHideModal = jest.fn();

  beforeEach(() => {
    mockGenerateAgentByAI.mockReset();
    mockSetAgent.mockReset();
    mockNavigateToAgent.mockClear();
    mockNavigateToAgentFn.mockClear();
    mockHideModal.mockReset();
  });

  it('fills prompt and category when a quick prompt preset chip is clicked', () => {
    render(<CreateAgentByAIDialog visible hideModal={mockHideModal} />, {
      wrapper,
    });

    const presetBtn = screen.getByTestId('ai-preset-cskh_routing');
    fireEvent.click(presetBtn);

    const textarea = screen.getByTestId(
      'ai-prompt-textarea',
    ) as HTMLTextAreaElement;
    expect(textarea.value.length).toBeGreaterThan(20);
  });

  it('supports Step 1 -> Step 2 Architecture Preview and then Confirm & Create', async () => {
    mockGenerateAgentByAI.mockResolvedValue({
      code: 0,
      data: {
        title: 'Trợ lý CSKH AI',
        description: 'Luồng phân loại và giải đáp CSKH',
        canvas_category: AgentCategory.AgentCanvas,
        explanation: 'Begin -> Categorize -> Retrieval -> Agent -> Message',
        used_fallback: false,
        node_plan: [
          {
            id: 'begin',
            component_name: 'Begin',
            name: 'Begin',
            role: 'Khởi tạo',
            upstream_ids: [],
            downstream_ids: ['Categorize:AINode_2'],
          },
          {
            id: 'Categorize:AINode_2',
            component_name: 'Categorize',
            name: 'Phân loại',
            role: 'Rẽ nhánh',
            upstream_ids: ['begin'],
            downstream_ids: ['Message:AINode_3'],
          },
          {
            id: 'Message:AINode_3',
            component_name: 'Message',
            name: 'Phản hồi',
            role: 'Xuất kết quả',
            upstream_ids: ['Categorize:AINode_2'],
            downstream_ids: [],
          },
        ],
        dsl: {
          components: {},
          graph: { nodes: [], edges: [] },
        },
      },
    });

    mockSetAgent.mockResolvedValue({
      code: 0,
      data: { id: 'canvas-created-from-preview' },
    });

    render(<CreateAgentByAIDialog visible hideModal={mockHideModal} />, {
      wrapper,
    });

    fireEvent.change(screen.getByTestId('ai-prompt-textarea'), {
      target: {
        value: 'Tạo trợ lý CSKH tự động phân loại câu hỏi và tra cứu tài liệu',
      },
    });

    fireEvent.click(screen.getByTestId('ai-generate-preview-btn'));

    await waitFor(() => {
      expect(screen.getByTestId('ai-architecture-preview')).toBeInTheDocument();
    });

    expect(mockGenerateAgentByAI).toHaveBeenCalledWith(
      expect.objectContaining({
        prompt: 'Tạo trợ lý CSKH tự động phân loại câu hỏi và tra cứu tài liệu',
        auto_create: false,
      }),
    );

    // Edit title in preview step
    fireEvent.change(screen.getByTestId('ai-preview-title-input'), {
      target: { value: 'Trợ lý CSKH AI Tinh chỉnh' },
    });

    fireEvent.click(screen.getByTestId('ai-confirm-create-btn'));

    await waitFor(() => {
      expect(mockSetAgent).toHaveBeenCalledWith(
        expect.objectContaining({
          title: 'Trợ lý CSKH AI Tinh chỉnh',
          canvas_category: AgentCategory.AgentCanvas,
        }),
      );
      expect(mockHideModal).toHaveBeenCalled();
      expect(mockNavigateToAgent).toHaveBeenCalledWith(
        'canvas-created-from-preview',
        AgentCategory.AgentCanvas,
      );
    });
  });

  it('supports 1-Click Direct Create & Open on Canvas (auto_create: true)', async () => {
    mockGenerateAgentByAI.mockResolvedValue({
      code: 0,
      data: {
        title: 'Pipeline Hợp đồng Pháp lý',
        description: 'Bóc tách hợp đồng theo tiêu đề',
        canvas_category: AgentCategory.DataflowCanvas,
        explanation: 'File -> Parser -> TitleChunker -> Extractor -> Tokenizer',
        used_fallback: false,
        node_plan: [],
        dsl: { components: {}, graph: { nodes: [], edges: [] } },
        agent: {
          id: 'canvas-direct-created-001',
          title: 'Pipeline Hợp đồng Pháp lý',
        },
      },
    });

    render(<CreateAgentByAIDialog visible hideModal={mockHideModal} />, {
      wrapper,
    });

    fireEvent.click(screen.getByTestId('ai-preset-pipeline_legal_sop'));
    fireEvent.click(screen.getByTestId('ai-direct-create-btn'));

    await waitFor(() => {
      expect(mockGenerateAgentByAI).toHaveBeenCalledWith(
        expect.objectContaining({
          canvas_category: AgentCategory.DataflowCanvas,
          auto_create: true,
        }),
      );
      expect(mockHideModal).toHaveBeenCalled();
      expect(mockNavigateToAgent).toHaveBeenCalledWith(
        'canvas-direct-created-001',
        AgentCategory.DataflowCanvas,
      );
    });
  });
});
