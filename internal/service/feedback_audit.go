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
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

// FeedbackAuditFilterRequest represents the unified query parameters for listing and filtering
// conversation sessions with feedback across both Chat Assistants and Agents.
type FeedbackAuditFilterRequest struct {
	SourceType     string // "all" | "chat" | "agent"
	AppID          string // dialog_id (chat) or canvas_id (agent)
	SessionID      string
	UserID         string
	HasFeedback    *bool   // true: only sessions with >= 1 rated message; false: only unrated sessions
	ThumbUp        *bool   // true: positive feedback; false: negative feedback
	MinCSAT        *int    // 1..5
	MaxCSAT        *int    // 1..5
	Channel        string  // "web" | "widget" | "telegram" | "api"
	CriterionKey   string  // specific criterion key to filter
	CriterionValue string  // "true" | "false" | "1".."5" | "failed"
	Keywords       string  // search in session id, session name, message content, or feedback comment
	FromDate       string
	ToDate         string
	OrderBy        string // "update_time" | "create_time" | "csat_score" | "rated_count" | "round"
	Desc           bool
	Page           int
	PageSize       int
}

// CriterionBreakdownStat holds aggregated statistics for a single feedback criterion.
type CriterionBreakdownStat struct {
	Key           string  `json:"key"`
	Label         string  `json:"label"`
	Type          string  `json:"type"` // "csat_5" | "binary"
	TotalCount    int     `json:"total_count"`
	PositiveCount int     `json:"positive_count"`
	NegativeCount int     `json:"negative_count"`
	AvgScore      float64 `json:"avg_score,omitempty"`
}

// FeedbackKPISummary holds the unified KPI metrics for the filtered sessions.
type FeedbackKPISummary struct {
	TotalSessions      int                               `json:"total_sessions"`
	RatedSessions      int                               `json:"rated_sessions"`
	RatedMessages      int                               `json:"rated_messages"`
	PositiveCount      int                               `json:"positive_count"`
	NegativeCount      int                               `json:"negative_count"`
	SatisfactionRate   float64                           `json:"satisfaction_rate"`
	AvgCSAT            float64                           `json:"avg_csat"`
	CSATCount          int                               `json:"csat_count"`
	CSATDistribution   map[string]int                    `json:"csat_distribution"`
	ByChannel          map[string]int                    `json:"by_channel"`
	BySourceType       map[string]int                    `json:"by_source_type"`
	CriteriaBreakdown  map[string]CriterionBreakdownStat `json:"criteria_breakdown"`
}

// SessionFeedbackSummary holds the aggregated feedback status of a single session.
type SessionFeedbackSummary struct {
	HasFeedback    bool           `json:"has_feedback"`
	RatedCount     int            `json:"rated_count"`
	PositiveCount  int            `json:"positive_count"`
	NegativeCount  int            `json:"negative_count"`
	AvgCSAT        *float64       `json:"avg_csat,omitempty"`
	MinCSAT        *int           `json:"min_csat,omitempty"`
	MaxCSAT        *int           `json:"max_csat,omitempty"`
	Channels       []string       `json:"channels,omitempty"`
	LatestComment  string         `json:"latest_comment,omitempty"`
	LatestRatedAt  int64          `json:"latest_rated_at,omitempty"`
	FailedCriteria []string       `json:"failed_criteria,omitempty"`
	CriteriaValues map[string]any `json:"criteria_values,omitempty"`
}

// UnifiedFeedbackSessionItem represents a normalized session row across Chat and Agent.
type UnifiedFeedbackSessionItem struct {
	ID              string                   `json:"id"`
	SessionID       string                   `json:"session_id"`
	Name            string                   `json:"name"`
	SourceType      string                   `json:"source_type"` // "chat" | "agent"
	StorageTable    string                   `json:"storage_table"`
	AppID           string                   `json:"app_id"`
	AppName         string                   `json:"app_name"`
	UserID          string                   `json:"user_id"`
	Channel         string                   `json:"channel"`
	Round           int                      `json:"round"`
	ThumbUp         int                      `json:"thumb_up"`
	Errors          *string                  `json:"errors,omitempty"`
	VersionTitle    *string                  `json:"version_title,omitempty"`
	FeedbackSummary SessionFeedbackSummary   `json:"feedback_summary"`
	FeedbackConfig  *entity.FeedbackConfig   `json:"feedback_config,omitempty"`
	Messages        []map[string]interface{} `json:"messages"`
	Message         []map[string]interface{} `json:"message"`
	Reference       []interface{}            `json:"reference"`
	CreateTime      int64                    `json:"create_time"`
	UpdateTime      int64                    `json:"update_time"`
	CreateDate      string                   `json:"create_date"`
	UpdateDate      string                   `json:"update_date"`
}

