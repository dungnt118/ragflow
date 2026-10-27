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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"go.uber.org/zap"

	"ragflow/internal/agent/canvas"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	mcpclient "ragflow/internal/mcp/client"
	"ragflow/internal/server"
)

const (
	feedbackTriggerConcurrency = 8
	feedbackTriggerTimeout     = 15 * time.Second
	feedbackTriggerMaxRedirects = 3
)

var feedbackTriggerSem = make(chan struct{}, feedbackTriggerConcurrency)

// FeedbackTriggerEvent is the structured payload sent to a triggered Agent workflow,
// MCP tool, or external HTTP webhook when user feedback matches the configured rule.
type FeedbackTriggerEvent struct {
	Event       string                       `json:"event"`
	TenantID    string                       `json:"tenant_id"`
	OwnerUserID string                       `json:"owner_user_id"`
	SourceType  string                       `json:"source_type"` // "chat" | "agent"
	SourceID    string                       `json:"source_id"`   // dialog_id or canvas_id
	SessionID   string                       `json:"session_id"`
	MessageID   string                       `json:"message_id"`
	Question    string                       `json:"question"`
	Answer      string                       `json:"answer"`
	Reference   map[string]any               `json:"reference,omitempty"`
	Feedback    entity.FeedbackDetailPayload `json:"feedback"`
}

// FeedbackWebhookAgentRunner abstracts AgentService.RunAgentWithWebhook for testing and decoupling.
type FeedbackWebhookAgentRunner interface {
	RunAgentWithWebhook(ctx context.Context, userID, canvasID string, payload map[string]any) (<-chan canvas.RunEvent, error)
}

// ParsedFeedbackInput holds validated feedback fields ready for persistence and trigger evaluation.
type ParsedFeedbackInput struct {
	ThumbUp      bool
	Comment      string
	Detail       *entity.FeedbackDetailPayload
	HasMultiData bool
}

// ParseFeedbackRequest validates and normalizes a raw feedback request body against FeedbackConfig.
func ParseFeedbackRequest(
	req map[string]interface{},
	cfg *entity.FeedbackConfig,
	defaultChannel string,
	raterID string,
) (*ParsedFeedbackInput, error) {
	if len(req) == 0 {
		return nil, errors.New("thumb-up must be a boolean")
	}

	var explicitThumb *bool
	if thumbRaw, exists := req["thumbup"]; exists && thumbRaw != nil {
		b, ok := thumbRaw.(bool)
		if !ok {
			return nil, errors.New("thumb-up must be a boolean")
		}
		explicitThumb = &b
	}

	var explicitCSAT *int
	if csatRaw, exists := req["csat_score"]; exists && csatRaw != nil {
		score, ok := parseCSATInt(csatRaw)
		if !ok || score < 1 || score > 5 {
			return nil, errors.New("csat_score must be an integer between 1 and 5")
		}
		explicitCSAT = &score
	}

	var criteria map[string]any
	if critRaw, exists := req["criteria"]; exists && critRaw != nil {
		critMap, ok := critRaw.(map[string]interface{})
		if !ok {
			return nil, errors.New("criteria must be a JSON object")
		}
		if len(critMap) > 0 {
			criteria = make(map[string]any, len(critMap))
			for k, v := range critMap {
				if v == nil {
					continue
				}
				switch tv := v.(type) {
				case bool:
					criteria[k] = tv
				default:
					score, ok := parseCSATInt(v)
					if !ok || score < 1 || score > 5 {
						return nil, fmt.Errorf("criterion %q must be a boolean or integer 1..5", k)
					}
					criteria[k] = score
				}
			}
		}
	}

	if explicitThumb == nil && explicitCSAT == nil && len(criteria) == 0 {
		return nil, errors.New("thumb-up must be a boolean")
	}

	mode := entity.FeedbackModeLikeDislike
	if cfg != nil && cfg.Mode != "" {
		mode = cfg.Mode
	} else if explicitCSAT != nil || len(criteria) > 0 {
		mode = entity.FeedbackModeMultiCriteria
	}

	derivedThumb, derivedCSAT := entity.DeriveThumbUpAndScore(mode, explicitThumb, explicitCSAT, criteria, cfg)
	finalThumb := false
	if derivedThumb != nil {
		finalThumb = *derivedThumb
	}

	comment := ""
	if fbRaw, exists := req["feedback"]; exists && fbRaw != nil {
		if s, ok := fbRaw.(string); ok {
			comment = s
		}
	}
	if comment == "" {
		if cRaw, exists := req["comment"]; exists && cRaw != nil {
			if s, ok := cRaw.(string); ok {
				comment = s
			}
		}
	}

	channel := defaultChannel
	if chRaw, exists := req["channel"]; exists {
		if s, ok := chRaw.(string); ok && s != "" {
			channel = s
		}
	}
	if channel == "" {
		channel = "web"
	}

	telegramStep := 0
	if stepRaw, exists := req["telegram_step"]; exists && stepRaw != nil {
		if step, ok := parseCSATInt(stepRaw); ok && step >= 0 {
			telegramStep = step
		}
	}

	digest := entity.ComputeTriggerDigest(&finalThumb, derivedCSAT, criteria)
	detail := &entity.FeedbackDetailPayload{
		Mode:              mode,
		ThumbUp:           &finalThumb,
		CSATScore:         derivedCSAT,
		Criteria:          criteria,
		Comment:           comment,
		Channel:           channel,
		RaterID:           raterID,
		RatedAt:           time.Now().Unix(),
		LastTriggerDigest: digest,
		TelegramStep:      telegramStep,
	}

	return &ParsedFeedbackInput{
		ThumbUp:      finalThumb,
		Comment:      comment,
		Detail:       detail,
		HasMultiData: explicitCSAT != nil || len(criteria) > 0 || mode == entity.FeedbackModeMultiCriteria,
	}, nil
}

