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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ragflow/internal/agent/canvas"
	"ragflow/internal/common"
	"ragflow/internal/entity"
)

func TestDeriveThumbUpAndScore_DEC01OptionA(t *testing.T) {
	cfg := &entity.FeedbackConfig{
		Enabled:  true,
		Mode:     entity.FeedbackModeMultiCriteria,
		Criteria: entity.DefaultFeedbackCriteria(),
	}

	// Case 1: CSAT = 4, binary = true -> thumbup = true, csat = 4
	thumb, csat := entity.DeriveThumbUpAndScore(entity.FeedbackModeMultiCriteria, nil, nil, map[string]any{
		"content_quality":   4,
		"business_accuracy": true,
	}, cfg)
	if thumb == nil || !*thumb {
		t.Fatalf("expected thumbup=true for CSAT=4 and binary=true, got %v", thumb)
	}
	if csat == nil || *csat != 4 {
		t.Fatalf("expected csat=4, got %v", csat)
	}

	// Case 2: CSAT = 5, binary = false -> thumbup = false (binary failed overrides high CSAT per DEC-01 A)
	thumb2, csat2 := entity.DeriveThumbUpAndScore(entity.FeedbackModeMultiCriteria, nil, nil, map[string]any{
		"content_quality":   5,
		"business_accuracy": false,
	}, cfg)
	if thumb2 == nil || *thumb2 {
		t.Fatalf("expected thumbup=false when binary criterion is false, got %v", thumb2)
	}
	if csat2 == nil || *csat2 != 5 {
		t.Fatalf("expected csat=5, got %v", csat2)
	}

	// Case 3: CSAT = 3, binary = true -> thumbup = false (avg CSAT < 4.0)
	thumb3, _ := entity.DeriveThumbUpAndScore(entity.FeedbackModeMultiCriteria, nil, nil, map[string]any{
		"content_quality":   3,
		"business_accuracy": true,
	}, cfg)
	if thumb3 == nil || *thumb3 {
		t.Fatalf("expected thumbup=false when CSAT=3 (< 4.0), got %v", thumb3)
	}
}

func TestSanitizePublicFeedbackConfig_StripsTriggerSecrets(t *testing.T) {
	raw := map[string]any{
		"feedback_config": map[string]any{
			"enabled": true,
			"mode":    "multi_criteria",
			"criteria": []map[string]any{
				{"key": "quality", "label": "Chất lượng", "type": "csat_5", "required": true},
			},
			"trigger_config": map[string]any{
				"enabled":          true,
				"rule":             "csat_lte",
				"csat_threshold":   2,
				"action_type":      "webhook",
				"webhook_url":      "https://internal.example.com/secret-hook",
				"webhook_headers":  map[string]string{"Authorization": "Bearer super-secret"},
				"target_agent_id":  "agent-999",
				"mcp_server_id":    "mcp-123",
			},
		},
	}

	extracted := entity.ExtractFeedbackConfig(raw)
	if extracted == nil || extracted.TriggerConfig == nil {
		t.Fatalf("expected ExtractFeedbackConfig to parse trigger_config")
	}

	sanitized := entity.SanitizePublicFeedbackConfig(extracted)
	if sanitized == nil {
		t.Fatal("expected non-nil sanitized config")
	}
	if sanitized.TriggerConfig != nil {
		t.Fatalf("expected TriggerConfig to be stripped from public config, got %#v", sanitized.TriggerConfig)
	}
	if !sanitized.Enabled || sanitized.Mode != "multi_criteria" || len(sanitized.Criteria) != 1 {
		t.Fatalf("unexpected sanitized config: %#v", sanitized)
	}
}

