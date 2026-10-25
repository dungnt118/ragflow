//
//  Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
//

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"ragflow/internal/agent/component"
	dslpkg "ragflow/internal/agent/dsl"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	modelModule "ragflow/internal/entity/models"
)

// GenerateAgentByAIRequest is the request payload for POST /api/v1/agents/generate-by-ai.
type GenerateAgentByAIRequest struct {
	Prompt         string   `json:"prompt"`
	Title          string   `json:"title,omitempty"`
	CanvasCategory string   `json:"canvas_category,omitempty"` // "agent_canvas" | "dataflow_canvas" | "auto"
	LLMID          string   `json:"llm_id,omitempty"`
	DatasetIDs     []string `json:"dataset_ids,omitempty"`
	AutoCreate     bool     `json:"auto_create,omitempty"`
}

// AINodePlanItem describes one synthesized node in the AI-generated architecture preview.
type AINodePlanItem struct {
	ID            string   `json:"id"`
	ComponentName string   `json:"component_name"`
	Name          string   `json:"name"`
	Role          string   `json:"role"`
	UpstreamIDs   []string `json:"upstream_ids"`
	DownstreamIDs []string `json:"downstream_ids"`
}

// GenerateAgentByAIResponse is the response payload for POST /api/v1/agents/generate-by-ai.
type GenerateAgentByAIResponse struct {
	Title          string            `json:"title"`
	Description    string            `json:"description"`
	CanvasCategory string            `json:"canvas_category"`
	Explanation    string            `json:"explanation"`
	NodePlan       []AINodePlanItem  `json:"node_plan"`
	DSL            entity.JSONMap    `json:"dsl"`
	UsedFallback   bool              `json:"used_fallback"`
	Agent          *entity.UserCanvas `json:"agent,omitempty"`
}

// aiPlannedNode is the intermediate blueprint node produced either by the LLM planner or the rule-based synthesizer.
type aiPlannedNode struct {
	Key            string                 `json:"key"`
	ComponentName  string                 `json:"component_name"`
	Name           string                 `json:"name"`
	Role           string                 `json:"role"`
	UpstreamKeys   []string               `json:"upstream_keys"`
	DownstreamKeys []string               `json:"downstream_keys"`
	Config         map[string]interface{} `json:"config,omitempty"`
}

// aiBlueprint is the structured architectural plan before compilation into Dual-Shape DSL.
type aiBlueprint struct {
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	CanvasCategory string          `json:"canvas_category"`
	Explanation    string          `json:"explanation"`
	Prologue       string          `json:"prologue,omitempty"`
	Nodes          []aiPlannedNode `json:"nodes"`
}

// AIPlannerOverride can be set by unit tests to deterministic mock LLM output or force fallback.
var AIPlannerOverride func(ctx context.Context, userID, tenantID string, req *GenerateAgentByAIRequest) (*aiBlueprint, error)

// GenerateAgentByAI synthesizes a complete, executable RAGFlow Agent Workflow or Ingestion Pipeline DSL
// from a natural-language user prompt, and optionally creates the canvas directly when AutoCreate is true.
func (s *AgentService) GenerateAgentByAI(
	ctx context.Context,
	userID string,
	req *GenerateAgentByAIRequest,
) (*GenerateAgentByAIResponse, common.ErrorCode, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, common.CodeAuthenticationError, errors.New("unauthenticated user")
	}
	if req == nil {
		return nil, common.CodeArgumentError, errors.New("generate agent by AI: nil request")
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		return nil, common.CodeArgumentError, errors.New("prompt is required")
	}

	tenantID := s.resolveEffectiveTenantID(ctx, userID)
	resolvedLLMID := s.resolveEffectiveChatModelID(ctx, userID, tenantID, strings.TrimSpace(req.LLMID))

	var (
		blueprint    *aiBlueprint
		usedFallback bool
	)

	if AIPlannerOverride != nil {
		bp, err := AIPlannerOverride(ctx, userID, tenantID, req)
		if err == nil && bp != nil && len(bp.Nodes) > 0 {
			blueprint = bp
		}
	} else {
		bp, err := s.planArchitectureWithLLM(ctx, userID, tenantID, resolvedLLMID, req)
		if err == nil && bp != nil && len(bp.Nodes) > 0 {
			blueprint = bp
		}
	}

	if blueprint == nil || len(blueprint.Nodes) == 0 {
		blueprint = buildRuleBasedBlueprint(prompt, req.CanvasCategory)
		usedFallback = true
	}

	// Normalize category if explicitly requested by caller.
	requestedCat := strings.TrimSpace(strings.ToLower(req.CanvasCategory))
	if requestedCat == "agent_canvas" || requestedCat == "dataflow_canvas" {
		if blueprint.CanvasCategory != requestedCat {
			// If LLM returned wrong category, rebuild or coerce.
			blueprint = buildRuleBasedBlueprint(prompt, requestedCat)
			usedFallback = true
		}
	}
	if blueprint.CanvasCategory != "dataflow_canvas" {
		blueprint.CanvasCategory = "agent_canvas"
	}

	if customTitle := strings.TrimSpace(req.Title); customTitle != "" {
		blueprint.Title = customTitle
	}
	if strings.TrimSpace(blueprint.Title) == "" {
		blueprint.Title = deriveTitleFromPrompt(prompt, blueprint.CanvasCategory)
	}
	if strings.TrimSpace(blueprint.Description) == "" {
		blueprint.Description = prompt
	}

	dsl, nodePlan := compileBlueprintToDualShapeDSL(blueprint, resolvedLLMID, req.DatasetIDs)

	// Normalize and run strict Go component parameter & dynamic entry validators.
	dsl = dslpkg.NormalizeForCanvas(dsl)
	if err := component.ValidateIntegerParameters(dsl); err != nil {
		// Defensive fallback to guaranteed-valid rule-based blueprint if LLM produced invalid integer params.
		blueprint = buildRuleBasedBlueprint(prompt, blueprint.CanvasCategory)
		usedFallback = true
		if customTitle := strings.TrimSpace(req.Title); customTitle != "" {
			blueprint.Title = customTitle
		}
		dsl, nodePlan = compileBlueprintToDualShapeDSL(blueprint, resolvedLLMID, req.DatasetIDs)
		dsl = dslpkg.NormalizeForCanvas(dsl)
		if err2 := component.ValidateIntegerParameters(dsl); err2 != nil {
			return nil, common.CodeArgumentError, fmt.Errorf("validate generated DSL parameters: %w", err2)
		}
	}
	if err := component.ValidateDraftDynamicEntries(dsl); err != nil {
		blueprint = buildRuleBasedBlueprint(prompt, blueprint.CanvasCategory)
		usedFallback = true
		if customTitle := strings.TrimSpace(req.Title); customTitle != "" {
			blueprint.Title = customTitle
		}
		dsl, nodePlan = compileBlueprintToDualShapeDSL(blueprint, resolvedLLMID, req.DatasetIDs)
		dsl = dslpkg.NormalizeForCanvas(dsl)
		if err2 := component.ValidateDraftDynamicEntries(dsl); err2 != nil {
			return nil, common.CodeArgumentError, fmt.Errorf("validate generated DSL dynamic entries: %w", err2)
		}
	}

	resp := &GenerateAgentByAIResponse{
		Title:          blueprint.Title,
		Description:    blueprint.Description,
		CanvasCategory: blueprint.CanvasCategory,
		Explanation:    blueprint.Explanation,
		NodePlan:       nodePlan,
		DSL:            entity.JSONMap(dsl),
		UsedFallback:   usedFallback,
	}

	if req.AutoCreate {
		titleCopy := resp.Title
		descCopy := resp.Description
		created, code, err := s.CreateAgent(ctx, userID, &CreateAgentRequest{
			Title:          &titleCopy,
			Description:    &descCopy,
			Permission:     "me",
			CanvasCategory: resp.CanvasCategory,
			DSL:            resp.DSL,
		})
		if err != nil {
			return nil, code, err
		}
		resp.Agent = created
		if created != nil && created.Title != nil {
			resp.Title = *created.Title
		}
	}

	return resp, common.CodeSuccess, nil
}

func (s *AgentService) resolveEffectiveTenantID(ctx context.Context, userID string) string {
	if dao.DB != nil {
		if tid, err := prepareAgentCreation(ctx, userID); err == nil && strings.TrimSpace(tid) != "" {
			return tid
		}
	}
	return userID
}

func (s *AgentService) resolveEffectiveChatModelID(ctx context.Context, userID, tenantID, requestedLLMID string) string {
	if requestedLLMID != "" {
		return requestedLLMID
	}
	if dao.DB != nil {
		info, err := NewModelFactory().ResolveDefaultInfo(ctx, ModelAccess{UserID: userID, TenantID: tenantID}, entity.ModelTypeChat)
		if err == nil && info != nil {
			if strings.TrimSpace(info.ID) != "" {
				return strings.TrimSpace(info.ID)
			}
			if strings.TrimSpace(info.Name) != "" {
				return strings.TrimSpace(info.Name)
			}
		}
	}
	return "gpt-4o___OpenAI-API@OpenAI-API-Compatible"
}