// UnifiedFeedbackAuditResponse is the unified response returned by /api/v1/feedback/sessions,
// /api/v1/chats/:chat_id/feedback-sessions, and /api/v1/agents/:canvas_id/feedback-sessions.
type UnifiedFeedbackAuditResponse struct {
	KPI      FeedbackKPISummary           `json:"kpi"`
	Total    int64                        `json:"total"`
	Page     int                          `json:"page"`
	PageSize int                          `json:"page_size"`
	Sessions []UnifiedFeedbackSessionItem `json:"sessions"`
	Apps     []FeedbackAppOption          `json:"apps,omitempty"`
}

// FeedbackAppOption describes an accessible Chat Assistant or Agent for filter dropdowns.
type FeedbackAppOption struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	SourceType     string                 `json:"source_type"` // "chat" | "agent"
	FeedbackConfig *entity.FeedbackConfig `json:"feedback_config,omitempty"`
}

// FeedbackAuditService provides unified feedback session querying and KPI aggregation.
type FeedbackAuditService struct {
	permissionChecker resourceScopeChecker
}

// NewFeedbackAuditService creates a new FeedbackAuditService.
func NewFeedbackAuditService() *FeedbackAuditService {
	return &FeedbackAuditService{}
}

type appMetadata struct {
	ID             string
	Name           string
	SourceType     string
	FeedbackConfig *entity.FeedbackConfig
}