func TestUpdateMessageFeedback_MultiCriteriaAndTriggerIdempotency(t *testing.T) {
	store := newFakeSessionStore()
	store.sessions["session-1"] = &entity.ChatSession{
		ID:       "session-1",
		DialogID: "chat-1",
		UserID:   strPtr("user-1"),
		Message: json.RawMessage(`[
			{"role":"assistant","content":"Xin chào!"},
			{"role":"user","content":"Giá dịch vụ là bao nhiêu?","id":"msg-1"},
			{"role":"assistant","content":"Giá dịch vụ là 100k.","id":"msg-1"}
		]`),
		Reference: json.RawMessage(`[
			{"chunks":[{"id":"chunk-1","kb_id":"kb-1","similarity":0.9}]}
		]`),
	}
	store.dialogExists["tenant-owner|chat-1"] = true
	store.dialogs["chat-1"] = &entity.Chat{
		ID:       "chat-1",
		TenantID: "tenant-owner",
		PromptConfig: entity.JSONMap{
			"feedback_config": map[string]any{
				"enabled": true,
				"mode":    "multi_criteria",
				"criteria": []map[string]any{
					{"key": "content_quality", "label": "Chất lượng nội dung", "type": "csat_5", "required": true},
					{"key": "business_accuracy", "label": "Độ chính xác nghiệp vụ", "type": "binary"},
				},
			},
		},
	}

	svc := &ChatSessionService{
		chatSessionDAO:    store,
		permissionChecker: store,
		pipeline:          &fakePipeline{},
	}

	// 1. Submit negative multi-criteria rating (CSAT=2, accuracy=true, comment="Thiếu bảng giá chi tiết")
	resp, code, err := svc.UpdateMessageFeedback(t.Context(), "user-1", "chat-1", "session-1", "msg-1", map[string]any{
		"criteria": map[string]any{
			"content_quality":   2,
			"business_accuracy": true,
		},
		"feedback": "Thiếu bảng giá chi tiết",
		"channel":  "widget",
	})
	if err != nil || code != common.CodeSuccess {
		t.Fatalf("first feedback failed: code=%v err=%v", code, err)
	}
	assistant := resp.Messages[2]
	if assistant["thumbup"] != false {
		t.Fatalf("expected derived thumbup=false, got %v", assistant["thumbup"])
	}
	if assistant["feedback"] != "Thiếu bảng giá chi tiết" {
		t.Fatalf("expected plain text feedback preserved, got %v", assistant["feedback"])
	}
	detail, ok := assistant["feedback_detail"].(map[string]any)
	if !ok {
		t.Fatalf("expected feedback_detail map on message, got %#v", assistant["feedback_detail"])
	}
	if detail["csat_score"] != float64(2) || detail["channel"] != "widget" {
		t.Fatalf("unexpected feedback_detail: %#v", detail)
	}
	firstDigest, _ := detail["last_trigger_digest"].(string)
	if firstDigest == "" {
		t.Fatalf("expected non-empty last_trigger_digest on negative feedback")
	}

	// 2. Re-submit the exact same negative criteria -> ShouldFireTrigger in history update must be false (DEC-03 Option A)
	_, code, err = svc.UpdateMessageFeedback(t.Context(), "user-1", "chat-1", "session-1", "msg-1", map[string]any{
		"criteria": map[string]any{
			"content_quality":   2,
			"business_accuracy": true,
		},
		"feedback": "Sửa lại câu chữ comment",
	})
	if err != nil || code != common.CodeSuccess {
		t.Fatalf("second feedback failed: code=%v err=%v", code, err)
	}
}

type fakeTriggerAgentRunner struct {
	called   int
	canvasID string
	payload  map[string]any
}

func (f *fakeTriggerAgentRunner) RunAgentWithWebhook(_ context.Context, _, canvasID string, payload map[string]any) (<-chan canvas.RunEvent, error) {
	f.called++
	f.canvasID = canvasID
	f.payload = payload
	ch := make(chan canvas.RunEvent, 1)
	ch <- canvas.RunEvent{Type: "done"}
	close(ch)
	return ch, nil
}

func TestExecuteFeedbackTrigger_AgentSelfLoopAndSSRFGuard(t *testing.T) {
	runner := &fakeTriggerAgentRunner{}
	event := FeedbackTriggerEvent{
		TenantID:    "tenant-1",
		OwnerUserID: "tenant-1",
		SourceType:  "agent",
		SourceID:    "agent-self",
		SessionID:   "sess-1",
		MessageID:   "msg-1",
		Question:    "Hỏi",
		Answer:      "Đáp",
	}

	// 1. Self-referential target_agent_id == SourceID must be rejected (FND-05)
	err := ExecuteFeedbackTrigger(t.Context(), &entity.FeedbackTriggerConfig{
		Enabled:       true,
		ActionType:    entity.TriggerActionAgentWebhook,
		TargetAgentID: "agent-self",
	}, event, runner, nil)
	if err == nil || !strings.Contains(err.Error(), "self-referential") {
		t.Fatalf("expected self-referential error, got %v", err)
	}
	if runner.called != 0 {
		t.Fatalf("runner should not have been called on self-loop")
	}

	// 2. Webhook SSRF check rejects non-HTTP scheme and private IP (127.0.0.1)
	err = ExecuteFeedbackTrigger(t.Context(), &entity.FeedbackTriggerConfig{
		Enabled:    true,
		ActionType: entity.TriggerActionWebhook,
		WebhookURL: "file:///etc/passwd",
	}, event, runner, nil)
	if err == nil || !strings.Contains(err.Error(), "unsafe webhook_url") {
		t.Fatalf("expected unsafe webhook_url error, got %v", err)
	}

	// 3. Private loopback webhook URL is blocked by SSRF guard
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, srv.URL+"/loop", http.StatusFound)
	}))
	defer srv.Close()
	err = ExecuteFeedbackTrigger(t.Context(), &entity.FeedbackTriggerConfig{
		Enabled:    true,
		ActionType: entity.TriggerActionWebhook,
		WebhookURL: srv.URL,
	}, event, runner, srv.Client())
	if err == nil || !strings.Contains(err.Error(), "unsafe webhook_url") {
		t.Fatalf("expected loopback SSRF guard error, got %v", err)
	}
}