func (s *AgentService) planArchitectureWithLLM(
	ctx context.Context,
	userID, tenantID, resolvedLLMID string,
	req *GenerateAgentByAIRequest,
) (*aiBlueprint, error) {
	if dao.DB == nil {
		return nil, errors.New("database not initialized")
	}
	factory := NewModelFactory()
	access := ModelAccess{UserID: userID, TenantID: tenantID}

	var (
		chatModel *modelModule.ChatModel
		err       error
	)
	if strings.TrimSpace(req.LLMID) != "" {
		chatModel, err = factory.NewChatModel(ctx, access, strings.TrimSpace(req.LLMID))
	} else {
		chatModel, err = factory.NewDefaultChatModel(ctx, access)
	}
	if err != nil || chatModel == nil {
		return nil, fmt.Errorf("resolve chat model: %w", err)
	}

	sysPrompt := buildAIArchitectSystemPrompt(req.CanvasCategory)
	userMsg := fmt.Sprintf("User Requirement: %s\nPreferred Category: %s\nAvailable Datasets Count: %d\nReturn ONLY valid JSON matching the schema.",
		req.Prompt, req.CanvasCategory, len(req.DatasetIDs))

	temp := 0.2
	maxTokens := 2048
	messages := []modelModule.Message{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: userMsg},
	}
	chatResp, err := chatModel.ChatWithMessages(ctx, messages, &modelModule.ChatConfig{
		Temperature: &temp,
		MaxTokens:   &maxTokens,
	}, nil)
	if err != nil || chatResp == nil || chatResp.Answer == nil {
		return nil, fmt.Errorf("chat completion failed: %w", err)
	}

	rawJSON := extractJSONObject(*chatResp.Answer)
	if rawJSON == "" {
		return nil, errors.New("empty JSON response from LLM")
	}

	var bp aiBlueprint
	if err := json.Unmarshal([]byte(rawJSON), &bp); err != nil {
		return nil, fmt.Errorf("unmarshal blueprint JSON: %w", err)
	}
	if !isValidBlueprint(&bp) {
		return nil, errors.New("LLM blueprint failed topology validation")
	}
	return &bp, nil
}

func buildAIArchitectSystemPrompt(preferredCategory string) string {
	return `You are the RAGFlow AI Agent & Dataflow Architect. Your task is to design an executable RAGFlow workflow from a user's requirement.
Respond ONLY with a valid JSON object matching this exact schema (no markdown fences, no extra commentary):
{
  "title": "Concise Vietnamese or English workflow title",
  "description": "Rich Vietnamese use-case description explaining mechanism, business scenario, and ideal inputs",
  "canvas_category": "agent_canvas" | "dataflow_canvas",
  "explanation": "Detailed Vietnamese explanation of why each node was selected and how data flows across the graph",
  "prologue": "Greeting message in Vietnamese (for agent_canvas)",
  "nodes": [
    {
      "key": "unique_short_key",
      "component_name": "Begin | Retrieval | Categorize | Agent | ExeSQL | CodeExec | DocGenerator | Email | TavilySearch | DuckDuckGo | Wikipedia | Message | File | Parser | TokenChunker | TitleChunker | Extractor | Tokenizer",
      "name": "Human-readable node title",
      "role": "Vietnamese explanation of this node's specific responsibility in the workflow",
      "upstream_keys": ["parent_key"],
      "downstream_keys": ["child_key"],
      "config": {
        "sys_prompt": "Custom system prompt for Agent node",
        "tools": ["TavilySearch", "DuckDuckGo", "Wikipedia", "Retrieval"],
        "categories": [
          {"name": "Category Name", "description": "When to route here", "examples": ["example 1"], "target_key": "child_key"}
        ]
      }
    }
  ]
}
Rules:
1. For "agent_canvas": First node MUST have key="begin" and component_name="Begin". Terminal node(s) MUST have component_name="Message".
2. For "dataflow_canvas": First node MUST have key="File" and component_name="File", followed by "Parser", then either "TokenChunker" or "TitleChunker", optional "Extractor", and ending with "Tokenizer".
3. Preferred category requested by user: ` + preferredCategory
}

func extractJSONObject(raw string) string {
	trimmed := strings.TrimSpace(raw)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start < 0 || end <= start {
		return ""
	}
	return trimmed[start : end+1]
}

func isValidBlueprint(bp *aiBlueprint) bool {
	if bp == nil || len(bp.Nodes) < 2 {
		return false
	}
	allowedComponents := map[string]bool{
		"Begin": true, "Retrieval": true, "Categorize": true, "Agent": true,
		"ExeSQL": true, "CodeExec": true, "DocGenerator": true, "Email": true,
		"TavilySearch": true, "DuckDuckGo": true, "Wikipedia": true, "Message": true,
		"File": true, "Parser": true, "TokenChunker": true, "TitleChunker": true,
		"Extractor": true, "Tokenizer": true,
	}
	keySet := make(map[string]bool, len(bp.Nodes))
	hasEntry := false
	hasTerminal := false
	for _, n := range bp.Nodes {
		if n.Key == "" || !allowedComponents[n.ComponentName] {
			return false
		}
		keySet[n.Key] = true
		if n.ComponentName == "Begin" || n.ComponentName == "File" {
			hasEntry = true
		}
		if n.ComponentName == "Message" || n.ComponentName == "Tokenizer" {
			hasTerminal = true
		}
	}
	return hasEntry && hasTerminal
}