// ListFeedbackSessions returns paginated unified sessions and KPI summary across Chat Assistants and Agents.
func (s *FeedbackAuditService) ListFeedbackSessions(
	ctx context.Context,
	userID string,
	req FeedbackAuditFilterRequest,
) (*UnifiedFeedbackAuditResponse, common.ErrorCode, error) {
	sourceType := strings.ToLower(strings.TrimSpace(req.SourceType))
	if sourceType == "" {
		sourceType = "all"
	}

	fromTime, err := parseAgentSessionDate(req.FromDate, false)
	if err != nil {
		return nil, common.CodeArgumentError, err
	}
	toTime, err := parseAgentSessionDate(req.ToDate, true)
	if err != nil {
		return nil, common.CodeArgumentError, err
	}

	// 1. Resolve accessible Chat Assistants and Agents for this user
	chatApps := make(map[string]appMetadata)
	agentApps := make(map[string]appMetadata)
	allAppOptions := make([]FeedbackAppOption, 0)

	if sourceType == "all" || sourceType == "chat" {
		chatIDs, cErr := accessibleChatIDs(ctx, s.permissionChecker, userID, permission.OperationRead)
		if cErr == nil && len(chatIDs) > 0 {
			var dialogs []entity.Chat
			q := dao.DB.WithContext(ctx).Where("id IN ? AND status = ?", chatIDs, common.StatusDialogValid)
			if req.AppID != "" && sourceType == "chat" {
				q = q.Where("id = ?", req.AppID)
			}
			if q.Find(&dialogs).Error == nil {
				for _, d := range dialogs {
					name := d.ID
					if d.Name != nil && strings.TrimSpace(*d.Name) != "" {
						name = *d.Name
					}
					cfg := entity.SanitizePublicFeedbackConfig(entity.ExtractFeedbackConfig(d.PromptConfig))
					meta := appMetadata{
						ID:             d.ID,
						Name:           name,
						SourceType:     "chat",
						FeedbackConfig: cfg,
					}
					chatApps[d.ID] = meta
					allAppOptions = append(allAppOptions, FeedbackAppOption{
						ID:             d.ID,
						Name:           name,
						SourceType:     "chat",
						FeedbackConfig: cfg,
					})
				}
			}
		}
	}

	if sourceType == "all" || sourceType == "agent" {
		canvasIDs, aErr := AccessibleCanvasIDs(ctx, permission.Subject{UserID: userID}, permission.OperationRead)
		if aErr == nil && len(canvasIDs) > 0 {
			var canvases []entity.UserCanvas
			q := dao.DB.WithContext(ctx).Where("id IN ?", canvasIDs)
			if req.AppID != "" && sourceType == "agent" {
				q = q.Where("id = ?", req.AppID)
			}
			if q.Find(&canvases).Error == nil {
				for _, c := range canvases {
					name := c.ID
					if c.Title != nil && strings.TrimSpace(*c.Title) != "" {
						name = *c.Title
					}
					cfg := entity.SanitizePublicFeedbackConfig(entity.ExtractFeedbackConfig(c.DSL))
					meta := appMetadata{
						ID:             c.ID,
						Name:           name,
						SourceType:     "agent",
						FeedbackConfig: cfg,
					}
					agentApps[c.ID] = meta
					allAppOptions = append(allAppOptions, FeedbackAppOption{
						ID:             c.ID,
						Name:           name,
						SourceType:     "agent",
						FeedbackConfig: cfg,
					})
				}
			}
		}
	}

	// Filter by specific AppID if provided with sourceType == "all"
	if req.AppID != "" && sourceType == "all" {
		for k := range chatApps {
			if k != req.AppID {
				delete(chatApps, k)
			}
		}
		for k := range agentApps {
			if k != req.AppID {
				delete(agentApps, k)
			}
		}
	}

	var candidateItems []UnifiedFeedbackSessionItem

	// 2. Load Chat Assistant sessions from `conversation` table
	if len(chatApps) > 0 {
		chatIDList := make([]string, 0, len(chatApps))
		for id := range chatApps {
			chatIDList = append(chatIDList, id)
		}
		var convs []*entity.ChatSession
		q := dao.DB.WithContext(ctx).Model(&entity.ChatSession{}).Where("dialog_id IN ?", chatIDList)
		if req.SessionID != "" {
			q = q.Where("id = ?", req.SessionID)
		}
		if req.UserID != "" {
			q = q.Where("user_id = ?", req.UserID)
		}
		if fromTime != nil {
			q = q.Where("update_time >= ?", fromTime.UnixMilli())
		}
		if toTime != nil {
			q = q.Where("update_time <= ?", toTime.UnixMilli())
		}
		if q.Order("update_time DESC").Find(&convs).Error == nil && len(convs) > 0 {
			_ = dao.HydrateChatSessionsForAudit(ctx, dao.DB, convs)
			for _, conv := range convs {
				meta := chatApps[conv.DialogID]
				item := buildUnifiedItemFromChatSession(conv, meta)
				candidateItems = append(candidateItems, item)
			}
		}
	}

	// 3. Load API4Conversation sessions (covers both Agent sessions AND Shared/Widget Chat sessions!)
	apiDialogIDList := make([]string, 0, len(chatApps)+len(agentApps))
	for id := range chatApps {
		apiDialogIDList = append(apiDialogIDList, id)
	}
	for id := range agentApps {
		apiDialogIDList = append(apiDialogIDList, id)
	}
	if len(apiDialogIDList) > 0 {
		var apiConvs []*entity.API4Conversation
		q := dao.DB.WithContext(ctx).Model(&entity.API4Conversation{}).Omit("dsl").Where("dialog_id IN ?", apiDialogIDList)
		if req.SessionID != "" {
			q = q.Where("id = ?", req.SessionID)
		}
		if req.UserID != "" {
			q = q.Where("user_id = ?", req.UserID)
		}
		if fromTime != nil {
			q = q.Where("update_time >= ?", fromTime.UnixMilli())
		}
		if toTime != nil {
			q = q.Where("update_time <= ?", toTime.UnixMilli())
		}
		if q.Order("update_time DESC").Find(&apiConvs).Error == nil && len(apiConvs) > 0 {
			_ = dao.HydrateAPIConversationsForAudit(ctx, dao.DB, apiConvs)
			for _, apiConv := range apiConvs {
				meta, ok := agentApps[apiConv.DialogID]
				if !ok {
					meta = chatApps[apiConv.DialogID]
				}
				item := buildUnifiedItemFromAPI4Session(apiConv, meta)
				candidateItems = append(candidateItems, item)
			}
		}
	}

	// 4. Apply unified feedback & content filters and compute KPI summary simultaneously
	criterionLabels := collectCriterionLabels(chatApps, agentApps)
	filtered := make([]UnifiedFeedbackSessionItem, 0, len(candidateItems))
	for _, item := range candidateItems {
		if matchesFeedbackAuditFilter(item, req) {
			filtered = append(filtered, item)
		}
	}

	kpi := computeFeedbackKPISummary(filtered, criterionLabels)

	// 5. Sort filtered items
	sortUnifiedSessions(filtered, req.OrderBy, req.Desc)

	// 6. Paginate
	total := int64(len(filtered))
	page := req.Page
	if page <= 0 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize <= 0 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	var paged []UnifiedFeedbackSessionItem
	if start < len(filtered) {
		end := start + pageSize
		if end > len(filtered) {
			end = len(filtered)
		}
		paged = filtered[start:end]
	} else {
		paged = []UnifiedFeedbackSessionItem{}
	}

	return &UnifiedFeedbackAuditResponse{
		KPI:      kpi,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
		Sessions: paged,
		Apps:     allAppOptions,
	}, common.CodeSuccess, nil
}

