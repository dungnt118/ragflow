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
	"strings"
	"testing"

	"ragflow/internal/agent/component"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func TestGenerateAgentByAI_RuleBasedArchitecturesAndDualShapeDSL(t *testing.T) {
	setupAgentSessionServiceTest(t)

	ctx := t.Context()
	svc := NewAgentService()

	testCases := []struct {
		name               string
		req                GenerateAgentByAIRequest
		wantCategory       string
		wantComponentNames []string
		checkCategorize    bool
		checkTools         bool
		checkDualChunker   string
	}{
		{
			name: "Customer_Support_Categorize_And_RAG",
			req: GenerateAgentByAIRequest{
				Prompt:         "Tạo trợ lý CSKH tự động phân loại khiếu nại và tra cứu chính sách bảo hành sản phẩm",
				CanvasCategory: "auto",
				LLMID:          "gpt-4o___OpenAI-API@OpenAI-API-Compatible",
				DatasetIDs:     []string{"ds-policy-001", "ds-faq-002"},
			},
			wantCategory:       "agent_canvas",
			wantComponentNames: []string{"Begin", "Categorize", "Retrieval", "Agent", "Message"},
			checkCategorize:    true,
		},
		{
			name: "Text2SQL_Database_BI_Analyst",
			req: GenerateAgentByAIRequest{
				Prompt:         "Xây dựng chuyên gia phân tích dữ liệu SQL truy vấn database MySQL để xuất báo cáo doanh thu BI",
				CanvasCategory: "agent_canvas",
				LLMID:          "qwen-max___Tongyi-Qianwen@Tongyi-Qianwen",
				DatasetIDs:     []string{"ds-schema-001"},
			},
			wantCategory:       "agent_canvas",
			wantComponentNames: []string{"Begin", "Retrieval", "Agent", "ExeSQL", "Message"},
		},
		{
			name: "Web_Crawler_And_CMS_Extraction_Demego",
			req: GenerateAgentByAIRequest{
				Prompt:         "Xây dựng trợ lý AI cào dữ liệu website CMS https://cms.demego.vn/ và tổng hợp hướng dẫn quản lý tòa nhà",
				CanvasCategory: "agent_canvas",
				LLMID:          "gpt-4o___OpenAI-API@OpenAI-API-Compatible",
				DatasetIDs:     []string{"ds-demego-cms"},
			},
			wantCategory:       "agent_canvas",
			wantComponentNames: []string{"Begin", "Crawler", "Retrieval", "Agent", "Message"},
			checkTools:         true,
		},
		{
			name: "Hybrid_Web_Search_And_RAG_Research",
			req: GenerateAgentByAIRequest{
				Prompt:         "Trợ lý nghiên cứu thị trường kết hợp tìm kiếm mạng Internet TavilySearch và kho tài liệu nội bộ",
				CanvasCategory: "agent_canvas",
				LLMID:          "gpt-4o___OpenAI-API@OpenAI-API-Compatible",
				DatasetIDs:     []string{"ds-internal-research"},
			},
			wantCategory:       "agent_canvas",
			wantComponentNames: []string{"Begin", "Retrieval", "Agent", "Message"},
			checkTools:         true,
		},
		{
			name: "Dataflow_Hierarchical_Legal_SOP_Pipeline",
			req: GenerateAgentByAIRequest{
				Prompt:         "Thiết kế pipeline nạp dữ liệu hợp đồng pháp lý và sổ tay SOP cắt theo tiêu đề chương mục",
				CanvasCategory: "auto",
				LLMID:          "gpt-4o___OpenAI-API@OpenAI-API-Compatible",
			},
			wantCategory:       "dataflow_canvas",
			wantComponentNames: []string{"File", "Parser", "TitleChunker", "Extractor", "Tokenizer"},
			checkDualChunker:   "TitleChunker",
		},
		{
			name: "Dataflow_Advanced_Token_Ingestion_Pipeline",
			req: GenerateAgentByAIRequest{
				Prompt:         "Xây dựng dataflow pipeline bóc tách PDF, Excel và làm giàu ngữ cảnh trước khi đánh chỉ mục",
				CanvasCategory: "dataflow_canvas",
				LLMID:          "gpt-4o___OpenAI-API@OpenAI-API-Compatible",
			},
			wantCategory:       "dataflow_canvas",
			wantComponentNames: []string{"File", "Parser", "TokenChunker", "Extractor", "Tokenizer"},
			checkDualChunker:   "TokenChunker",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp, code, err := svc.GenerateAgentByAI(ctx, "user-1", &tc.req)
			if err != nil || code != common.CodeSuccess {
				t.Fatalf("GenerateAgentByAI failed: code=%v err=%v", code, err)
			}
			if resp.CanvasCategory != tc.wantCategory {
				t.Fatalf("CanvasCategory = %q, want %q", resp.CanvasCategory, tc.wantCategory)
			}
			if strings.TrimSpace(resp.Title) == "" || strings.TrimSpace(resp.Description) == "" || strings.TrimSpace(resp.Explanation) == "" {
				t.Fatalf("expected non-empty Title/Description/Explanation, got title=%q desc=%q expl=%q",
					resp.Title, resp.Description, resp.Explanation)
			}
			if len(resp.NodePlan) < len(tc.wantComponentNames) {
				t.Fatalf("NodePlan len = %d, want at least %d", len(resp.NodePlan), len(tc.wantComponentNames))
			}

			// Verify Go backend validators pass on the compiled DSL
			if err := component.ValidateIntegerParameters(resp.DSL); err != nil {
				t.Fatalf("ValidateIntegerParameters failed: %v", err)
			}
			if err := component.ValidateDraftDynamicEntries(resp.DSL); err != nil {
				t.Fatalf("ValidateDraftDynamicEntries failed: %v", err)
			}

			componentsMap, ok := resp.DSL["components"].(map[string]interface{})
			if !ok || len(componentsMap) == 0 {
				t.Fatalf("DSL missing components map: %#v", resp.DSL["components"])
			}
			graphMap, ok := resp.DSL["graph"].(map[string]interface{})
			if !ok {
				t.Fatalf("DSL missing graph map: %#v", resp.DSL["graph"])
			}
			nodesSlice, ok := graphMap["nodes"].([]interface{})
			if !ok || len(nodesSlice) == 0 {
				t.Fatalf("DSL graph.nodes missing or empty: %#v", graphMap["nodes"])
			}
			edgesSlice, ok := graphMap["edges"].([]interface{})
			if !ok || len(edgesSlice) == 0 {
				t.Fatalf("DSL graph.edges missing or empty: %#v", graphMap["edges"])
			}

			// Verify every wanted component exists in components
			seenComponents := make(map[string]bool)
			for compID, rawComp := range componentsMap {
				compObj, _ := rawComp.(map[string]interface{})
				objMap, _ := compObj["obj"].(map[string]interface{})
				cName, _ := objMap["component_name"].(string)
				paramsMap, _ := objMap["params"].(map[string]interface{})
				seenComponents[cName] = true

				// Verify dataset_ids and llm_id binding
				if cName == "Retrieval" && len(tc.req.DatasetIDs) > 0 {
					dsSlice, _ := paramsMap["dataset_ids"].([]interface{})
					if len(dsSlice) != len(tc.req.DatasetIDs) {
						t.Errorf("Retrieval %s dataset_ids len = %d, want %d", compID, len(dsSlice), len(tc.req.DatasetIDs))
					}
				}
				if (cName == "Agent" || cName == "Categorize" || cName == "Extractor") && tc.req.LLMID != "" {
					if gotLLM, _ := paramsMap["llm_id"].(string); gotLLM != tc.req.LLMID {
						t.Errorf("%s %s llm_id = %q, want %q", cName, compID, gotLLM, tc.req.LLMID)
					}
				}

				// Verify Categorize Dual-Shape parity (items in form vs category_description in params)
				if tc.checkCategorize && cName == "Categorize" {
					catDesc, ok := paramsMap["category_description"].(map[string]interface{})
					if !ok || len(catDesc) < 2 {
						t.Fatalf("Categorize obj.params.category_description missing or < 2 branches: %#v", paramsMap["category_description"])
					}
				}

				// Verify Dataflow Dual-Shape parity for TokenChunker / TitleChunker / Parser
				if tc.checkDualChunker == "TokenChunker" && cName == "TokenChunker" {
					if ov, _ := paramsMap["overlapped_percent"].(float64); ov != 0.1 {
						t.Errorf("TokenChunker obj.params.overlapped_percent = %v, want 0.1", paramsMap["overlapped_percent"])
					}
				}
				if tc.checkDualChunker == "TitleChunker" && cName == "TitleChunker" {
					if _, ok := paramsMap["levels"].([]interface{}); !ok {
						t.Errorf("TitleChunker obj.params.levels missing: %#v", paramsMap["levels"])
					}
				}
			}

			for _, wantComp := range tc.wantComponentNames {
				if !seenComponents[wantComp] {
					t.Errorf("missing expected component %q in generated DSL (seen: %v)", wantComp, seenComponents)
				}
			}

			// Verify Tool visual node is attached in graph.nodes when Agent has tools
			if tc.checkTools {
				hasToolNode := false
				for _, rawN := range nodesSlice {
					nMap, _ := rawN.(map[string]interface{})
					if nMap["type"] == "toolNode" {
						hasToolNode = true
						break
					}
				}
				if !hasToolNode {
					t.Errorf("expected visual toolNode in graph.nodes for tool-enabled Agent")
				}
			}
		})
	}
}