// buildRuleBasedBlueprint deterministically synthesizes an optimal, domain-tailored RAGFlow architecture
// based on intent analysis of the user's prompt.
func buildRuleBasedBlueprint(prompt, requestedCategory string) *aiBlueprint {
	lower := strings.ToLower(prompt)
	cat := strings.ToLower(strings.TrimSpace(requestedCategory))

	// Detect if user wants a Dataflow / Ingestion Pipeline
	isDataflowIntent := cat == "dataflow_canvas" ||
		(cat != "agent_canvas" && (containsAny(lower,
			"pipeline", "dataflow", "ingest", "nạp dữ liệu", "bóc tách tài liệu",
			"chunk", "băm nhỏ", "index tài liệu", "xử lý pdf", "etl", "parser", "tokenizer")))

	if isDataflowIntent {
		isHierarchical := containsAny(lower,
			"hợp đồng", "pháp lý", "luật", "sổ tay", "sop", "quy chế", "tiêu đề",
			"chương", "điều khoản", "title", "hierarchy", "markdown", "docx")
		if isHierarchical {
			return &aiBlueprint{
				Title:          "Pipeline Bóc tách Tài liệu Phân cấp (SOP / Hợp đồng / Pháp lý)",
				Description:    "Luồng nạp dữ liệu chuẩn hóa dành cho tài liệu có cấu trúc chương mục: bóc tách đa định dạng (Parser) -> cắt đoạn theo cấp độ tiêu đề H1-H3 (TitleChunker) -> làm giàu ngữ cảnh bằng AI tóm tắt & bộ câu hỏi giả định (Extractor) -> lập chỉ mục tìm kiếm lai Vector + Full-text (Tokenizer).",
				CanvasCategory: "dataflow_canvas",
				Explanation:    "AI Brain nhận diện nhu cầu xử lý tài liệu có cấu trúc phân cấp (Hợp đồng, Quy chế, Sổ tay SOP). Hệ thống thiết kế Pipeline gồm 5 chặng: (1) File tiếp nhận tài liệu đầu vào -> (2) Parser bóc tách văn bản/bảng biểu -> (3) TitleChunker giữ nguyên vẹn cấu trúc Điều/Khoản/Mục -> (4) Extractor dùng LLM sinh Tóm tắt, Từ khóa & Câu hỏi giả định (HyDE) -> (5) Tokenizer đánh chỉ mục Hybrid Search vào Dataset.",
				Nodes: []aiPlannedNode{
					{
						Key: "File", ComponentName: "File", Name: "File",
						Role:           "Điểm tiếp nhận file tài liệu gốc từ Dataset hoặc người dùng tải lên",
						UpstreamKeys:   []string{},
						DownstreamKeys: []string{"parser_1"},
					},
					{
						Key: "parser_1", ComponentName: "Parser", Name: "Bộ bóc tách Đa định dạng",
						Role:           "Chuyển đổi PDF (DeepDOC), Word, Markdown, HTML thành cấu trúc Markdown chuẩn",
						UpstreamKeys:   []string{"File"},
						DownstreamKeys: []string{"title_chunker_1"},
					},
					{
						Key: "title_chunker_1", ComponentName: "TitleChunker", Name: "Cắt đoạn theo Tiêu đề (H1-H3)",
						Role:           "Chia nhỏ tài liệu theo cấu trúc cây tiêu đề Chương/Mục/Điều khoản mà không làm đứt gãy ngữ cảnh",
						UpstreamKeys:   []string{"parser_1"},
						DownstreamKeys: []string{"extractor_1"},
					},
					{
						Key: "extractor_1", ComponentName: "Extractor", Name: "Trích xuất Ngữ cảnh AI (HyDE)",
						Role:           "Dùng LLM trích xuất Bản tóm tắt (Summary), Từ khóa (Keywords) và Câu hỏi giả định (Questions) cho từng đoạn",
						UpstreamKeys:   []string{"title_chunker_1"},
						DownstreamKeys: []string{"tokenizer_1"},
					},
					{
						Key: "tokenizer_1", ComponentName: "Tokenizer", Name: "Lập chỉ mục Hybrid (Vector + BM25)",
						Role:           "Mã hóa Vector Embedding kết hợp tách từ Full-text để ghi vào Dataset phục vụ truy hồi",
						UpstreamKeys:   []string{"extractor_1"},
						DownstreamKeys: []string{},
					},
				},
			}
		}

		return &aiBlueprint{
			Title:          "Pipeline Nạp & Làm giàu Tri thức Đa năng (Advanced Ingestion)",
			Description:    "Luồng tiền xử lý và nạp dữ liệu nâng cao vào Dataset: bóc tách đa định dạng (PDF DeepDOC, Excel, Ảnh OCR, Audio) -> cắt đoạn theo độ dài Token tối ưu có gối đầu (TokenChunker) -> làm giàu siêu dữ liệu bằng AI (Extractor) -> lập chỉ mục tìm kiếm lai (Tokenizer).",
			CanvasCategory: "dataflow_canvas",
			Explanation:    "AI Brain thiết kế Dataflow Pipeline 5 bước chuẩn công nghiệp để làm giàu Dataset: (1) File nhận tài liệu -> (2) Parser bóc tách bố cục tài liệu phức tạp -> (3) TokenChunker cắt đoạn 512 tokens (gối đầu 10%) -> (4) Extractor dùng LLM sinh Tóm tắt, Từ khóa & Câu hỏi giả định -> (5) Tokenizer đánh chỉ mục Hybrid (Embedding + Full-text).",
			Nodes: []aiPlannedNode{
				{
					Key: "File", ComponentName: "File", Name: "File",
					Role:           "Tiếp nhận tài liệu đầu vào từ Dataset",
					UpstreamKeys:   []string{},
					DownstreamKeys: []string{"parser_1"},
				},
				{
					Key: "parser_1", ComponentName: "Parser", Name: "Bóc tách Tài liệu Đa phương thức",
					Role:           "Nhận diện bố cục PDF (DeepDOC), bảng tính Excel, hình ảnh OCR và chuyển thành văn bản sạch",
					UpstreamKeys:   []string{"File"},
					DownstreamKeys: []string{"token_chunker_1"},
				},
				{
					Key: "token_chunker_1", ComponentName: "TokenChunker", Name: "Băm nhỏ theo Token (512 tokens)",
					Role:           "Chia văn bản thành các đoạn 512 tokens với tỷ lệ gối đầu 10% giúp bảo toàn mạch thông tin",
					UpstreamKeys:   []string{"parser_1"},
					DownstreamKeys: []string{"extractor_1"},
				},
				{
					Key: "extractor_1", ComponentName: "Extractor", Name: "Làm giàu Ngữ cảnh bằng AI",
					Role:           "Tự động sinh Summary, Keywords và Hypothetical Questions giúp tăng độ chính xác khi truy hồi RAG",
					UpstreamKeys:   []string{"token_chunker_1"},
					DownstreamKeys: []string{"tokenizer_1"},
				},
				{
					Key: "tokenizer_1", ComponentName: "Tokenizer", Name: "Lập chỉ mục Hybrid Search",
					Role:           "Vector hóa và đánh chỉ mục từ khóa vào cơ sở dữ liệu tri thức",
					UpstreamKeys:   []string{"extractor_1"},
					DownstreamKeys: []string{},
				},
			},
		}
	}

	// AGENT CANVAS ARCHITECTURES
	// Pattern 1: Customer Support / Classification / Feedback / Routing
	if containsAny(lower, "cskh", "phân loại", "khiếu nại", "phản hồi", "hỗ trợ khách hàng", "routing", "điều phối", "categorize", "ticket", "tư vấn") {
		return &aiBlueprint{
			Title:          "Trợ lý AI Phân loại & Điều phối CSKH Thông minh",
			Description:    "Hệ thống tiếp nhận yêu cầu khách hàng, tự động phân loại ý định (Hỏi đáp sản phẩm/kỹ thuật vs Khiếu nại/Bảo hành) bằng Categorize Node, kết hợp tra cứu Kho tri thức nội bộ (Retrieval) và phản hồi chuyên biệt cho từng nhóm.",
			CanvasCategory: "agent_canvas",
			Explanation:    "AI Brain thiết kế kiến trúc phân nhánh thông minh: (1) Begin đón yêu cầu khách hàng -> (2) Categorize dùng LLM phân loại chính xác thành 2 luồng: 'Tra cứu Chính sách & Kỹ thuật' và 'Xử lý Khiếu nại & Hỗ trợ khẩn' -> (3) Nhánh kỹ thuật đi qua Retrieval để lấy tài liệu chuẩn từ Dataset trước khi đưa vào Agent trả lời; Nhánh khiếu nại đi tới Agent chuyên trách soạn phản hồi đồng cảm & hướng dẫn quy trình xử lý -> (4) Message xuất câu trả lời hoàn chỉnh.",
			Prologue:       "Xin chào! Tôi là Trợ lý AI Chăm sóc Khách hàng. Bạn cần tra cứu thông tin sản phẩm, chính sách bảo hành hay phản ánh dịch vụ ạ?",
			Nodes: []aiPlannedNode{
				{
					Key: "begin", ComponentName: "Begin", Name: "Begin",
					Role:           "Khởi tạo phiên hội thoại và tiếp nhận câu hỏi của khách hàng",
					UpstreamKeys:   []string{},
					DownstreamKeys: []string{"categorize_1"},
				},
				{
					Key: "categorize_1", ComponentName: "Categorize", Name: "Phân loại Ý định Khách hàng",
					Role:           "Phân tích ngữ nghĩa câu hỏi để rẽ nhánh sang luồng Tra cứu Tri thức hoặc luồng Xử lý Khiếu nại",
					UpstreamKeys:   []string{"begin"},
					DownstreamKeys: []string{"retrieval_1", "agent_complaint"},
					Config: map[string]interface{}{
						"categories": []map[string]interface{}{
							{
								"name":        "technical_or_policy_inquiry",
								"description": "Khách hàng hỏi về thông tin sản phẩm, hướng dẫn sử dụng, bảng giá hoặc chính sách bảo hành",
								"examples":    []string{"Chính sách đổi trả trong bao lâu?", "Hướng dẫn cài đặt tính năng này giúp tôi"},
								"target_key":  "retrieval_1",
							},
							{
								"name":        "complaint_or_urgent_support",
								"description": "Khách hàng phàn nàn, khiếu nại lỗi dịch vụ, yêu cầu hoàn tiền hoặc cần hỗ trợ khẩn cấp",
								"examples":    []string{"Đơn hàng của tôi bị giao sai và hỏng!", "Tại sao tài khoản bị trừ tiền 2 lần?"},
								"target_key":  "agent_complaint",
							},
						},
					},
				},
				{
					Key: "retrieval_1", ComponentName: "Retrieval", Name: "Tra cứu Kho Tri thức Sản phẩm",
					Role:           "Truy hồi các đoạn tài liệu hướng dẫn và chính sách liên quan nhất từ Dataset",
					UpstreamKeys:   []string{"categorize_1"},
					DownstreamKeys: []string{"agent_rag"},
				},
				{
					Key: "agent_rag", ComponentName: "Agent", Name: "Chuyên viên Tư vấn Tri thức (RAG)",
					Role:           "Tổng hợp câu trả lời chính xác dựa trên trích dẫn tài liệu nội bộ từ bước Retrieval",
					UpstreamKeys:   []string{"retrieval_1"},
					DownstreamKeys: []string{"message_1"},
					Config: map[string]interface{}{
						"sys_prompt": "Bạn là Chuyên viên Tư vấn Khách hàng chuyên nghiệp. Hãy dựa trên dữ liệu truy hồi từ Kho tri thức:\n{retrieval_1@formalized_content}\n\nĐể giải đáp chính xác, rõ ràng và lịch sự câu hỏi của khách hàng: {sys.query}. Nếu không có thông tin trong tài liệu, hãy nói rõ và hướng dẫn khách để lại thông tin liên hệ.",
					},
				},
				{
					Key: "agent_complaint", ComponentName: "Agent", Name: "Chuyên viên Xử lý Khiếu nại",
					Role:           "Phân tích mức độ nghiêm trọng, soạn thông điệp xin lỗi chân thành và đề xuất phương án giải quyết ngay",
					UpstreamKeys:   []string{"categorize_1"},
					DownstreamKeys: []string{"message_1"},
					Config: map[string]interface{}{
						"sys_prompt": "Bạn là Trưởng bộ phận Chăm sóc Khách hàng. Khách hàng đang gửi phản ánh/khiếu nại: {sys.query}.\nHãy: (1) Thể hiện sự đồng cảm và xin lỗi chân thành, (2) Tóm tắt lại vấn đề khách đang gặp phải, (3) Đưa ra các bước xử lý ưu tiên và thông tin cần khách cung cấp thêm (mã đơn hàng, SĐT) để bộ phận kỹ thuật xử lý ngay lập tức.",
					},
				},
				{
					Key: "message_1", ComponentName: "Message", Name: "Phản hồi Khách hàng",
					Role:           "Hiển thị nội dung phản hồi từ nhánh tương ứng ra giao diện hội thoại",
					UpstreamKeys:   []string{"agent_rag", "agent_complaint"},
					DownstreamKeys: []string{},
				},
			},
		}
	}

	// Pattern 2: Text-to-SQL / Database BI / Data Query
	if containsAny(lower, "sql", "database", "cơ sở dữ liệu", "truy vấn db", "báo cáo doanh thu", "bi", "mysql", "postgres", "clickhouse", "bảng dữ liệu") {
		return &aiBlueprint{
			Title:          "Chuyên gia Phân tích Dữ liệu Text-to-SQL & Báo cáo BI",
			Description:    "Luồng chuyển đổi câu hỏi ngôn ngữ tự nhiên thành câu lệnh SQL chuẩn xác dựa trên Schema lấy từ Kho tri thức (Retrieval), thực thi trực tiếp trên Database (ExeSQL) và diễn giải kết quả thành báo cáo phân tích trực quan.",
			CanvasCategory: "agent_canvas",
			Explanation:    "AI Brain thiết kế kiến trúc Text-to-SQL an toàn 5 chặng: (1) Begin nhận yêu cầu thống kê/báo cáo -> (2) Retrieval tra cứu tài liệu mô tả cấu trúc bảng (DDL/Schema) và từ điển nghiệp vụ -> (3) Agent (SQL Generator) sinh câu lệnh SELECT tối ưu -> (4) ExeSQL thực thi truy vấn trên cơ sở dữ liệu -> (5) Agent (BI Analyst) phân tích bảng kết quả và trình bày báo cáo -> (6) Message xuất kết quả.",
			Prologue:       "Xin chào! Tôi là Chuyên gia Phân tích Dữ liệu SQL (Text-to-SQL). Hãy mô tả chỉ số hoặc báo cáo dữ liệu bạn muốn truy vấn nhé!",
			Nodes: []aiPlannedNode{
				{
					Key: "begin", ComponentName: "Begin", Name: "Begin",
					Role:           "Nhận câu hỏi phân tích số liệu bằng ngôn ngữ tự nhiên",
					UpstreamKeys:   []string{},
					DownstreamKeys: []string{"retrieval_schema"},
				},
				{
					Key: "retrieval_schema", ComponentName: "Retrieval", Name: "Tra cứu Schema & Từ điển Dữ liệu",
					Role:           "Tìm kiếm cấu trúc bảng (DDL), tên cột và quy tắc tính toán từ Dataset",
					UpstreamKeys:   []string{"begin"},
					DownstreamKeys: []string{"agent_sql_gen"},
				},
				{
					Key: "agent_sql_gen", ComponentName: "Agent", Name: "Kiến trúc sư Sinh lệnh SQL",
					Role:           "Chuyển câu hỏi của người dùng và Schema thành câu lệnh truy vấn SQL SELECT hợp lệ",
					UpstreamKeys:   []string{"retrieval_schema"},
					DownstreamKeys: []string{"exesql_1"},
					Config: map[string]interface{}{
						"sys_prompt": "Bạn là Chuyên gia cơ sở dữ liệu SQL. Dựa vào thông tin Schema sau:\n{retrieval_schema@formalized_content}\n\nHãy viết duy nhất câu lệnh SQL SELECT hợp lệ để trả lời yêu cầu: {sys.query}. Chỉ trả về câu lệnh SQL thuần túy.",
					},
				},
				{
					Key: "exesql_1", ComponentName: "ExeSQL", Name: "Thực thi Truy vấn Database (ExeSQL)",
					Role:           "Chạy câu lệnh SQL trên cơ sở dữ liệu được cấu hình và thu về bảng kết quả JSON/Markdown",
					UpstreamKeys:   []string{"agent_sql_gen"},
					DownstreamKeys: []string{"agent_bi_analyst"},
				},
				{
					Key: "agent_bi_analyst", ComponentName: "Agent", Name: "Chuyên gia Phân tích Báo cáo BI",
					Role:           "Đọc kết quả truy vấn từ ExeSQL, tổng hợp bảng số liệu và rút ra nhận định quản trị",
					UpstreamKeys:   []string{"exesql_1"},
					DownstreamKeys: []string{"message_1"},
					Config: map[string]interface{}{
						"sys_prompt": "Bạn là Chuyên gia Phân tích Dữ liệu (BI Analyst). Dựa trên yêu cầu gốc: {sys.query}\nVà kết quả thực thi SQL: {exesql_1@formalized_content}\n\nHãy trình bày bảng số liệu rõ ràng và đưa ra các nhận xét, xu hướng quan trọng bằng tiếng Việt.",
					},
				},
				{
					Key: "message_1", ComponentName: "Message", Name: "Xuất Báo cáo Phân tích",
					Role:           "Hiển thị báo cáo BI hoàn chỉnh cho người dùng",
					UpstreamKeys:   []string{"agent_bi_analyst"},
					DownstreamKeys: []string{},
				},
			},
		}
	}

	// Pattern 3: Deep Research / Web Search + Internal RAG Hybrid
	if containsAny(lower, "web", "internet", "tìm kiếm mạng", "nghiên cứu", "research", "tin tức", "thị trường", "đối thủ", "tavily", "duckduckgo", "wikipedia", "hybrid") {
		return &aiBlueprint{
			Title:          "Trợ lý Nghiên cứu Chuyên sâu Đa nguồn (Internal RAG + Live Web Search)",
			Description:    "Kết hợp đồng thời tra cứu Kho tri thức nội bộ (Retrieval) và tìm kiếm Internet thời gian thực (TavilySearch, DuckDuckGo, Wikipedia), sau đó tổng hợp thành báo cáo phân tích đa chiều có trích dẫn nguồn.",
			CanvasCategory: "agent_canvas",
			Explanation:    "AI Brain thiết kế kiến trúc Hybrid RAG + Web Search: (1) Begin tiếp nhận chủ đề nghiên cứu -> (2) Retrieval tìm kiếm tài liệu nội bộ trong Dataset -> (3) Agent Nghiên cứu được trang bị bộ công cụ tìm kiếm Web (TavilySearch, DuckDuckGo, Wikipedia) để đối chiếu thông tin mới nhất trên Internet với tài liệu nội bộ -> (4) Message xuất báo cáo tổng hợp chi tiết.",
			Prologue:       "Xin chào! Tôi là Trợ lý Nghiên cứu Đa nguồn (kết hợp Kho tri thức nội bộ và Tìm kiếm Internet thời gian thực). Bạn muốn nghiên cứu chủ đề gì hôm nay?",
			Nodes: []aiPlannedNode{
				{
					Key: "begin", ComponentName: "Begin", Name: "Begin",
					Role:           "Tiếp nhận chủ đề hoặc câu hỏi cần nghiên cứu chuyên sâu",
					UpstreamKeys:   []string{},
					DownstreamKeys: []string{"retrieval_1"},
				},
				{
					Key: "retrieval_1", ComponentName: "Retrieval", Name: "Tra cứu Tri thức Nội bộ (Dataset)",
					Role:           "Truy xuất các báo cáo, tài liệu sẵn có trong kho dữ liệu của tổ chức",
					UpstreamKeys:   []string{"begin"},
					DownstreamKeys: []string{"agent_researcher"},
				},
				{
					Key: "agent_researcher", ComponentName: "Agent", Name: "Chuyên gia Nghiên cứu & Tổng hợp (Web + RAG)",
					Role:           "Kết hợp dữ liệu nội bộ với công cụ tìm kiếm Web (Tavily/DuckDuckGo/Wikipedia) để phân tích và lập báo cáo",
					UpstreamKeys:   []string{"retrieval_1"},
					DownstreamKeys: []string{"message_1"},
					Config: map[string]interface{}{
						"sys_prompt": "Bạn là Chuyên gia Nghiên cứu Cấp cao. Hãy kết hợp:\n1. Dữ liệu nội bộ từ Kho tri thức:\n{retrieval_1@formalized_content}\n2. Thông tin cập nhật mới nhất từ các công cụ tìm kiếm Web được gắn kèm.\n\nĐể phân tích toàn diện yêu cầu: {sys.query}. Hãy trình bày có cấu trúc rõ ràng, nêu bật số liệu và ghi rõ nguồn trích dẫn.",
						"tools":      []string{"TavilySearch", "DuckDuckGo", "Wikipedia"},
					},
				},
				{
					Key: "message_1", ComponentName: "Message", Name: "Báo cáo Nghiên cứu",
					Role:           "Hiển thị báo cáo tổng hợp đa nguồn tới người dùng",
					UpstreamKeys:   []string{"agent_researcher"},
					DownstreamKeys: []string{},
				},
			},
		}
	}

	// Default Pattern 4: Enterprise RAG Knowledge Assistant with Query Refinement & Citation
	return &aiBlueprint{
		Title:          "Trợ lý Hỏi đáp Tri thức Doanh nghiệp Chuẩn xác (Enterprise RAG)",
		Description:    "Quy trình RAG tiêu chuẩn doanh nghiệp: Tiếp nhận câu hỏi -> Truy hồi ngữ nghĩa kết hợp từ khóa trên Kho tri thức (Retrieval) -> Tổng hợp câu trả lời trung thực kèm trích dẫn nguồn bằng LLM (Agent) -> Xuất phản hồi.",
		CanvasCategory: "agent_canvas",
		Explanation:    "AI Brain thiết kế luồng Agent RAG chuẩn hóa gồm 4 Node cốt lõi: (1) Begin khởi tạo hội thoại -> (2) Retrieval thực hiện Hybrid Search (Vector + Full-text) trên các Dataset được gắn -> (3) Agent đóng vai trò Chuyên gia Tri thức, phân tích ngữ cảnh truy hồi được để trả lời câu hỏi chính xác, chống ảo giác (Anti-Hallucination) -> (4) Message trả kết quả về giao diện chat.",
		Prologue:       "Xin chào! Tôi là Trợ lý Tri thức AI được thiết kế riêng theo yêu cầu của bạn. Hãy đặt câu hỏi để tôi tra cứu và giải đáp nhé!",
		Nodes: []aiPlannedNode{
			{
				Key: "begin", ComponentName: "Begin", Name: "Begin",
				Role:           "Điểm bắt đầu hội thoại và nhận câu hỏi {sys.query} từ người dùng",
				UpstreamKeys:   []string{},
				DownstreamKeys: []string{"retrieval_1"},
			},
			{
				Key: "retrieval_1", ComponentName: "Retrieval", Name: "Truy hồi Kho Tri thức (Hybrid Retrieval)",
				Role:           "Tìm kiếm các đoạn tài liệu liên quan nhất từ Dataset bằng kết hợp Vector Similarity và Từ khóa",
				UpstreamKeys:   []string{"begin"},
				DownstreamKeys: []string{"agent_1"},
			},
			{
				Key: "agent_1", ComponentName: "Agent", Name: "Chuyên gia Tổng hợp & Giải đáp (LLM)",
				Role:           "Đọc ngữ cảnh từ Retrieval và biên soạn câu trả lời chính xác, bám sát yêu cầu nghiệp vụ",
				UpstreamKeys:   []string{"retrieval_1"},
				DownstreamKeys: []string{"message_1"},
				Config: map[string]interface{}{
					"sys_prompt": fmt.Sprintf("Bạn là Trợ lý AI Chuyên trách được xây dựng cho mục tiêu: %s.\n\nHãy dựa hoàn toàn vào ngữ cảnh tri thức được truy hồi dưới đây:\n{retrieval_1@formalized_content}\n\nĐể trả lời câu hỏi của người dùng: {sys.query} một cách chính xác, đầy đủ và có cấu trúc rõ ràng bằng tiếng Việt.", prompt),
				},
			},
			{
				Key: "message_1", ComponentName: "Message", Name: "Xuất Câu trả lời",
				Role:           "Hiển thị câu trả lời hoàn chỉnh của Agent ra cửa sổ hội thoại",
				UpstreamKeys:   []string{"agent_1"},
				DownstreamKeys: []string{},
			},
		},
	}
}

