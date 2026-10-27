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

package entity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

const (
	FeedbackModeLikeDislike   = "like_dislike"
	FeedbackModeMultiCriteria = "multi_criteria"

	CriterionTypeCSAT5  = "csat_5"
	CriterionTypeBinary = "binary"

	TriggerRuleOnDislike      = "on_dislike"
	TriggerRuleCSATLTE        = "csat_lte"
	TriggerRuleCriteriaFailed = "criteria_failed"
	TriggerRuleBatchCount     = "batch_count"

	TriggerActionAgentWebhook = "agent_webhook"
	TriggerActionMCPTool      = "mcp_tool"
	TriggerActionWebhook      = "webhook"
)

// FeedbackCriterionConfig defines a single evaluation criterion configured by admin.
type FeedbackCriterionConfig struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"` // "csat_5" | "binary"
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

// FeedbackTriggerConfig defines automated trigger rules and target action for feedback.
type FeedbackTriggerConfig struct {
	Enabled            bool              `json:"enabled"`
	Rule               string            `json:"rule,omitempty"` // "on_dislike" | "csat_lte" | "criteria_failed" | "batch_count"
	CSATThreshold      int               `json:"csat_threshold,omitempty"`
	FailedCriteriaKeys []string          `json:"failed_criteria_keys,omitempty"`
	BatchCount         int               `json:"batch_count,omitempty"`
	ActionType         string            `json:"action_type,omitempty"` // "agent_webhook" | "mcp_tool" | "webhook"
	TargetAgentID      string            `json:"target_agent_id,omitempty"`
	MCPServerID        string            `json:"mcp_server_id,omitempty"`
	MCPToolName        string            `json:"mcp_tool_name,omitempty"`
	WebhookURL         string            `json:"webhook_url,omitempty"`
	WebhookHeaders     map[string]string `json:"webhook_headers,omitempty"`
}

// FeedbackConfig defines the feedback configuration stored in dialog.prompt_config["feedback_config"]
// or user_canvas.dsl["feedback_config"].
type FeedbackConfig struct {
	Enabled       bool                      `json:"enabled"`
	Mode          string                    `json:"mode,omitempty"` // "like_dislike" | "multi_criteria"
	Criteria      []FeedbackCriterionConfig `json:"criteria,omitempty"`
	TriggerConfig *FeedbackTriggerConfig    `json:"trigger_config,omitempty"`
}

// FeedbackDetailPayload stores structured multi-criteria rating details in message metadata["feedback_detail"].
type FeedbackDetailPayload struct {
	Mode              string         `json:"mode,omitempty"`
	ThumbUp           *bool          `json:"thumbup,omitempty"`
	CSATScore         *int           `json:"csat_score,omitempty"`
	Criteria          map[string]any `json:"criteria,omitempty"`
	Comment           string         `json:"comment,omitempty"`
	Channel           string         `json:"channel,omitempty"`
	RaterID           string         `json:"rater_id,omitempty"`
	RatedAt           int64          `json:"rated_at,omitempty"`
	LastTriggerDigest string         `json:"last_trigger_digest,omitempty"`
	TelegramStep      int            `json:"telegram_step,omitempty"`
}

// DefaultFeedbackCriteria returns the default multi-criteria configuration when none is specified.
func DefaultFeedbackCriteria() []FeedbackCriterionConfig {
	return []FeedbackCriterionConfig{
		{
			Key:      "content_quality",
			Label:    "Chất lượng nội dung phản hồi",
			Type:     CriterionTypeCSAT5,
			Required: true,
		},
		{
			Key:      "business_accuracy",
			Label:    "Độ chính xác nghiệp vụ / thông tin",
			Type:     CriterionTypeBinary,
			Required: false,
		},
	}
}

// ExtractFeedbackConfig extracts FeedbackConfig from a prompt_config or DSL map.
func ExtractFeedbackConfig(container map[string]any) *FeedbackConfig {
	if len(container) == 0 {
		return nil
	}
	raw, ok := container["feedback_config"]
	if !ok || raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var cfg FeedbackConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		return nil
	}
	if cfg.Mode == "" {
		cfg.Mode = FeedbackModeLikeDislike
	}
	if cfg.Mode == FeedbackModeMultiCriteria && len(cfg.Criteria) == 0 {
		cfg.Criteria = DefaultFeedbackCriteria()
	}
	return &cfg
}