func parseCSATInt(v any) (int, bool) {
	switch val := v.(type) {
	case float64:
		if math.Trunc(val) != val {
			return 0, false
		}
		return int(val), true
	case int:
		return val, true
	case int64:
		return int(val), true
	case json.Number:
		i, err := val.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func countNegativeMessages(messages []map[string]interface{}) int {
	count := 0
	for _, msg := range messages {
		if stringValue(msg["role"]) != "assistant" {
			continue
		}
		if b, ok := msg["thumbup"].(bool); ok && !b {
			count++
		}
	}
	return count
}

func extractTurnQuestionAnswer(messages []map[string]interface{}, assistantIdx int) (string, string) {
	if assistantIdx < 0 || assistantIdx >= len(messages) {
		return "", ""
	}
	answer := stringValue(messages[assistantIdx]["content"])
	question := ""
	for i := assistantIdx - 1; i >= 0; i-- {
		if stringValue(messages[i]["role"]) == "user" {
			question = stringValue(messages[i]["content"])
			break
		}
	}
	return question, answer
}

// ShouldTriggerFeedback evaluates whether a feedback submission satisfies the configured trigger rule.
func ShouldTriggerFeedback(cfg *entity.FeedbackTriggerConfig, detail entity.FeedbackDetailPayload, negativeCount int) bool {
	if cfg == nil || !cfg.Enabled {
		return false
	}
	threshold := cfg.CSATThreshold
	if threshold <= 0 || threshold > 5 {
		threshold = 2
	}

	rule := cfg.Rule
	if rule == "" {
		rule = entity.TriggerRuleOnDislike
	}

	switch rule {
	case entity.TriggerRuleOnDislike:
		return detail.ThumbUp != nil && !*detail.ThumbUp
	case entity.TriggerRuleCSATLTE:
		if detail.CSATScore != nil {
			return *detail.CSATScore <= threshold
		}
		return detail.ThumbUp != nil && !*detail.ThumbUp
	case entity.TriggerRuleCriteriaFailed:
		if len(detail.Criteria) == 0 {
			return detail.ThumbUp != nil && !*detail.ThumbUp
		}
		if len(cfg.FailedCriteriaKeys) > 0 {
			for _, key := range cfg.FailedCriteriaKeys {
				if val, exists := detail.Criteria[key]; exists && isCriterionFailedValue(val, threshold) {
					return true
				}
			}
			return false
		}
		for _, val := range detail.Criteria {
			if isCriterionFailedValue(val, threshold) {
				return true
			}
		}
		return false
	case entity.TriggerRuleBatchCount:
		batchSize := cfg.BatchCount
		if batchSize <= 0 {
			batchSize = 3
		}
		isNegative := (detail.ThumbUp != nil && !*detail.ThumbUp) || (detail.CSATScore != nil && *detail.CSATScore <= threshold)
		return isNegative && negativeCount >= batchSize && (negativeCount%batchSize == 0)
	default:
		return detail.ThumbUp != nil && !*detail.ThumbUp
	}
}

func isCriterionFailedValue(val any, csatThreshold int) bool {
	switch v := val.(type) {
	case bool:
		return !v
	case float64:
		score := int(math.Round(v))
		return score >= 1 && score <= csatThreshold
	case int:
		return v >= 1 && v <= csatThreshold
	case json.Number:
		if i, err := v.Int64(); err == nil {
			return int(i) >= 1 && int(i) <= csatThreshold
		}
	}
	return false
}

// DispatchFeedbackTriggerAsync spawns a bounded background worker to execute the configured trigger action.
func DispatchFeedbackTriggerAsync(
	cfg *entity.FeedbackTriggerConfig,
	event FeedbackTriggerEvent,
	agentRunner FeedbackWebhookAgentRunner,
) {
	if cfg == nil || !cfg.Enabled {
		return
	}
	select {
	case feedbackTriggerSem <- struct{}{}:
		go func() {
			defer func() { <-feedbackTriggerSem }()
			ctx, cancel := context.WithTimeout(context.Background(), feedbackTriggerTimeout)
			defer cancel()
			if err := ExecuteFeedbackTrigger(ctx, cfg, event, agentRunner, nil); err != nil {
				common.Warn("feedback trigger execution failed",
					zap.String("source_type", event.SourceType),
					zap.String("source_id", event.SourceID),
					zap.String("message_id", event.MessageID),
					zap.String("action_type", cfg.ActionType),
					zap.Error(err),
				)
			}
		}()
	default:
		common.Warn("feedback trigger dropped: worker pool at capacity",
			zap.String("source_id", event.SourceID),
			zap.String("message_id", event.MessageID),
		)
	}
}

// ExecuteFeedbackTrigger runs the feedback trigger synchronously within ctx.
// Exposed for deterministic unit testing and called by DispatchFeedbackTriggerAsync.
func ExecuteFeedbackTrigger(
	ctx context.Context,
	cfg *entity.FeedbackTriggerConfig,
	event FeedbackTriggerEvent,
	agentRunner FeedbackWebhookAgentRunner,
	httpClient *http.Client,
) error {
	if cfg == nil || !cfg.Enabled {
		return nil
	}
	if event.Event == "" {
		event.Event = "feedback.triggered"
	}

	switch cfg.ActionType {
	case entity.TriggerActionAgentWebhook:
		if cfg.TargetAgentID == "" {
			return errors.New("feedback trigger: target_agent_id is empty")
		}
		if cfg.TargetAgentID == event.SourceID {
			return errors.New("feedback trigger: refusing self-referential target_agent_id")
		}
		if agentRunner == nil {
			agentRunner = NewAgentService()
		}
		ownerID := event.OwnerUserID
		if ownerID == "" {
			ownerID = event.TenantID
		}
		if dao.DB != nil {
			cv, err := dao.NewUserCanvasDAO().GetByID(ctx, dao.DB, cfg.TargetAgentID)
			if err != nil {
				return fmt.Errorf("feedback trigger: load target canvas: %w", err)
			}
			if cv == nil {
				return errors.New("feedback trigger: target canvas not found")
			}
			if event.TenantID != "" && cv.UserID != event.TenantID && cv.UserID != event.OwnerUserID {
				return errors.New("feedback trigger: target canvas belongs to another tenant")
			}
			ownerID = cv.UserID
		}
		rawBytes, err := json.Marshal(event)
		if err != nil {
			return err
		}
		var payloadMap map[string]any
		if err := json.Unmarshal(rawBytes, &payloadMap); err != nil {
			return err
		}
		runCtx := WithAgentSessionID(ctx, common.GenerateUUID())
		ch, err := agentRunner.RunAgentWithWebhook(runCtx, ownerID, cfg.TargetAgentID, payloadMap)
		if err != nil {
			return err
		}
		for range ch {
			// Drain run events until workflow finishes or context expires.
		}
		return nil

	case entity.TriggerActionMCPTool:
		if cfg.MCPServerID == "" || cfg.MCPToolName == "" {
			return errors.New("feedback trigger: mcp_server_id and mcp_tool_name are required")
		}
		if dao.DB == nil {
			return errors.New("feedback trigger: database not initialized for MCP lookup")
		}
		mcpSrv, err := dao.NewMCPServerDAO().GetByID(ctx, dao.DB, cfg.MCPServerID)
		if err != nil {
			return fmt.Errorf("feedback trigger: load MCP server: %w", err)
		}
		if mcpSrv == nil {
			return errors.New("feedback trigger: MCP server not found")
		}
		if event.TenantID != "" && mcpSrv.TenantID != event.TenantID {
			return errors.New("feedback trigger: MCP server belongs to another tenant")
		}
		argsRaw, err := json.Marshal(event)
		if err != nil {
			return err
		}
		headers := jsonMapStringValues(mcpSrv.Headers)
		variables := jsonMapStringValues(mcpSrv.Variables)
		_, err = mcpclient.CallTool(ctx, mcpclient.CallOptions{
			URL:               mcpSrv.URL,
			ServerType:        mcpSrv.ServerType,
			Headers:           headers,
			Variables:         variables,
			ToolName:          cfg.MCPToolName,
			Arguments:         argsRaw,
			Timeout:           feedbackTriggerTimeout,
			HTTPClient:        httpClient,
			AllowPrivateHosts: server.MCPAllowPrivateHosts(),
		})
		return err

	case entity.TriggerActionWebhook:
		if cfg.WebhookURL == "" {
			return errors.New("feedback trigger: webhook_url is empty")
		}
		allowPrivate := server.MCPAllowPrivateHosts()
		if _, _, err := mcpclient.AssertMCPURLSafe(cfg.WebhookURL, allowPrivate); err != nil {
			return fmt.Errorf("feedback trigger: unsafe webhook_url: %w", err)
		}
		bodyBytes, err := json.Marshal(event)
		if err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		for k, v := range cfg.WebhookHeaders {
			if k != "" && v != "" {
				req.Header.Set(k, v)
			}
		}
		client := httpClient
		if client == nil {
			client = &http.Client{
				Timeout: feedbackTriggerTimeout,
			}
		} else {
			cloned := *client
			client = &cloned
		}
		client.CheckRedirect = func(redirectReq *http.Request, via []*http.Request) error {
			if len(via) >= feedbackTriggerMaxRedirects {
				return errors.New("feedback trigger: too many redirects")
			}
			if _, _, err := mcpclient.AssertMCPURLSafe(redirectReq.URL.String(), allowPrivate); err != nil {
				return fmt.Errorf("feedback trigger: unsafe redirect target: %w", err)
			}
			return nil
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		if resp.StatusCode >= 400 {
			return fmt.Errorf("feedback trigger: webhook returned status %d", resp.StatusCode)
		}
		return nil

	default:
		return fmt.Errorf("feedback trigger: unsupported action_type %q", cfg.ActionType)
	}
}
