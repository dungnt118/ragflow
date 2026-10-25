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

package handler

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"ragflow/internal/common"
	"ragflow/internal/service"
)

func parseFeedbackAuditQuery(c *gin.Context, defaultSourceType, defaultAppID string) service.FeedbackAuditFilterRequest {
	sourceType := strings.TrimSpace(c.Query("source_type"))
	if sourceType == "" {
		sourceType = defaultSourceType
	}
	appID := strings.TrimSpace(c.Query("app_id"))
	if appID == "" {
		appID = defaultAppID
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page <= 0 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize <= 0 {
		pageSize = 20
	}

	orderby := c.DefaultQuery("orderby", "update_time")
	descParam := c.Query("desc")
	desc := descParam != "false" && descParam != "False"

	var hasFeedback *bool
	if v := strings.ToLower(strings.TrimSpace(c.Query("has_feedback"))); v == "true" || v == "1" {
		b := true
		hasFeedback = &b
	} else if v == "false" || v == "0" {
		b := false
		hasFeedback = &b
	}

	var thumbUp *bool
	if v := strings.ToLower(strings.TrimSpace(c.Query("thumb_up"))); v == "true" || v == "1" || v == "positive" {
		b := true
		thumbUp = &b
	} else if v == "false" || v == "0" || v == "negative" {
		b := false
		thumbUp = &b
	}

	var minCSAT, maxCSAT *int
	if v, err := strconv.Atoi(strings.TrimSpace(c.Query("min_csat"))); err == nil && v >= 1 && v <= 5 {
		minCSAT = &v
	}
	if v, err := strconv.Atoi(strings.TrimSpace(c.Query("max_csat"))); err == nil && v >= 1 && v <= 5 {
		maxCSAT = &v
	}

	return service.FeedbackAuditFilterRequest{
		SourceType:     sourceType,
		AppID:          appID,
		SessionID:      strings.TrimSpace(c.Query("id")),
		UserID:         strings.TrimSpace(c.Query("user_id")),
		HasFeedback:    hasFeedback,
		ThumbUp:        thumbUp,
		MinCSAT:        minCSAT,
		MaxCSAT:        maxCSAT,
		Channel:        strings.TrimSpace(c.Query("channel")),
		CriterionKey:   strings.TrimSpace(c.Query("criterion_key")),
		CriterionValue: strings.TrimSpace(c.Query("criterion_value")),
		Keywords:       strings.TrimSpace(c.Query("keywords")),
		FromDate:       strings.TrimSpace(c.Query("from_date")),
		ToDate:         strings.TrimSpace(c.Query("to_date")),
		OrderBy:        orderby,
		Desc:           desc,
		Page:           page,
		PageSize:       pageSize,
	}
}

// ListUnifiedFeedbackSessions GET /api/v1/feedback/sessions
func (h *ChatSessionHandler) ListUnifiedFeedbackSessions(c *gin.Context) {
	user, code, msg := GetUser(c)
	if code != common.CodeSuccess {
		common.ResponseWithCodeData(c, code, nil, msg)
		return
	}
	req := parseFeedbackAuditQuery(c, "all", "")
	auditSvc := service.NewFeedbackAuditService()
	resp, errCode, err := auditSvc.ListFeedbackSessions(c.Request.Context(), user.ID, req)
	if err != nil {
		common.ErrorWithCode(c, errCode, err.Error())
		return
	}
	common.SuccessWithData(c, resp, "success")
}

// ListChatFeedbackSessions GET /api/v1/chats/:chat_id/feedback-sessions
func (h *ChatSessionHandler) ListChatFeedbackSessions(c *gin.Context) {
	user, code, msg := GetUser(c)
	if code != common.CodeSuccess {
		common.ResponseWithCodeData(c, code, nil, msg)
		return
	}
	chatID := c.Param("chat_id")
	req := parseFeedbackAuditQuery(c, "chat", chatID)
	auditSvc := service.NewFeedbackAuditService()
	resp, errCode, err := auditSvc.ListFeedbackSessions(c.Request.Context(), user.ID, req)
	if err != nil {
		common.ErrorWithCode(c, errCode, err.Error())
		return
	}
	common.SuccessWithData(c, resp, "success")
}

// ListAgentFeedbackSessions GET /api/v1/agents/:canvas_id/feedback-sessions
func (h *AgentHandler) ListAgentFeedbackSessions(c *gin.Context) {
	user, code, msg := GetUser(c)
	if code != common.CodeSuccess {
		common.ResponseWithCodeData(c, code, nil, msg)
		return
	}
	canvasID := c.Param("canvas_id")
	req := parseFeedbackAuditQuery(c, "agent", canvasID)
	auditSvc := service.NewFeedbackAuditService()
	resp, errCode, err := auditSvc.ListFeedbackSessions(c.Request.Context(), user.ID, req)
	if err != nil {
		common.ErrorWithCode(c, errCode, err.Error())
		return
	}
	common.SuccessWithData(c, resp, "success")
}