// SanitizePublicFeedbackConfig returns a copy of FeedbackConfig safe for public/unauthenticated endpoints
// by stripping TriggerConfig (which may contain internal IDs or webhook secrets).
func SanitizePublicFeedbackConfig(cfg *FeedbackConfig) *FeedbackConfig {
	if cfg == nil {
		return nil
	}
	copiedCriteria := make([]FeedbackCriterionConfig, len(cfg.Criteria))
	copy(copiedCriteria, cfg.Criteria)
	mode := cfg.Mode
	if mode == "" {
		mode = FeedbackModeLikeDislike
	}
	return &FeedbackConfig{
		Enabled:  cfg.Enabled,
		Mode:     mode,
		Criteria: copiedCriteria,
	}
}

// DeriveThumbUpAndScore derives the aggregate ThumbUp boolean and representative CSATScore (1..5)
// according to DEC-01 (Option A: avg CSAT >= 4.0 and no binary criterion == false -> thumb_up = true).
func DeriveThumbUpAndScore(
	mode string,
	explicitThumbUp *bool,
	explicitCSAT *int,
	criteria map[string]any,
	cfg *FeedbackConfig,
) (*bool, *int) {
	var csatSum float64
	var csatCount int
	var hasBinaryFalse bool
	var hasAnyCriterion bool

	criterionTypeMap := make(map[string]string)
	if cfg != nil {
		for _, c := range cfg.Criteria {
			criterionTypeMap[c.Key] = c.Type
		}
	}

	for k, v := range criteria {
		cType := criterionTypeMap[k]
		switch val := v.(type) {
		case bool:
			hasAnyCriterion = true
			if !val {
				hasBinaryFalse = true
			}
		case float64:
			score := int(math.Round(val))
			if score >= 1 && score <= 5 {
				hasAnyCriterion = true
				csatSum += float64(score)
				csatCount++
			}
		case int:
			if val >= 1 && val <= 5 {
				hasAnyCriterion = true
				csatSum += float64(val)
				csatCount++
			}
		case json.Number:
			if i, err := val.Int64(); err == nil && i >= 1 && i <= 5 {
				hasAnyCriterion = true
				csatSum += float64(i)
				csatCount++
			}
		default:
			_ = cType
		}
	}

	var resolvedCSAT *int
	if explicitCSAT != nil && *explicitCSAT >= 1 && *explicitCSAT <= 5 {
		v := *explicitCSAT
		resolvedCSAT = &v
		if csatCount == 0 {
			csatSum = float64(v)
			csatCount = 1
		}
	} else if csatCount > 0 {
		avg := int(math.Round(csatSum / float64(csatCount)))
		if avg < 1 {
			avg = 1
		} else if avg > 5 {
			avg = 5
		}
		resolvedCSAT = &avg
	}

	// If explicitThumbUp was provided and there are no multi-criteria values overriding it, keep it;
	// If multi-criteria / CSAT was submitted without explicit thumbup (or in multi_criteria mode), derive per DEC-01 (A).
	if (mode == FeedbackModeMultiCriteria || explicitThumbUp == nil) && (csatCount > 0 || hasAnyCriterion) {
		positive := !hasBinaryFalse
		if csatCount > 0 {
			avg := csatSum / float64(csatCount)
			if avg < 4.0 {
				positive = false
			}
		}
		return &positive, resolvedCSAT
	}

	return explicitThumbUp, resolvedCSAT
}

// ComputeTriggerDigest computes a deterministic digest of the negative/failed feedback state (DEC-03 Option A).
// Returns "" if the feedback has no negative signal.
func ComputeTriggerDigest(thumbUp *bool, csatScore *int, criteria map[string]any) string {
	var parts []string
	if thumbUp != nil && !*thumbUp {
		parts = append(parts, "thumbup=false")
	}
	if csatScore != nil && *csatScore <= 3 {
		parts = append(parts, fmt.Sprintf("csat=%d", *csatScore))
	}
	if len(criteria) > 0 {
		keys := make([]string, 0, len(criteria))
		for k := range criteria {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			v := criteria[k]
			switch val := v.(type) {
			case bool:
				if !val {
					parts = append(parts, fmt.Sprintf("crit:%s=false", k))
				}
			case float64:
				score := int(math.Round(val))
				if score >= 1 && score <= 3 {
					parts = append(parts, fmt.Sprintf("crit:%s=%d", k, score))
				}
			case int:
				if val >= 1 && val <= 3 {
					parts = append(parts, fmt.Sprintf("crit:%s=%d", k, val))
				}
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:16])
}