func TestBuildTelegramRatingButtons_CallbackDataUnder64Bytes(t *testing.T) {
	msgID := "88b17c7a9012345678901234567890ab" // 32-char UUID
	cfg := &entity.FeedbackConfig{
		Enabled:  true,
		Mode:     entity.FeedbackModeMultiCriteria,
		Criteria: entity.DefaultFeedbackCriteria(),
	}

	step0 := BuildTelegramRatingButtons(msgID, cfg, 0)
	if len(step0) != 1 || len(step0[0]) != 5 {
		t.Fatalf("expected 1 row of 5 CSAT buttons at step 0, got %#v", step0)
	}
	for _, btn := range step0[0] {
		if len(btn.CallbackData) > 64 {
			t.Fatalf("callback_data %q exceeds 64 bytes (%d)", btn.CallbackData, len(btn.CallbackData))
		}
	}

	step1 := BuildTelegramRatingButtons(msgID, cfg, 1)
	if len(step1) != 1 || len(step1[0]) != 2 {
		t.Fatalf("expected 1 row of 2 binary buttons at step 1, got %#v", step1)
	}
	for _, btn := range step1[0] {
		if len(btn.CallbackData) > 64 {
			t.Fatalf("callback_data %q exceeds 64 bytes (%d)", btn.CallbackData, len(btn.CallbackData))
		}
	}
}