func buildUnifiedItemFromChatSession(conv *entity.ChatSession, meta appMetadata) UnifiedFeedbackSessionItem {
	messages := parseMessages(conv.Message)
	references := parseReferenceList(conv.Reference)
	for index, ref := range references {
		if refMap, ok := ref.(map[string]interface{}); ok {
			refMap["chunks"] = formatReferenceChunks(refMap)
			references[index] = refMap
		}
	}

	// Attach per-turn reference onto assistant messages so AgentLogDetailModal & third-party consumers
	// can read either session.reference OR message.reference seamlessly.
	attachReferencesToMessages(messages, references)

	name := ""
	if conv.Name != nil {
		name = *conv.Name
	}
	if name == "" {
		name = firstUserQuestion(messages)
	}
	userID := ""
	if conv.UserID != nil {
		userID = *conv.UserID
	}
	if strings.TrimSpace(userID) == "" {
		for _, m := range messages {
			if stringValue(m["role"]) != "user" {
				continue
			}
			for _, key := range []string{"sender_display", "sender_username", "sender_name", "sender_id"} {
				if v := strings.TrimSpace(stringValue(m[key])); v != "" {
					userID = v
					break
				}
			}
			if userID != "" {
				break
			}
		}
	}

	defaultChannel := "web"
	if strings.HasPrefix(name, "(channel:") || strings.HasPrefix(name, "channel:") {
		defaultChannel = "telegram"
	}

	summary, roundCount := summarizeSessionMessages(messages, defaultChannel, meta.FeedbackConfig)
	primaryChannel := defaultChannel
	if len(summary.Channels) > 0 {
		primaryChannel = summary.Channels[len(summary.Channels)-1]
	}

	var createTime, updateTime int64
	if conv.CreateTime != nil {
		createTime = *conv.CreateTime
	}
	if conv.UpdateTime != nil {
		updateTime = *conv.UpdateTime
	}
	createDate := formatAuditTime(conv.CreateDate, createTime)
	updateDate := formatAuditTime(conv.UpdateDate, updateTime)

	return UnifiedFeedbackSessionItem{
		ID:              conv.ID,
		SessionID:       conv.ID,
		Name:            name,
		SourceType:      meta.SourceType,
		StorageTable:    "conversation",
		AppID:           conv.DialogID,
		AppName:         meta.Name,
		UserID:          userID,
		Channel:         primaryChannel,
		Round:           roundCount,
		ThumbUp:         summary.PositiveCount,
		FeedbackSummary: summary,
		FeedbackConfig:  meta.FeedbackConfig,
		Messages:        messages,
		Message:         messages,
		Reference:       references,
		CreateTime:      createTime,
		UpdateTime:      updateTime,
		CreateDate:      createDate,
		UpdateDate:      updateDate,
	}
}