func containsAny(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

func deriveTitleFromPrompt(prompt, canvasCategory string) string {
	clean := strings.TrimSpace(prompt)
	runes := []rune(clean)
	if len(runes) > 48 {
		clean = string(runes[:48]) + "..."
	}
	if canvasCategory == "dataflow_canvas" {
		return "AI Pipeline: " + clean
	}
	return "AI Agent: " + clean
}

// compileBlueprintToDualShapeDSL compiles an aiBlueprint into RAGFlow's exact Dual-Shape DSL
// (`dsl.graph.nodes` + `dsl.graph.edges` for ReactFlow AND `dsl.components` for Go runtime),
// computing topological multi-layer coordinates and binding `llm_id` and `dataset_ids`.
func compileBlueprintToDualShapeDSL(
	bp *aiBlueprint,
	resolvedLLMID string,
	datasetIDs []string,
) (map[string]interface{}, []AINodePlanItem) {
	if datasetIDs == nil {
		datasetIDs = []string{}
	}

	// Step 1: Assign canonical RAGFlow node IDs (e.g., "begin", "File", "Retrieval:Node_1", "Agent:Node_2")
	keyToID := make(map[string]string, len(bp.Nodes))
	for i, n := range bp.Nodes {
		switch n.ComponentName {
		case "Begin":
			keyToID[n.Key] = "begin"
		case "File":
			keyToID[n.Key] = "File"
		default:
			keyToID[n.Key] = fmt.Sprintf("%s:AINode_%d", n.ComponentName, i+1)
		}
	}

	// Step 2: Reconcile bidirectional upstream/downstream links using canonical IDs
	upMap := make(map[string][]string, len(bp.Nodes))
	downMap := make(map[string][]string, len(bp.Nodes))
	for _, n := range bp.Nodes {
		srcID := keyToID[n.Key]
		for _, dKey := range n.DownstreamKeys {
			if dstID, ok := keyToID[dKey]; ok && dstID != srcID {
				downMap[srcID] = appendUniqueString(downMap[srcID], dstID)
				upMap[dstID] = appendUniqueString(upMap[dstID], srcID)
			}
		}
		for _, uKey := range n.UpstreamKeys {
			if parID, ok := keyToID[uKey]; ok && parID != srcID {
				upMap[srcID] = appendUniqueString(upMap[srcID], parID)
				downMap[parID] = appendUniqueString(downMap[parID], srcID)
			}
		}
	}

	// Ensure a connected chain if the LLM omitted explicit links
	for i := 0; i < len(bp.Nodes)-1; i++ {
		curID := keyToID[bp.Nodes[i].Key]
		nextID := keyToID[bp.Nodes[i+1].Key]
		if len(downMap[curID]) == 0 && len(upMap[nextID]) == 0 {
			downMap[curID] = appendUniqueString(downMap[curID], nextID)
			upMap[nextID] = appendUniqueString(upMap[nextID], curID)
		}
	}

	// Step 3: Compute topological layer for each node for clean left-to-right visual layout
	layerByID := computeTopologicalLayers(bp.Nodes, keyToID, upMap)
	nodesInLayer := make(map[int][]string)
	for _, n := range bp.Nodes {
		id := keyToID[n.Key]
		l := layerByID[id]
		nodesInLayer[l] = append(nodesInLayer[l], id)
	}

	coordsByID := make(map[string]map[string]float64, len(bp.Nodes))
	for layer, ids := range nodesInLayer {
		count := len(ids)
		for idx, id := range ids {
			x := float64(80 + layer*340)
			yOffset := (float64(idx) - float64(count-1)/2.0) * 220.0
			y := 220.0 + yOffset
			coordsByID[id] = map[string]float64{"x": x, "y": y}
		}
	}

	// Step 4: Build Dual-Shape nodes, edges, and components
	graphNodes := make([]interface{}, 0, len(bp.Nodes)+2)
	graphEdges := make([]interface{}, 0, len(bp.Nodes)*2)
	components := make(map[string]interface{}, len(bp.Nodes))
	nodePlan := make([]AINodePlanItem, 0, len(bp.Nodes))

	// Map from Categorize source node ID -> target node ID -> category UUID (for ReactFlow sourceHandle)
	categorizeEdgeHandles := make(map[string]map[string]string)

	for i, n := range bp.Nodes {
		nodeID := keyToID[n.Key]
		upIDs := upMap[nodeID]
		if upIDs == nil {
			upIDs = []string{}
		}
		downIDs := downMap[nodeID]
		if downIDs == nil {
			downIDs = []string{}
		}
		pos := coordsByID[nodeID]

		nodeType := reactFlowNodeType(n.ComponentName, bp.CanvasCategory)
		formMap, paramsMap, catHandles := buildDualShapeNodePayloads(
			n, nodeID, upIDs, downIDs, keyToID, resolvedLLMID, datasetIDs, bp.Prologue,
		)
		if len(catHandles) > 0 {
			categorizeEdgeHandles[nodeID] = catHandles
		}

		graphNode := map[string]interface{}{
			"id":             nodeID,
			"type":           nodeType,
			"position":       pos,
			"sourcePosition": "right",
			"targetPosition": "left",
			"width":          200,
			"height":         100,
			"data": map[string]interface{}{
				"label": n.ComponentName,
				"name":  nonEmptyOrDefault(n.Name, fmt.Sprintf("%s_%d", n.ComponentName, i+1)),
				"form":  formMap,
			},
		}
		graphNodes = append(graphNodes, graphNode)

		components[nodeID] = map[string]interface{}{
			"upstream":   upIDs,
			"downstream": downIDs,
			"obj": map[string]interface{}{
				"component_name": n.ComponentName,
				"params":         paramsMap,
			},
		}

		// When an Agent node uses tools, attach the visual Tool node & edge in graph.nodes/graph.edges
		// (matching web_search_assistant.json where Tool node is visual in graph.nodes).
		if n.ComponentName == "Agent" {
			if toolsSlice, ok := paramsMap["tools"].([]interface{}); ok && len(toolsSlice) > 0 {
				toolNodeID := fmt.Sprintf("Tool:AITool_%d", i+1)
				graphNodes = append(graphNodes, map[string]interface{}{
					"id":             toolNodeID,
					"type":           "toolNode",
					"position":       map[string]float64{"x": pos["x"], "y": pos["y"] + 210},
					"sourcePosition": "right",
					"targetPosition": "left",
					"width":          200,
					"height":         68,
					"data": map[string]interface{}{
						"label": "Tool",
						"name":  "Bộ công cụ AI",
						"form":  map[string]interface{}{},
					},
				})
				graphEdges = append(graphEdges, map[string]interface{}{
					"id":           fmt.Sprintf("edge-%s-%s", nodeID, toolNodeID),
					"source":       nodeID,
					"sourceHandle": "tool",
					"target":       toolNodeID,
					"targetHandle": "end",
				})
			}
		}

		nodePlan = append(nodePlan, AINodePlanItem{
			ID:            nodeID,
			ComponentName: n.ComponentName,
			Name:          nonEmptyOrDefault(n.Name, n.ComponentName),
			Role:          n.Role,
			UpstreamIDs:   upIDs,
			DownstreamIDs: downIDs,
		})
	}

	// Step 5: Build ReactFlow edges for all upstream -> downstream connections
	for _, n := range bp.Nodes {
		srcID := keyToID[n.Key]
		for _, dstID := range downMap[srcID] {
			sourceHandle := "start"
			if handles, ok := categorizeEdgeHandles[srcID]; ok {
				if h, exists := handles[dstID]; exists && h != "" {
					sourceHandle = h
				}
			}
			graphEdges = append(graphEdges, map[string]interface{}{
				"id":           fmt.Sprintf("edge-%s-%s", srcID, dstID),
				"source":       srcID,
				"sourceHandle": sourceHandle,
				"target":       dstID,
				"targetHandle": "end",
			})
		}
	}

	dsl := map[string]interface{}{
		"components": components,
		"graph": map[string]interface{}{
			"nodes": graphNodes,
			"edges": graphEdges,
		},
		"history":   []interface{}{},
		"messages":  []interface{}{},
		"path":      []interface{}{},
		"reference": []interface{}{},
		"globals": map[string]interface{}{
			"sys.query":             "",
			"sys.user_id":           "",
			"sys.conversation_turns": 0,
			"sys.files":             []interface{}{},
		},
		"variables": map[string]interface{}{},
	}

	return dsl, nodePlan
}

func computeTopologicalLayers(
	nodes []aiPlannedNode,
	keyToID map[string]string,
	upMap map[string][]string,
) map[string]int {
	layerByID := make(map[string]int, len(nodes))
	for _, n := range nodes {
		id := keyToID[n.Key]
		if n.ComponentName == "Begin" || n.ComponentName == "File" {
			layerByID[id] = 0
		} else {
			layerByID[id] = 1
		}
	}
	// Bellman-Ford style relaxation up to len(nodes) iterations (handles DAGs cleanly and prevents cycles from looping)
	for iter := 0; iter < len(nodes); iter++ {
		changed := false
		for _, n := range nodes {
			id := keyToID[n.Key]
			for _, uID := range upMap[id] {
				if layerByID[uID]+1 > layerByID[id] {
					layerByID[id] = layerByID[uID] + 1
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	return layerByID
}

func reactFlowNodeType(componentName, canvasCategory string) string {
	switch componentName {
	case "Begin", "File":
		return "beginNode"
	case "Retrieval":
		return "retrievalNode"
	case "Categorize":
		return "categorizeNode"
	case "Agent":
		return "agentNode"
	case "Message":
		return "messageNode"
	case "Parser":
		return "parserNode"
	case "TokenChunker", "TitleChunker":
		return "chunkerNode"
	case "Extractor":
		return "contextNode"
	case "Tokenizer":
		return "tokenizerNode"
	default:
		if canvasCategory == "dataflow_canvas" {
			return "ragNode"
		}
		return "logicNode"
	}
}

func buildDualShapeNodePayloads(
	n aiPlannedNode,
	nodeID string,
	upIDs, downIDs []string,
	keyToID map[string]string,
	resolvedLLMID string,
	datasetIDs []string,
	prologue string,
) (map[string]interface{}, map[string]interface{}, map[string]string) {
	if prologue == "" {
		prologue = "Xin chào! Tôi là Trợ lý AI của bạn. Tôi có thể giúp gì cho bạn hôm nay?"
	}
	dsList := make([]interface{}, len(datasetIDs))
	for i, id := range datasetIDs {
		dsList[i] = id
	}

	switch n.ComponentName {
	case "Begin":
		form := map[string]interface{}{
			"enablePrologue": true,
			"inputs":         map[string]interface{}{},
			"mode":           "conversational",
			"prologue":       prologue,
			"outputs":        map[string]interface{}{},
		}
		params := cloneStringMap(form)
		return form, params, nil

	case "File":
		form := map[string]interface{}{
			"outputs": map[string]interface{}{
				"file": map[string]interface{}{
					"type":  "Object",
					"value": map[string]interface{}{},
				},
			},
		}
		return form, map[string]interface{}{}, nil

	case "Parser":
		return buildCanonicalParserPayloads(resolvedLLMID)

	case "TokenChunker":
		form := map[string]interface{}{
			"chunk_token_size":           512,
			"overlapped_percent":         10,
			"delimiter_mode":             "delimiter",
			"delimiters":                 []interface{}{map[string]interface{}{"value": "\n"}},
			"children_delimiters":        []interface{}{},
			"enable_children_delimiters": false,
			"image_table_context_window": 0,
			"table_context_size":         0,
			"image_context_size":         0,
			"outputs": map[string]interface{}{
				"chunks": map[string]interface{}{
					"type":  "Array<Object>",
					"value": []interface{}{},
				},
			},
		}
		params := map[string]interface{}{
			"chunk_token_size":           512,
			"overlapped_percent":         0.1,
			"delimiter_mode":             "delimiter",
			"delimiters":                 []interface{}{"\n"},
			"children_delimiters":        []interface{}{},
			"enable_children_delimiters": false,
			"image_context_size":         0,
			"table_context_size":         0,
			"outputs": map[string]interface{}{
				"chunks": map[string]interface{}{
					"type":  "Array<Object>",
					"value": []interface{}{},
				},
			},
		}
		return form, params, nil

	case "TitleChunker":
		form := map[string]interface{}{
			"method":                  "hierarchy",
			"hierarchy":               3,
			"include_heading_content": false,
			"rules": []interface{}{
				map[string]interface{}{
					"levels": []interface{}{
						map[string]interface{}{"expression": "^#[^#]"},
						map[string]interface{}{"expression": "^##[^#]"},
						map[string]interface{}{"expression": "^###[^#]"},
					},
				},
			},
			"outputs": map[string]interface{}{
				"chunks": map[string]interface{}{
					"type":  "Array<Object>",
					"value": []interface{}{},
				},
			},
		}
		params := map[string]interface{}{
			"method":                  "hierarchy",
			"hierarchy":               3,
			"include_heading_content": false,
			"levels": []interface{}{
				[]interface{}{"^#[^#]", "^##[^#]", "^###[^#]"},
			},
			"outputs": map[string]interface{}{
				"chunks": map[string]interface{}{
					"type":  "Array<Object>",
					"value": []interface{}{},
				},
			},
		}
		return form, params, nil

	case "Extractor":
		payload := map[string]interface{}{
			"llm_id":                  resolvedLLMID,
			"temperature":             0.1,
			"top_p":                   0.3,
			"presence_penalty":        0.4,
			"frequency_penalty":       0.7,
			"max_tokens":              256,
			"temperatureEnabled":      true,
			"topPEnabled":             true,
			"presencePenaltyEnabled":  true,
			"frequencyPenaltyEnabled": true,
			"maxTokensEnabled":        false,
			"keywords": map[string]interface{}{
				"enabled":       true,
				"top_n":         5,
				"system_prompt": "Hãy trích xuất các từ khóa/cụm từ khóa quan trọng nhất của đoạn văn bản phục vụ tìm kiếm.",
			},
			"questions": map[string]interface{}{
				"enabled":       true,
				"top_n":         3,
				"system_prompt": "Hãy đặt các câu hỏi thực tế mà đoạn văn bản này có thể giải đáp chính xác.",
			},
			"summary": map[string]interface{}{
				"enabled":       true,
				"system_prompt": "Hãy tóm tắt ngắn gọn các ý cốt lõi của đoạn văn bản này.",
			},
			"tags": map[string]interface{}{
				"enabled":     false,
				"top_n":       0,
				"tag_file_id": "",
			},
			"metadata": map[string]interface{}{
				"enabled":           false,
				"metadata":          []interface{}{},
				"built_in_metadata": []interface{}{},
			},
			"outputs": map[string]interface{}{
				"chunks": map[string]interface{}{
					"type":  "Array<Object>",
					"value": []interface{}{},
				},
			},
		}
		return payload, cloneStringMap(payload), nil

	case "Tokenizer":
		payload := map[string]interface{}{
			"fields":               "text",
			"filename_embd_weight": 0.1,
			"outputs":              map[string]interface{}{},
			"search_method":        []interface{}{"embedding", "full_text"},
		}
		return payload, cloneStringMap(payload), nil

	case "Retrieval":
		payload := map[string]interface{}{
			"dataset_ids":                dsList,
			"retrieval_from":             "dataset",
			"query":                      "{sys.query}",
			"similarity_threshold":       0.2,
			"keywords_similarity_weight": 0.7,
			"top_n":                      8,
			"top_k":                      1024,
			"rerank_id":                  "",
			"rerank_candidates_count":    64,
			"use_kg":                     false,
			"cross_languages":            []interface{}{},
			"empty_response":             "",
			"meta_data_filter":           map[string]interface{}{" logic": "and", "method": "disabled", "manual": []interface{}{}},
			"outputs": map[string]interface{}{
				"formalized_content": map[string]interface{}{
					"type":  "string",
					"value": "",
				},
				"json": map[string]interface{}{
					"type":  "Array<Object>",
					"value": []interface{}{},
				},
			},
		}
		return payload, cloneStringMap(payload), nil

	case "Categorize":
		return buildCategorizePayloads(n, downIDs, keyToID, resolvedLLMID)

	case "Agent":
		return buildAgentNodePayloads(n, upIDs, resolvedLLMID, dsList)

	case "ExeSQL":
		sqlInput := "{sys.query}"
		if len(upIDs) > 0 && upIDs[0] != "begin" {
			sqlInput = fmt.Sprintf("{%s@content}", upIDs[0])
		}
		payload := map[string]interface{}{
			"db_type":    "mysql",
			"host":       "127.0.0.1",
			"port":       3306,
			"database":   "ragflow",
			"username":   "root",
			"password":   "",
			"max_records": 128,
			"sql":        sqlInput,
			"outputs": map[string]interface{}{
				"formalized_content": map[string]interface{}{"type": "string", "value": ""},
				"json":               map[string]interface{}{"type": "Array<Object>", "value": []interface{}{}},
			},
		}
		return payload, cloneStringMap(payload), nil

	case "Message":
		var contentRefs []interface{}
		for _, uID := range upIDs {
			if strings.HasPrefix(uID, "Retrieval:") || strings.HasPrefix(uID, "ExeSQL:") {
				contentRefs = append(contentRefs, fmt.Sprintf("{%s@formalized_content}", uID))
			} else if uID != "begin" {
				contentRefs = append(contentRefs, fmt.Sprintf("{%s@content}", uID))
			}
		}
		if len(contentRefs) == 0 {
			contentRefs = []interface{}{"{sys.query}"}
		}
		payload := map[string]interface{}{
			"content":        contentRefs,
			"output_format":  "",
			"auto_play":      false,
			"status":         true,
			"memory_Penalty": false,
		}
		return payload, cloneStringMap(payload), nil

	default:
		payload := map[string]interface{}{
			"outputs": map[string]interface{}{
				"content": map[string]interface{}{"type": "string", "value": ""},
			},
		}
		return payload, cloneStringMap(payload), nil
	}
}

func buildCategorizePayloads(
	n aiPlannedNode,
	downIDs []string,
	keyToID map[string]string,
	resolvedLLMID string,
) (map[string]interface{}, map[string]interface{}, map[string]string) {
	items := make([]interface{}, 0, len(downIDs))
	catDescMap := make(map[string]interface{}, len(downIDs))
	edgeHandles := make(map[string]string, len(downIDs))

	var rawCats []map[string]interface{}
	if n.Config != nil {
		if rc, ok := n.Config["categories"].([]map[string]interface{}); ok {
			rawCats = rc
		} else if rcSlice, ok := n.Config["categories"].([]interface{}); ok {
			for _, elem := range rcSlice {
				if m, ok := elem.(map[string]interface{}); ok {
					rawCats = append(rawCats, m)
				}
			}
		}
	}

	for idx, dstID := range downIDs {
		catUUID := fmt.Sprintf("cat_handle_%d", idx+1)
		catName := fmt.Sprintf("branch_%d", idx+1)
		catDesc := fmt.Sprintf("Điều hướng sang nhánh %s", dstID)
		var exampleStrings []string

		if idx < len(rawCats) {
			rc := rawCats[idx]
			if s, ok := rc["name"].(string); ok && strings.TrimSpace(s) != "" {
				catName = strings.TrimSpace(s)
			}
			if s, ok := rc["description"].(string); ok && strings.TrimSpace(s) != "" {
				catDesc = strings.TrimSpace(s)
			}
			if tKey, ok := rc["target_key"].(string); ok {
				if mappedTarget, exists := keyToID[tKey]; exists {
					dstID = mappedTarget
				}
			}
			if exs, ok := rc["examples"].([]string); ok {
				exampleStrings = exs
			} else if exSlice, ok := rc["examples"].([]interface{}); ok {
				for _, e := range exSlice {
					if es, ok := e.(string); ok {
						exampleStrings = append(exampleStrings, es)
					}
				}
			}
		}

		formExamples := make([]interface{}, 0, len(exampleStrings))
		paramExamples := make([]interface{}, 0, len(exampleStrings))
		for _, ex := range exampleStrings {
			formExamples = append(formExamples, map[string]interface{}{"value": ex})
			paramExamples = append(paramExamples, ex)
		}

		items = append(items, map[string]interface{}{
			"name":        catName,
			"description": catDesc,
			"examples":    formExamples,
			"uuid":        catUUID,
			"index":       idx,
		})
		catDescMap[catName] = map[string]interface{}{
			"description": catDesc,
			"examples":    paramExamples,
			"to":          []interface{}{dstID},
			"index":       idx,
		}
		edgeHandles[dstID] = catUUID
	}

	base := map[string]interface{}{
		"llm_id":                    resolvedLLMID,
		"query":                     "{sys.query}",
		"message_history_window_size": 1,
		"temperature":               0.1,
		"top_p":                     0.3,
		"presence_penalty":          0.4,
		"frequency_penalty":         0.7,
		"max_tokens":                512,
		"temperatureEnabled":        true,
		"topPEnabled":               false,
		"presencePenaltyEnabled":    false,
		"frequencyPenaltyEnabled":   false,
		"maxTokensEnabled":          false,
		"outputs": map[string]interface{}{
			"category_name": map[string]interface{}{"type": "string", "value": ""},
		},
	}

	form := cloneStringMap(base)
	form["items"] = items

	params := cloneStringMap(base)
	params["category_description"] = catDescMap

	return form, params, edgeHandles
}

func buildAgentNodePayloads(
	n aiPlannedNode,
	upIDs []string,
	resolvedLLMID string,
	dsList []interface{},
) (map[string]interface{}, map[string]interface{}, map[string]string) {
	sysPrompt := "Bạn là Trợ lý AI thông minh. Hãy giải đáp yêu cầu của người dùng một cách chính xác, rõ ràng bằng tiếng Việt."
	if n.Config != nil {
		if sp, ok := n.Config["sys_prompt"].(string); ok && strings.TrimSpace(sp) != "" {
			sysPrompt = strings.TrimSpace(sp)
		}
	}

	// Automatically reference upstream Retrieval / ExeSQL / Agent outputs if not already referenced
	for _, uID := range upIDs {
		if strings.HasPrefix(uID, "Retrieval:") || strings.HasPrefix(uID, "ExeSQL:") {
			refToken := fmt.Sprintf("{%s@formalized_content}", uID)
			if !strings.Contains(sysPrompt, uID) {
				sysPrompt += "\n\nNgữ cảnh dữ liệu từ bước trước:\n" + refToken
			}
		} else if strings.HasPrefix(uID, "Agent:") {
			refToken := fmt.Sprintf("{%s@content}", uID)
			if !strings.Contains(sysPrompt, uID) {
				sysPrompt += "\n\nKết quả phân tích từ bước trước:\n" + refToken
			}
		}
	}

	var toolEntries []interface{}
	if n.Config != nil {
		var toolNames []string
		if ts, ok := n.Config["tools"].([]string); ok {
			toolNames = ts
		} else if tSlice, ok := n.Config["tools"].([]interface{}); ok {
			for _, t := range tSlice {
				if s, ok := t.(string); ok {
					toolNames = append(toolNames, s)
				}
			}
		}
		for _, tName := range toolNames {
			toolEntries = append(toolEntries, buildAgentToolConfig(tName, dsList))
		}
	}
	if toolEntries == nil {
		toolEntries = []interface{}{}
	}

	payload := map[string]interface{}{
		"llm_id":                      resolvedLLMID,
		"sys_prompt":                  sysPrompt,
		"prompts":                     []interface{}{map[string]interface{}{"role": "user", "content": "{sys.query}"}},
		"temperature":                 0.2,
		"top_p":                       0.3,
		"presence_penalty":            0.4,
		"frequency_penalty":           0.7,
		"max_tokens":                  2048,
		"temperatureEnabled":          true,
		"topPEnabled":                 false,
		"presencePenaltyEnabled":      false,
		"frequencyPenaltyEnabled":     false,
		"maxTokensEnabled":            false,
		"max_retries":                 3,
		"max_rounds":                  5,
		"delay_after_error":           1,
		"message_history_window_size": 12,
		"cite":                        true,
		"show_structured_output_dialog": false,
		"visual_files_var":            "",
		"exception_method":            "",
		"exception_default_value":     "",
		"exception_goto":              []interface{}{},
		"mcp":                         []interface{}{},
		"tools":                       toolEntries,
		"outputs": map[string]interface{}{
			"content": map[string]interface{}{
				"type":  "string",
				"value": "",
			},
		},
	}
	return payload, cloneStringMap(payload), nil
}

func buildAgentToolConfig(toolName string, dsList []interface{}) map[string]interface{} {
	switch toolName {
	case "TavilySearch":
		return map[string]interface{}{
			"component_name": "TavilySearch",
			"id":             "TavilySearch",
			"name":           "TavilySearch",
			"params": map[string]interface{}{
				"api_key":        "",
				"search_depth":   "basic",
				"topic":          "general",
				"max_results":    5,
				"days":           7,
				"include_answer": false,
				"include_raw_content": false,
				"include_images": false,
				"include_Image_descriptions": false,
				"include_domains": []interface{}{},
				"exclude_domains": []interface{}{},
			},
		}
	case "Wikipedia":
		return map[string]interface{}{
			"component_name": "Wikipedia",
			"id":             "Wikipedia",
			"name":           "Wikipedia",
			"params": map[string]interface{}{
				"top_n":    5,
				"language": "vi",
			},
		}
	case "Retrieval":
		return map[string]interface{}{
			"component_name": "Retrieval",
			"id":             "Retrieval",
			"name":           "Retrieval",
			"params": map[string]interface{}{
				"dataset_ids":                dsList,
				"retrieval_from":             "dataset",
				"similarity_threshold":       0.2,
				"keywords_similarity_weight": 0.7,
				"top_n":                      8,
				"top_k":                      1024,
				"use_kg":                     false,
				"cross_languages":            []interface{}{},
			},
		}
	default: // DuckDuckGo
		return map[string]interface{}{
			"component_name": "DuckDuckGo",
			"id":             "DuckDuckGo",
			"name":           "DuckDuckGo",
			"params": map[string]interface{}{
				"top_n":   5,
				"channel": "text",
			},
		}
	}
}

func buildCanonicalParserPayloads(resolvedLLMID string) (map[string]interface{}, map[string]interface{}, map[string]string) {
	outputs := map[string]interface{}{
		"html":     map[string]interface{}{"type": "string", "value": ""},
		"json":     map[string]interface{}{"type": "Array<Object>", "value": []interface{}{}},
		"markdown": map[string]interface{}{"type": "string", "value": ""},
		"text":     map[string]interface{}{"type": "string", "value": ""},
	}
	form := map[string]interface{}{
		"outputs": outputs,
		"setups": []interface{}{
			map[string]interface{}{
				"fileFormat":   "pdf",
				"outputFormat": "json",
				"parseMethod":  "deepdoc",
				"lang":         "English",
				" suffix":      []interface{}{"pdf"},
			},
			map[string]interface{}{
				"fileFormat":   "spreadsheet",
				"outputFormat": "html",
				"parseMethod":  "deepdoc",
				"suffix":       []interface{}{"xls", "xlsx", "csv"},
			},
			map[string]interface{}{
				"fileFormat":   "doc",
				"outputFormat": "json",
				"parseMethod":  "naive",
				"suffix":       []interface{}{"doc"},
			},
			map[string]interface{}{
				"fileFormat":   "docx",
				"outputFormat": "json",
				"parseMethod":  "deepdoc",
				"suffix":       []interface{}{"docx"},
			},
			map[string]interface{}{
				"fileFormat":   "markdown",
				"outputFormat": "markdown",
				"parseMethod":  "naive",
				"suffix":       []interface{}{"md", "mdx", "markdown"},
			},
			map[string]interface{}{
				"fileFormat":   "text&code",
				"outputFormat": "text",
				"parseMethod":  "naive",
				"suffix":       []interface{}{"txt", "py", "js", "java", "c", "cpp", "go", "ts", "json", "yaml", "yml"},
			},
			map[string]interface{}{
				"fileFormat":   "html",
				"outputFormat": "text",
				"parseMethod":  "naive",
				"suffix":       []interface{}{"htm", "html"},
			},
			map[string]interface{}{
				"fileFormat":   "slides",
				"outputFormat": "json",
				"parseMethod":  "deepdoc",
				"suffix":       []interface{}{"pptx", "ppt"},
			},
			map[string]interface{}{
				"fileFormat":     "image",
				"outputFormat":   "text",
				"parseMethod":    "ocr",
				"lang":           "English",
				"system_prompt":  "",
				"vlm":            map[string]interface{}{"llm_id": resolvedLLMID},
				"suffix":         []interface{}{"jpg", "jpeg", "png", "gif", "bmp", "tif", "tiff", "webp"},
			},
			map[string]interface{}{
				"fileFormat":   "email",
				"outputFormat": "text",
				"parseMethod":  "naive",
				"fields":       []interface{}{"from", "to", "cc", "bcc", "date", "subject", "body", "attachments"},
				"suffix":       []interface{}{"eml", "msg"},
			},
		},
	}

	params := map[string]interface{}{
		"outputs": outputs,
		"pdf": map[string]interface{}{
			"lang":          "English",
			"output_format": "json",
			"parse_method":  "deepdoc",
			"suffix":        []interface{}{"pdf"},
		},
		"spreadsheet": map[string]interface{}{
			"output_format": "html",
			"parse_method":  "deepdoc",
			"suffix":        []interface{}{"xls", "xlsx", "csv"},
		},
		"doc": map[string]interface{}{
			"output_format": "json",
			"parse_method":  "naive",
			"suffix":        []interface{}{"doc"},
		},
		"docx": map[string]interface{}{
			"output_format": "json",
			"parse_method":  "deepdoc",
			"suffix":        []interface{}{"docx"},
		},
		"markdown": map[string]interface{}{
			"output_format": "markdown",
			"parse_method":  "naive",
			"suffix":        []interface{}{"md", "mdx", "markdown"},
		},
		"text&code": map[string]interface{}{
			"output_format": "text",
			"parse_method":  "naive",
			"suffix":        []interface{}{"txt", "py", "js", "java", "c", "cpp", "go", "ts", "json", "yaml", "yml"},
		},
		"html": map[string]interface{}{
			"output_format": "text",
			"parse_method":  "naive",
			"suffix":        []interface{}{"htm", "html"},
		},
		"slides": map[string]interface{}{
			"output_format": "json",
			"parse_method":  "deepdoc",
			"suffix":        []interface{}{"pptx", "ppt"},
		},
		"image": map[string]interface{}{
			"lang":          "English",
			"output_format": "text",
			"parse_method":  "ocr",
			"system_prompt": "",
			"suffix":        []interface{}{"jpg", "jpeg", "png", "gif", "bmp", "tif", "tiff", "webp"},
		},
		"email": map[string]interface{}{
			"fields":        []interface{}{"from", "to", "cc", "bcc", "date", "subject", "body", "attachments"},
			"output_format": "text",
			"parse_method":  "naive",
			"suffix":        []interface{}{"eml", "msg"},
		},
	}

	return form, params, nil
}

func appendUniqueString(slice []string, val string) []string {
	for _, item := range slice {
		if item == val {
			return slice
		}
	}
	return append(slice, val)
}

func nonEmptyOrDefault(val, def string) string {
	if strings.TrimSpace(val) != "" {
		return strings.TrimSpace(val)
	}
	return def
}

func cloneStringMap(src map[string]interface{}) map[string]interface{} {
	raw, err := json.Marshal(src)
	if err != nil {
		out := make(map[string]interface{}, len(src))
		for k, v := range src {
			out[k] = v
		}
		return out
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return src
	}
	// Preserve deterministic keys
	_ = sort.Strings
	return out
}