func TestFeedbackAuditService_SummarizeFilterAndKPI(t *testing.T) {
	cfg := &entity.FeedbackConfig{
		Enabled:  true,
		Mode:     entity.FeedbackModeMultiCriteria,
		Criteria: entity.DefaultFeedbackCriteria(),
	}

	// Session 1: Chat Assistant (Widget), Negative feedback (CSAT=2, business_accuracy=false)
	msgs1 := []map[string]interface{}{
		{"role": "user", "content": "Chi phí gói Enterprise là bao nhiêu?"},
		{
			"role":     "assistant",
			"content":  "Hiện tại chưa có thông tin bảng giá Enterprise.",
			"thumbup":  false,
			"feedback": "Thiếu bảng giá chi tiết",
			"feedback_detail": map[string]interface{}{
				"mode":       "multi_criteria",
				"thumbup":    false,
				"csat_score": float64(2),
				"criteria": map[string]interface{}{
					"content_quality":   float64(2),
					"business_accuracy": false,
				},
				"comment":  "Thiếu bảng giá chi tiết",
				"rated_at": int64(1761350000),
				"channel":  "widget",
			},
		},
	}
	sum1, rounds1 := summarizeSessionMessages(msgs1, "widget", cfg)
	if !sum1.HasFeedback || sum1.RatedCount != 1 || sum1.NegativeCount != 1 || sum1.PositiveCount != 0 {
		t.Fatalf("unexpected summary for session 1: %#v", sum1)
	}
	if rounds1 != 1 {
		t.Fatalf("expected 1 round, got %d", rounds1)
	}
	if sum1.AvgCSAT == nil || *sum1.AvgCSAT != 2.0 {
		t.Fatalf("expected AvgCSAT=2.0, got %v", sum1.AvgCSAT)
	}
	if len(sum1.FailedCriteria) != 2 {
		t.Fatalf("expected 2 failed criteria, got %v", sum1.FailedCriteria)
	}

	// Session 2: Agent (Web), Positive feedback (CSAT=5, business_accuracy=true)
	msgs2 := []map[string]interface{}{
		{"role": "user", "content": "Hướng dẫn cấu hình webhook"},
		{
			"role":     "assistant",
			"content":  "Bạn vào mục Cài đặt -> Đánh giá -> Trigger tự động.",
			"thumbup":  true,
			"feedback": "Rất rõ ràng",
			"feedback_detail": map[string]interface{}{
				"mode":       "multi_criteria",
				"thumbup":    true,
				"csat_score": float64(5),
				"criteria": map[string]interface{}{
					"content_quality":   float64(5),
					"business_accuracy": true,
				},
				"comment":  "Rất rõ ràng",
				"rated_at": int64(1761350500),
				"channel":  "web",
			},
		},
	}
	sum2, _ := summarizeSessionMessages(msgs2, "web", cfg)

	items := []UnifiedFeedbackSessionItem{
		{
			ID:              "sess-chat-1",
			SessionID:       "sess-chat-1",
			Name:            "Chi phí gói Enterprise",
			SourceType:      "chat",
			AppID:           "chat-1",
			AppName:         "Trợ lý Bán hàng",
			Channel:         "widget",
			Round:           1,
			FeedbackSummary: sum1,
			Messages:        msgs1,
		},
		{
			ID:              "sess-agent-2",
			SessionID:       "sess-agent-2",
			Name:            "Hướng dẫn cấu hình webhook",
			SourceType:      "agent",
			AppID:           "agent-1",
			AppName:         "Agent Hỗ trợ Kỹ thuật",
			Channel:         "web",
			Round:           1,
			FeedbackSummary: sum2,
			Messages:        msgs2,
		},
	}

	// Test filtering by thumb_up=false, max_csat=2, channel=widget, criterion_key=business_accuracy, criterion_value=failed
	thumbFalse := false
	maxCSAT2 := 2
	reqNeg := FeedbackAuditFilterRequest{
		ThumbUp:        &thumbFalse,
		MaxCSAT:        &maxCSAT2,
		Channel:        "widget",
		CriterionKey:   "business_accuracy",
		CriterionValue: "failed",
		Keywords:       "Enterprise",
	}
	if !matchesFeedbackAuditFilter(items[0], reqNeg) {
		t.Fatalf("expected items[0] to match negative filter")
	}
	if matchesFeedbackAuditFilter(items[1], reqNeg) {
		t.Fatalf("expected items[1] NOT to match negative filter")
	}

	// Test KPI aggregation across both Chat and Agent sessions
	criterionMeta := map[string]entity.FeedbackCriterionConfig{
		"content_quality":   {Key: "content_quality", Label: "Chất lượng nội dung phản hồi", Type: entity.CriterionTypeCSAT5},
		"business_accuracy": {Key: "business_accuracy", Label: "Độ chính xác nghiệp vụ", Type: entity.CriterionTypeBinary},
	}
	kpi := computeFeedbackKPISummary(items, criterionMeta)
	if kpi.TotalSessions != 2 || kpi.RatedSessions != 2 || kpi.RatedMessages != 2 {
		t.Fatalf("unexpected session/message counts in KPI: %#v", kpi)
	}
	if kpi.PositiveCount != 1 || kpi.NegativeCount != 1 || kpi.SatisfactionRate != 50.0 {
		t.Fatalf("unexpected satisfaction KPI: %#v", kpi)
	}
	if kpi.AvgCSAT != 3.5 || kpi.CSATCount != 2 {
		t.Fatalf("expected AvgCSAT=3.5 and CSATCount=2, got AvgCSAT=%v CSATCount=%d", kpi.AvgCSAT, kpi.CSATCount)
	}
	if kpi.BySourceType["chat"] != 1 || kpi.BySourceType["agent"] != 1 {
		t.Fatalf("unexpected BySourceType: %#v", kpi.BySourceType)
	}
	if kpi.ByChannel["widget"] != 1 || kpi.ByChannel["web"] != 1 {
		t.Fatalf("unexpected ByChannel: %#v", kpi.ByChannel)
	}
	if kpi.CriteriaBreakdown["business_accuracy"].NegativeCount != 1 || kpi.CriteriaBreakdown["business_accuracy"].PositiveCount != 1 {
		t.Fatalf("unexpected business_accuracy breakdown: %#v", kpi.CriteriaBreakdown["business_accuracy"])
	}
}