func buildUnifiedItemFromAPI4Session(session *entity.API4Conversation, meta appMetadata) UnifiedFeedbackSessionItem {
	normalized := normalizeAgentSession(session, false)
	messages, _ := normalized["message"].([]map[string]interface{})
	references := parseReferenceList(session.Reference)

	name := ""
	if session.Name != nil {
		name = *session.Name
	}
	if name == "" {
		name = firstUserQuestion(messages)
	}

	defaultChannel := "widget"
	if session.Source != nil && strings.TrimSpace(*session.Source) != "" {
		defaultChannel = strings.ToLower(strings.TrimSpace(*session.Source))
	} else if meta.SourceType == "agent" {
		defaultChannel = "web"
	}

	summary, roundCount := summarizeSessionMessages(messages, defaultChannel, meta.FeedbackConfig)
	if session.Round > roundCount {
		roundCount = session.Round
	}
	primaryChannel := defaultChannel
	if len(summary.Channels) > 0 {
		primaryChannel = summary.Channels[len(summary.Channels)-1]
	}

	var createTime, updateTime int64
	if session.CreateTime != nil {
		createTime = *session.CreateTime
	}
	if session.UpdateTime != nil {
		updateTime = *session.UpdateTime
	}
	createDate := formatAuditTime(session.CreateDate, createTime)
	updateDate := formatAuditTime(session.UpdateDate, updateTime)

	return UnifiedFeedbackSessionItem{
		ID:              session.ID,
		SessionID:       session.ID,
		Name:            name,
		SourceType:      meta.SourceType,
		StorageTable:    "api_4_conversation",
		AppID:           session.DialogID,
		AppName:         meta.Name,
		UserID:          session.UserID,
		Channel:         primaryChannel,
		Round:           roundCount,
		ThumbUp:         session.ThumbUp,
		Errors:          session.Errors,
		VersionTitle:    session.VersionTitle,
		FeedbackSummary: summary,
		FeedbackConfig:  meta.FeedbackConfig,
		Messages:        messages,
		Message:         messages,
		Reference:       references,
		CreateTime:      createTime,
		UpdateTime:      updateTime,
		CreateDate:      createDate,
		UpdateDate:      updateDate,
	}
}

func attachReferencesToMessages(messages []map[string]interface{}, references []interface{}) {
	if len(messages) == 0 || len(references) == 0 {
		return
	}
	refIdx := 0
	hasUser := false
	for i, msg := range messages {
		role := stringValue(msg["role"])
		if role == "user" {
			hasUser = true
			continue
		}
		if role == "assistant" && hasUser && refIdx < len(references) {
			if msg["reference"] == nil {
				if refMap, ok := references[refIdx].(map[string]interface{}); ok {
					if chunks, hasChunks := refMap["chunks"]; hasChunks {
						msg["reference"] = chunks
					}
				}
			}
			messages[i] = msg
			refIdx++
		}
	}
}

func firstUserQuestion(messages []map[string]interface{}) string {
	for _, msg := range messages {
		if stringValue(msg["role"]) == "user" {
			content := strings.TrimSpace(stringValue(msg["content"]))
			if content != "" {
				return content
			}
		}
	}
	if len(messages) > 0 {
		return strings.TrimSpace(stringValue(messages[0]["content"]))
	}
	return ""
}