func TestGenerateAgentByAI_CustomLLMPlannerAndAutoCreate(t *testing.T) {
	setupAgentSessionServiceTest(t)

	// Create permission tables needed by CreateAgent
	for _, statement := range []string{
		`CREATE TABLE IF NOT EXISTS permission (
			id TEXT PRIMARY KEY, member_id TEXT, group_id TEXT, tenant_id TEXT NOT NULL,
			resource_type TEXT NOT NULL, resource_id TEXT, permission INTEGER NOT NULL DEFAULT 0,
			status TEXT, create_time INTEGER, create_date DATETIME, update_time INTEGER, update_date DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS group_member (id TEXT PRIMARY KEY, member_id TEXT, group_id TEXT, status TEXT)`,
		`CREATE TABLE IF NOT EXISTS group_info (id TEXT PRIMARY KEY, tenant_id TEXT, status TEXT)`,
	} {
		if err := dao.DB.Exec(statement).Error; err != nil {
			t.Fatalf("create permission table: %v", err)
		}
	}

	const userID = "user-ai-creator"
	valid := string(entity.StatusValid)
	if err := dao.DB.Create(&entity.User{ID: userID, Nickname: "AI Architect", Email: "ai@ragflow.io"}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if err := dao.DB.Create(&entity.UserTenant{
		ID:       "ut-ai-creator",
		UserID:   userID,
		TenantID: userID,
		Role:     string(permission.RoleOwner),
		Status:   &valid,
	}).Error; err != nil {
		t.Fatalf("seed user_tenant: %v", err)
	}

	origPlanner := AIPlannerOverride
	AIPlannerOverride = func(ctx context.Context, uID, tID string, req *GenerateAgentByAIRequest) (*aiBlueprint, error) {
		return &aiBlueprint{
			Title:          "Trợ lý Kiểm toán Nội bộ AI",
			Description:    "Luồng kiểm toán tự động kết hợp tra cứu quy định và phân tích chuyên sâu",
			CanvasCategory: "agent_canvas",
			Explanation:    "Begin -> Retrieval -> Agent -> Message",
			Prologue:       "Xin chào, tôi là Trợ lý Kiểm toán Nội bộ.",
			Nodes: []aiPlannedNode{
				{Key: "begin", ComponentName: "Begin", Name: "Begin", Role: "Khởi tạo", UpstreamKeys: []string{}, DownstreamKeys: []string{"ret"}},
				{Key: "ret", ComponentName: "Retrieval", Name: "Tra cứu Quy chế", Role: "Truy hồi", UpstreamKeys: []string{"begin"}, DownstreamKeys: []string{"ag"}},
				{Key: "ag", ComponentName: "Agent", Name: "Kiểm toán viên AI", Role: "Phân tích", UpstreamKeys: []string{"ret"}, DownstreamKeys: []string{"msg"}},
				{Key: "msg", ComponentName: "Message", Name: "Báo cáo", Role: "Phản hồi", UpstreamKeys: []string{"ag"}, DownstreamKeys: []string{}},
			},
		}, nil
	}
	t.Cleanup(func() { AIPlannerOverride = origPlanner })

	ctx := t.Context()
	svc := NewAgentService()
	resp, code, err := svc.GenerateAgentByAI(ctx, userID, &GenerateAgentByAIRequest{
		Prompt:         "Tạo trợ lý kiểm toán nội bộ",
		CanvasCategory: "agent_canvas",
		DatasetIDs:     []string{"ds-audit-1"},
		AutoCreate:     true,
	})
	if err != nil || code != common.CodeSuccess {
		t.Fatalf("GenerateAgentByAI with AutoCreate failed: code=%v err=%v", code, err)
	}
	if resp.UsedFallback {
		t.Errorf("UsedFallback = true, want false when AIPlannerOverride succeeded")
	}
	if resp.Agent == nil || resp.Agent.ID == "" {
		t.Fatalf("expected persisted Agent when AutoCreate=true, got %+v", resp.Agent)
	}
	if resp.Agent.Title == nil || *resp.Agent.Title != "Trợ lý Kiểm toán Nội bộ AI" {
		t.Errorf("persisted Agent title = %v, want 'Trợ lý Kiểm toán Nội bộ AI'", resp.Agent.Title)
	}

	// Verify row exists in user_canvas table
	persisted, err := dao.NewUserCanvasDAO().GetByID(ctx, dao.DB, resp.Agent.ID)
	if err != nil || persisted == nil {
		t.Fatalf("GetByID persisted agent failed: %v", err)
	}
}

func TestGenerateAgentByAI_ValidationErrors(t *testing.T) {
	svc := NewAgentService()
	ctx := t.Context()

	if _, code, err := svc.GenerateAgentByAI(ctx, "", &GenerateAgentByAIRequest{Prompt: "test"}); err == nil || code != common.CodeAuthenticationError {
		t.Fatalf("expected unauthenticated error, got code=%v err=%v", code, err)
	}
	if _, code, err := svc.GenerateAgentByAI(ctx, "user-1", &GenerateAgentByAIRequest{Prompt: "   "}); err == nil || code != common.CodeArgumentError {
		t.Fatalf("expected empty prompt argument error, got code=%v err=%v", code, err)
	}
}