func summarizeSessionMessages(
	messages []map[string]interface{},
	defaultChannel string,
	cfg *entity.FeedbackConfig,
) (SessionFeedbackSummary, int) {
	summary := SessionFeedbackSummary{
		CriteriaValues: make(map[string]any),
	}
	channelSet := make(map[string]bool)
	failedSet := make(map[string]bool)
	var csatSum float64
	var csatCount int
	var minCSAT, maxCSAT int
	roundCount := 0

	for _, msg := range messages {
		role := stringValue(msg["role"])
		if role == "user" {
			roundCount++
			continue
		}
		if role != "assistant" {
			continue
		}

		hasThumb, thumbVal := boolFromAny(msg["thumbup"])
		comment := strings.TrimSpace(stringValue(msg["feedback"]))
		detailMap, _ := msg["feedback_detail"].(map[string]interface{})

		if !hasThumb && comment == "" && len(detailMap) == 0 {
			continue
		}

		summary.HasFeedback = true
		summary.RatedCount++

		if !hasThumb && len(detailMap) > 0 {
			hasThumb, thumbVal = boolFromAny(detailMap["thumbup"])
		}
		if hasThumb {
			if thumbVal {
				summary.PositiveCount++
			} else {
				summary.NegativeCount++
			}
		}

		ch := defaultChannel
		if len(detailMap) > 0 {
			if detailCh := strings.TrimSpace(stringValue(detailMap["channel"])); detailCh != "" {
				ch = strings.ToLower(detailCh)
			}
			if detailComment := strings.TrimSpace(stringValue(detailMap["comment"])); detailComment != "" && comment == "" {
				comment = detailComment
			}
			if ratedAt, ok := int64FromAny(detailMap["rated_at"]); ok && ratedAt >= summary.LatestRatedAt {
				summary.LatestRatedAt = ratedAt
				if comment != "" {
					summary.LatestComment = comment
				}
			}
			if csat, ok := intFromAny(detailMap["csat_score"]); ok && csat >= 1 && csat <= 5 {
				csatSum += float64(csat)
				csatCount++
				if minCSAT == 0 || csat < minCSAT {
					minCSAT = csat
				}
				if csat > maxCSAT {
					maxCSAT = csat
				}
			}
			if criteria, ok := detailMap["criteria"].(map[string]interface{}); ok {
				for k, v := range criteria {
					summary.CriteriaValues[k] = v
					if isBool, bVal := boolFromAny(v); isBool && !bVal {
						failedSet[k] = true
					} else if score, isInt := intFromAny(v); isInt && score >= 1 && score <= 2 {
						failedSet[k] = true
					}
				}
			}
		}
		if comment != "" && summary.LatestComment == "" {
			summary.LatestComment = comment
		}
		if ch != "" && !channelSet[ch] {
			channelSet[ch] = true
			summary.Channels = append(summary.Channels, ch)
		}
	}

	if csatCount > 0 {
		avg := math.Round((csatSum/float64(csatCount))*100) / 100
		summary.AvgCSAT = &avg
		summary.MinCSAT = &minCSAT
		summary.MaxCSAT = &maxCSAT
	}
	for k := range failedSet {
		summary.FailedCriteria = append(summary.FailedCriteria, k)
	}
	sort.Strings(summary.FailedCriteria)
	return summary, roundCount
}

func matchesFeedbackAuditFilter(item UnifiedFeedbackSessionItem, req FeedbackAuditFilterRequest) bool {
	fb := item.FeedbackSummary

	if req.HasFeedback != nil {
		if *req.HasFeedback != fb.HasFeedback {
			return false
		}
	}

	if req.ThumbUp != nil {
		if !fb.HasFeedback {
			return false
		}
		if *req.ThumbUp {
			if fb.PositiveCount == 0 {
				return false
			}
		} else {
			if fb.NegativeCount == 0 {
				return false
			}
		}
	}

	if req.MinCSAT != nil {
		if fb.AvgCSAT == nil || *fb.AvgCSAT < float64(*req.MinCSAT) {
			return false
		}
	}

	if req.MaxCSAT != nil {
		if fb.AvgCSAT == nil || *fb.AvgCSAT > float64(*req.MaxCSAT) {
			return false
		}
	}

	if ch := strings.ToLower(strings.TrimSpace(req.Channel)); ch != "" && ch != "all" {
		matchedCh := strings.EqualFold(item.Channel, ch)
		for _, c := range fb.Channels {
			if strings.EqualFold(c, ch) {
				matchedCh = true
				break
			}
		}
		if !matchedCh {
			return false
		}
	}

	if cKey := strings.TrimSpace(req.CriterionKey); cKey != "" && cKey != "all" {
		val, exists := fb.CriteriaValues[cKey]
		if !exists {
			return false
		}
		cVal := strings.ToLower(strings.TrimSpace(req.CriterionValue))
		if cVal != "" && cVal != "all" {
			if cVal == "failed" {
				isFailed := false
				for _, f := range fb.FailedCriteria {
					if f == cKey {
						isFailed = true
						break
					}
				}
				if !isFailed {
					return false
				}
			} else if isBool, bVal := boolFromAny(val); isBool {
				wantBool := cVal == "true" || cVal == "1"
				if bVal != wantBool {
					return false
				}
			} else if iVal, isInt := intFromAny(val); isInt {
				if strconv.Itoa(iVal) != cVal {
					return false
				}
			}
		}
	}

	if kw := strings.ToLower(strings.TrimSpace(req.Keywords)); kw != "" {
		matched := strings.Contains(strings.ToLower(item.ID), kw) ||
			strings.Contains(strings.ToLower(item.Name), kw) ||
			strings.Contains(strings.ToLower(item.AppName), kw) ||
			strings.Contains(strings.ToLower(item.UserID), kw) ||
			strings.Contains(strings.ToLower(fb.LatestComment), kw)
		if !matched {
			for _, msg := range item.Messages {
				if strings.Contains(strings.ToLower(stringValue(msg["content"])), kw) ||
					strings.Contains(strings.ToLower(stringValue(msg["feedback"])), kw) {
					matched = true
					break
				}
			}
		}
		if !matched {
			return false
		}
	}

	return true
}

func collectCriterionLabels(chatApps, agentApps map[string]appMetadata) map[string]entity.FeedbackCriterionConfig {
	result := make(map[string]entity.FeedbackCriterionConfig)
	for _, c := range entity.DefaultFeedbackCriteria() {
		result[c.Key] = c
	}
	for _, meta := range chatApps {
		if meta.FeedbackConfig != nil {
			for _, c := range meta.FeedbackConfig.Criteria {
				result[c.Key] = c
			}
		}
	}
	for _, meta := range agentApps {
		if meta.FeedbackConfig != nil {
			for _, c := range meta.FeedbackConfig.Criteria {
				result[c.Key] = c
			}
		}
	}
	return result
}

func computeFeedbackKPISummary(
	items []UnifiedFeedbackSessionItem,
	criterionMeta map[string]entity.FeedbackCriterionConfig,
) FeedbackKPISummary {
	kpi := FeedbackKPISummary{
		TotalSessions: len(items),
		CSATDistribution: map[string]int{
			"1": 0, "2": 0, "3": 0, "4": 0, "5": 0,
		},
		ByChannel:         make(map[string]int),
		BySourceType:      map[string]int{"chat": 0, "agent": 0},
		CriteriaBreakdown: make(map[string]CriterionBreakdownStat),
	}

	var csatSum float64
	criteriaScoreSum := make(map[string]float64)
	criteriaScoreCount := make(map[string]int)

	for _, item := range items {
		kpi.BySourceType[item.SourceType]++
		if item.FeedbackSummary.HasFeedback {
			kpi.RatedSessions++
		}

		for _, msg := range item.Messages {
			if stringValue(msg["role"]) != "assistant" {
				continue
			}
			hasThumb, thumbVal := boolFromAny(msg["thumbup"])
			comment := strings.TrimSpace(stringValue(msg["feedback"]))
			detailMap, _ := msg["feedback_detail"].(map[string]interface{})

			if !hasThumb && comment == "" && len(detailMap) == 0 {
				continue
			}

			kpi.RatedMessages++
			if !hasThumb && len(detailMap) > 0 {
				hasThumb, thumbVal = boolFromAny(detailMap["thumbup"])
			}
			if hasThumb {
				if thumbVal {
					kpi.PositiveCount++
				} else {
					kpi.NegativeCount++
				}
			}

			ch := item.Channel
			if len(detailMap) > 0 {
				if detailCh := strings.TrimSpace(stringValue(detailMap["channel"])); detailCh != "" {
					ch = strings.ToLower(detailCh)
				}
				if csat, ok := intFromAny(detailMap["csat_score"]); ok && csat >= 1 && csat <= 5 {
					csatSum += float64(csat)
					kpi.CSATCount++
					kpi.CSATDistribution[strconv.Itoa(csat)]++
				}
				if criteria, ok := detailMap["criteria"].(map[string]interface{}); ok {
					for k, v := range criteria {
						stat, exists := kpi.CriteriaBreakdown[k]
						if !exists {
							meta, hasMeta := criterionMeta[k]
							label := k
							cType := entity.CriterionTypeBinary
							if hasMeta {
								label = meta.Label
								cType = meta.Type
							}
							stat = CriterionBreakdownStat{
								Key:   k,
								Label: label,
								Type:  cType,
							}
						}
						stat.TotalCount++
						if isBool, bVal := boolFromAny(v); isBool {
							if bVal {
								stat.PositiveCount++
							} else {
								stat.NegativeCount++
							}
						} else if score, isInt := intFromAny(v); isInt && score >= 1 && score <= 5 {
							stat.Type = entity.CriterionTypeCSAT5
							if score >= 4 {
								stat.PositiveCount++
							} else if score <= 2 {
								stat.NegativeCount++
							}
							criteriaScoreSum[k] += float64(score)
							criteriaScoreCount[k]++
						}
						kpi.CriteriaBreakdown[k] = stat
					}
				}
			}
			if ch == "" {
				ch = "web"
			}
			kpi.ByChannel[ch]++
		}
	}

	totalVotes := kpi.PositiveCount + kpi.NegativeCount
	if totalVotes > 0 {
		kpi.SatisfactionRate = math.Round((float64(kpi.PositiveCount)*1000)/float64(totalVotes)) / 10
	}
	if kpi.CSATCount > 0 {
		kpi.AvgCSAT = math.Round((csatSum/float64(kpi.CSATCount))*100) / 100
	}
	for k, stat := range kpi.CriteriaBreakdown {
		if cnt := criteriaScoreCount[k]; cnt > 0 {
			stat.AvgScore = math.Round((criteriaScoreSum[k]/float64(cnt))*100) / 100
			kpi.CriteriaBreakdown[k] = stat
		}
	}

	return kpi
}

func sortUnifiedSessions(items []UnifiedFeedbackSessionItem, orderBy string, desc bool) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		var less bool
		switch orderBy {
		case "create_time", "create_date":
			less = a.CreateTime < b.CreateTime
		case "round":
			less = a.Round < b.Round
		case "thumb_up", "rated_count":
			less = a.FeedbackSummary.RatedCount < b.FeedbackSummary.RatedCount
		case "csat_score", "avg_csat":
			aScore := -1.0
			if a.FeedbackSummary.AvgCSAT != nil {
				aScore = *a.FeedbackSummary.AvgCSAT
			}
			bScore := -1.0
			if b.FeedbackSummary.AvgCSAT != nil {
				bScore = *b.FeedbackSummary.AvgCSAT
			}
			less = aScore < bScore
		default:
			less = a.UpdateTime < b.UpdateTime
		}
		if desc {
			return !less
		}
		return less
	})
}

func formatAuditTime(t *time.Time, ms int64) string {
	if t != nil && !t.IsZero() {
		return t.Local().Format("2006-01-02 15:04:05")
	}
	if ms > 0 {
		return time.UnixMilli(ms).Local().Format("2006-01-02 15:04:05")
	}
	return ""
}

func boolFromAny(v interface{}) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return true, val
	case *bool:
		if val != nil {
			return true, *val
		}
	}
	return false, false
}

func intFromAny(v interface{}) (int, bool) {
	switch val := v.(type) {
	case int:
		return val, true
	case int64:
		return int(val), true
	case float64:
		return int(math.Round(val)), true
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return int(i), true
		}
	}
	return 0, false
}

func int64FromAny(v interface{}) (int64, bool) {
	switch val := v.(type) {
	case int64:
		return val, true
	case int:
		return int64(val), true
	case float64:
		return int64(val), true
	case json.Number:
		if i, err := val.Int64(); err == nil {
			return i, true
		}
	}
	return 0, false
}
