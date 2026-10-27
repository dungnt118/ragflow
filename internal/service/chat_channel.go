// Copyright 2026 The InfiniFlow Authors. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"go.uber.org/zap"

	"gorm.io/gorm"
	"ragflow/internal/agent/canvas"
	"ragflow/internal/channels/core"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

// ChatChannelDataError represents a client/domain validation error on chat-channel operations (maps to CodeDataError).
type ChatChannelDataError struct {
	Msg string
}

func (e *ChatChannelDataError) Error() string {
	return e.Msg
}

type ChatChannelService struct {
	chatChannelDAO      *dao.ChatChannelDAO
	chatDAO             *dao.ChatDAO
	userCanvasDAO       *dao.UserCanvasDAO
	api4ConversationDAO *dao.API4ConversationDAO
	chatSessionSvc      *ChatSessionService
	agentSvc            *AgentService
}

var (
	chatChannelAgentServiceMu sync.RWMutex
	chatChannelAgentService   *AgentService
)

// SetChatChannelAgentService installs the shared AgentService used by chat-channel message handlers.
func SetChatChannelAgentService(svc *AgentService) {
	chatChannelAgentServiceMu.Lock()
	defer chatChannelAgentServiceMu.Unlock()
	chatChannelAgentService = svc
}

func getChatChannelAgentService() *AgentService {
	chatChannelAgentServiceMu.RLock()
	defer chatChannelAgentServiceMu.RUnlock()
	return chatChannelAgentService
}

func NewChatChannelService() *ChatChannelService {
	agentSvc := getChatChannelAgentService()
	if agentSvc == nil {
		agentSvc = NewAgentService()
	}
	return &ChatChannelService{
		chatChannelDAO:      dao.NewChatChannel(),
		chatDAO:             dao.NewChatDAO(),
		userCanvasDAO:       dao.NewUserCanvasDAO(),
		api4ConversationDAO: dao.NewAPI4ConversationDAO(),
		chatSessionSvc:      NewChatSessionService(),
		agentSvc:            agentSvc,
	}
}

type ChatChannelIncomingMessage struct {
	Channel          string
	AccountID        string
	ChatID           string
	ChatType         string
	ChatTitle        string
	MessageID        string
	ThreadID         string
	SenderID         string
	SenderName       string
	SenderUsername   string
	ReplyToMessageID string
	ReplyToSender    string
	ReplyToText      string
	ReplyToIsBot     bool
	QuoteText        string
	Text             string
}

type ChatChannelOutgoingReply struct {
	Answer        string
	MessageID     string
	InlineButtons [][]core.InlineButton
}

// ChatChannelRuntimeProvider reads runtime metadata for one live chat-channel instance.
type ChatChannelRuntimeProvider func(channelID string) (map[string]any, bool)

var (
	chatChannelRuntimeProviderMu sync.RWMutex
	chatChannelRuntimeProvider   ChatChannelRuntimeProvider
)

// SetChatChannelRuntimeProvider installs the runtime snapshot reader used by the chat-channel API.
func SetChatChannelRuntimeProvider(provider ChatChannelRuntimeProvider) {
	chatChannelRuntimeProviderMu.Lock()
	defer chatChannelRuntimeProviderMu.Unlock()
	chatChannelRuntimeProvider = provider
}

// getChatChannelRuntimeProvider returns the current runtime snapshot reader.
func getChatChannelRuntimeProvider() ChatChannelRuntimeProvider {
	chatChannelRuntimeProviderMu.RLock()
	defer chatChannelRuntimeProviderMu.RUnlock()
	return chatChannelRuntimeProvider
}

func (s *ChatChannelService) List(ctx context.Context, userID string) ([]*entity.ChatChannelListResponse, error) {
	channelIDs, err := accessibleChatChannelIDs(ctx, userID, permission.OperationRead)
	if err != nil {
		return nil, err
	}
	return s.chatChannelDAO.ListByIDs(ctx, dao.DB, channelIDs)
}

func normalizeOptionalTargetID(id *string) *string {
	if id == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*id)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func (s *ChatChannelService) validateChannelAgentTarget(
	ctx context.Context,
	userID, tenantID, agentID string,
	operation permission.Operation,
) (*entity.UserCanvas, common.ErrorCode, error) {
	if err := CheckCanvasPermission(ctx, permission.Subject{UserID: userID}, agentID, operation); err != nil {
		code, normalizedErr := normalizeChatChannelPermissionError(err)
		return nil, code, normalizedErr
	}
	if tenantID != "" && tenantID != userID {
		if err := CheckCanvasPermission(ctx, permission.Subject{UserID: tenantID}, agentID, operation); err != nil {
			code, normalizedErr := normalizeChatChannelPermissionError(err)
			return nil, code, normalizedErr
		}
	}
	canvasRow, err := s.userCanvasDAO.GetByID(ctx, dao.DB, agentID)
	if err != nil {
		if dao.IsNotFoundErr(err) || errors.Is(err, dao.ErrUserCanvasNotFound) {
			code, permissionErr := normalizeChatChannelPermissionError(permission.ErrResourceNotFound)
			return nil, code, permissionErr
		}
		return nil, common.CodeServerError, err
	}
	if canvasRow == nil {
		code, permissionErr := normalizeChatChannelPermissionError(permission.ErrResourceNotFound)
		return nil, code, permissionErr
	}
	if strings.TrimSpace(canvasRow.CanvasCategory) != "" && canvasRow.CanvasCategory != "agent_canvas" {
		return nil, common.CodeDataError, &ChatChannelDataError{Msg: "Can't find this Agent!"}
	}
	return canvasRow, common.CodeSuccess, nil
}

func (s *ChatChannelService) CreateChatChannel(ctx context.Context, userID, name, channelType string, config entity.JSONMap, chatID, agentID *string) (*entity.ChatChannel, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, permission.ErrUnauthenticated
	}
	tenantID, err := requireCurrentTenantID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if err := checkFeatureAction(ctx, userID, "chat", permission.ActionWrite); err != nil {
		return nil, err
	}
	chatID = normalizeOptionalTargetID(chatID)
	agentID = normalizeOptionalTargetID(agentID)
	if chatID != nil && agentID != nil {
		return nil, &ChatChannelDataError{Msg: "Choose either a chat assistant or an Agent, not both."}
	}
	if chatID != nil {
		if err := checkChatPermission(ctx, nil, userID, *chatID, permission.OperationUse); err != nil {
			return nil, err
		}
	}
	if agentID != nil {
		if _, _, err := s.validateChannelAgentTarget(ctx, userID, tenantID, *agentID, permission.OperationUse); err != nil {
			return nil, err
		}
	}
	row := &entity.ChatChannel{
		ID:       common.GenerateUUID(),
		TenantID: tenantID,
		Name:     name,
		Channel:  channelType,
		Config:   config,
		ChatID:   chatID,
		AgentID:  agentID,
		Status:   1,
	}

	if err := dao.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.chatChannelDAO.Create(ctx, tx, row); err != nil {
			return err
		}
		return grantCreatedResourceOwner(ctx, tx, userID, tenantID, permission.ResourceKindChatChannel, row.ID)
	}); err != nil {
		return nil, err
	}

	created, err := s.chatChannelDAO.GetByID(ctx, dao.DB, row.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load created chat channel: %w", err)
	}
	return created, nil
}

func (s *ChatChannelService) GetChatChannel(ctx context.Context, userID, channelID string) (*entity.ChatChannel, common.ErrorCode, error) {
	if err := checkChatChannelPermission(ctx, userID, channelID, permission.OperationRead); err != nil {
		code, permissionErr := normalizeChatChannelPermissionError(err)
		return nil, code, permissionErr
	}

	channel, err := s.chatChannelDAO.GetByID(ctx, dao.DB, channelID)
	if err != nil {
		if dao.IsNotFoundErr(err) {
			code, permissionErr := normalizeChatChannelPermissionError(permission.ErrResourceNotFound)
			return nil, code, permissionErr
		}
		return nil, common.CodeServerError, err
	}
	return channel, common.CodeSuccess, nil
}

func (s *ChatChannelService) UpdateChatChannel(ctx context.Context, userID, channelID string, req map[string]interface{}) (*entity.ChatChannel, common.ErrorCode, error) {
	if err := checkChatChannelPermission(ctx, userID, channelID, permission.OperationUpdate); err != nil {
		code, permissionErr := normalizeChatChannelPermissionError(err)
		return nil, code, permissionErr
	}
	channel, err := s.chatChannelDAO.GetByID(ctx, dao.DB, channelID)
	if err != nil {
		if dao.IsNotFoundErr(err) {
			code, permissionErr := normalizeChatChannelPermissionError(permission.ErrResourceNotFound)
			return nil, code, permissionErr
		}
		return nil, common.CodeServerError, err
	}

	updates := map[string]interface{}{}

	if value, exists := req["name"]; exists {
		name, ok := value.(string)
		if !ok {
			return nil, common.CodeDataError, errors.New("name must be string")
		}
		updates["name"] = name
	}

	if value, exists := req["config"]; exists {
		if value == nil {
			updates["config"] = nil
		} else {
			config, ok := value.(map[string]interface{})
			if !ok {
				return nil, common.CodeDataError, errors.New("config must be object")
			}
			updates["config"] = entity.JSONMap(config)
		}
	}

	chatValue, hasChatKey := req["chat_id"]
	agentValue, hasAgentKey := req["agent_id"]

	var parsedChatID *string
	if hasChatKey && chatValue != nil {
		chatIDStr, ok := chatValue.(string)
		if !ok {
			return nil, common.CodeDataError, errors.New("chat_id must be string or null")
		}
		parsedChatID = normalizeOptionalTargetID(&chatIDStr)
	}

	var parsedAgentID *string
	if hasAgentKey && agentValue != nil {
		agentIDStr, ok := agentValue.(string)
		if !ok {
			return nil, common.CodeDataError, errors.New("agent_id must be string or null")
		}
		parsedAgentID = normalizeOptionalTargetID(&agentIDStr)
	}

	if parsedChatID != nil && parsedAgentID != nil {
		return nil, common.CodeDataError, errors.New("Choose either a chat assistant or an Agent, not both.")
	}

	if hasChatKey {
		if parsedChatID == nil {
			updates["chat_id"] = nil
		} else {
			chatID := *parsedChatID
			if err := checkChatPermission(ctx, nil, userID, chatID, permission.OperationUse); err != nil {
				code, normalizedErr := normalizeChatPermissionError(err)
				return nil, code, normalizedErr
			}
			if channel.TenantID != userID {
				if err := checkChatPermission(ctx, nil, channel.TenantID, chatID, permission.OperationUse); err != nil {
					code, normalizedErr := normalizeChatPermissionError(err)
					return nil, code, normalizedErr
				}
			}
			updates["chat_id"] = chatID
			updates["agent_id"] = nil
		}
	}

	if hasAgentKey {
		if parsedAgentID == nil {
			if parsedChatID == nil {
				updates["agent_id"] = nil
			}
		} else {
			agentID := *parsedAgentID
			if _, code, err := s.validateChannelAgentTarget(ctx, userID, channel.TenantID, agentID, permission.OperationUse); err != nil {
				return nil, code, err
			}
			updates["agent_id"] = agentID
			updates["chat_id"] = nil
		}
	}

	if len(updates) > 0 {
		if err = s.chatChannelDAO.UpdateByID(ctx, dao.DB, channelID, channel.TenantID, updates); err != nil {
			return nil, common.CodeDataError, err
		}
	}

	updated, err := s.chatChannelDAO.GetByID(ctx, dao.DB, channelID)
	if err != nil {
		if dao.IsNotFoundErr(err) {
			code, permissionErr := normalizeChatChannelPermissionError(permission.ErrResourceNotFound)
			return nil, code, permissionErr
		}
		return nil, common.CodeServerError, err
	}
	return updated, common.CodeSuccess, nil
}

// GetChatChannelRuntime returns the live runtime metadata for a WhatsApp chat channel.
func (s *ChatChannelService) GetChatChannelRuntime(ctx context.Context, userID, channelID string) (map[string]any, common.ErrorCode, error) {
	if err := checkChatChannelPermission(ctx, userID, channelID, permission.OperationUse); err != nil {
		code, permissionErr := normalizeChatChannelPermissionError(err)
		return nil, code, permissionErr
	}
	conn, err := s.chatChannelDAO.GetByID(ctx, dao.DB, channelID)
	if err != nil {
		if dao.IsNotFoundErr(err) {
			code, permissionErr := normalizeChatChannelPermissionError(permission.ErrResourceNotFound)
			return nil, code, permissionErr
		}
		return nil, common.CodeServerError, err
	}

	if conn.Channel != "whatsapp" {
		return nil, common.CodeDataError, errors.New("runtime snapshot is only available for WhatsApp")
	}

	if provider := getChatChannelRuntimeProvider(); provider != nil {
		if snapshot, ok := provider(channelID); ok {
			return snapshot, common.CodeSuccess, nil
		}
	}
	return waitingRuntimeSnapshotMap(channelID), common.CodeSuccess, nil
}

// HandleIncomingMessage routes one external channel message through the bound RAGFlow dialog.
func (s *ChatChannelService) HandleIncomingMessage(ctx context.Context, msg ChatChannelIncomingMessage) (string, error) {
	reply, err := s.HandleIncomingChannelMessage(ctx, msg)
	if err != nil || reply == nil {
		return "", err
	}
	return reply.Answer, nil
}

// HandleIncomingChannelMessage routes one external channel message and returns both text and optional inline rating buttons.
func (s *ChatChannelService) HandleIncomingChannelMessage(ctx context.Context, msg ChatChannelIncomingMessage) (*ChatChannelOutgoingReply, error) {
	if strings.TrimSpace(msg.Text) == "" {
		return nil, nil
	}

	channel, err := s.chatChannelDAO.GetByID(ctx, dao.DB, msg.AccountID)
	if err != nil {
		if dao.IsNotFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}
	if channel.AgentID != nil && strings.TrimSpace(*channel.AgentID) != "" {
		return s.handleIncomingAgentMessage(ctx, channel, msg)
	}
	if channel.ChatID == nil || strings.TrimSpace(*channel.ChatID) == "" {
		return nil, nil
	}

	dialog, err := s.chatDAO.GetByID(ctx, dao.DB, *channel.ChatID)
	if err != nil {
		if dao.IsNotFoundErr(err) {
			return nil, nil
		}
		return nil, err
	}

	senderDisplay := formatChannelSenderDisplay(msg.SenderUsername, msg.SenderName, msg.SenderID, msg.ChatID)
	channelLabel := formatChannelSessionLabel(channel.Name, msg.ChatType, msg.ChatTitle, senderDisplay)
	session, err := s.resolveOrCreateChannelSession(ctx, dialog.ID, channel.ID, channelLabel, senderDisplay, msg)
	if err != nil {
		return nil, err
	}

	existingMsgs := parseMessages(session.Message)
	effectiveText := buildContextualChannelQuery(msg, existingMsgs)
	userMsg := buildChannelUserMessagePayload(msg, effectiveText, senderDisplay)

	result, err := s.chatSessionSvc.ChatCompletions(
		ctx,
		dialog.TenantID,
		dialog.ID,
		session.ID,
		[]map[string]interface{}{userMsg},
		"",
		nil,
		"",
		nil,
		map[string]interface{}{
			"quote":            false,
			"refine_multiturn": true,
		},
		false,
		false,
		nil,
	)
	if err != nil {
		common.Error("chat channel completion failed", err, zap.String("channel_id", channel.ID))
		return &ChatChannelOutgoingReply{Answer: "Sorry, I couldn't process your message right now."}, nil
	}
	if result == nil {
		return nil, nil
	}
	answer, _ := result["answer"].(string)
	replyMsgID, _ := result["id"].(string)
	if replyMsgID != "" {
		rememberChannelReplySession(replyMsgID, session.ID)
	}
	var inlineButtons [][]core.InlineButton
	feedbackCfg := entity.ExtractFeedbackConfig(dialog.PromptConfig)
	if feedbackCfg != nil && feedbackCfg.Enabled && replyMsgID != "" {
		inlineButtons = BuildTelegramRatingButtons(replyMsgID, feedbackCfg, 0)
	}
	return &ChatChannelOutgoingReply{
		Answer:        answer,
		MessageID:     replyMsgID,
		InlineButtons: inlineButtons,
	}, nil
}

func (s *ChatChannelService) handleIncomingAgentMessage(
	ctx context.Context,
	channel *entity.ChatChannel,
	msg ChatChannelIncomingMessage,
) (*ChatChannelOutgoingReply, error) {
	if channel == nil || channel.AgentID == nil || strings.TrimSpace(*channel.AgentID) == "" {
		return nil, nil
	}
	agentID := strings.TrimSpace(*channel.AgentID)
	canvasRow, _, err := s.validateChannelAgentTarget(ctx, channel.TenantID, channel.TenantID, agentID, permission.OperationRun)
	if err != nil || canvasRow == nil {
		if err != nil && !errors.Is(err, permission.ErrResourceNotFound) && !errors.Is(err, permission.ErrPermissionDenied) {
			common.Warn("chat channel agent validation failed", zap.String("channel_id", channel.ID), zap.String("agent_id", agentID), zap.Error(err))
		}
		return nil, nil
	}

	senderDisplay := formatChannelSenderDisplay(msg.SenderUsername, msg.SenderName, msg.SenderID, msg.ChatID)
	channelLabel := formatChannelSessionLabel(channel.Name, msg.ChatType, msg.ChatTitle, senderDisplay)
	sessionID, existingSession, err := s.resolveOrCreateAgentChannelSession(ctx, canvasRow.ID, channel.ID, msg)
	if err != nil {
		return nil, err
	}

	var existingMsgs []map[string]interface{}
	var pendingInputs map[string]any
	if existingSession != nil {
		existingMsgs = parseMessages(existingSession.Message)
		pendingInputs = extractPendingAgentChannelInputs(existingSession.DSL)
	}
	effectiveText := buildContextualChannelQuery(msg, existingMsgs)
	var userInput any = effectiveText
	if len(pendingInputs) > 0 {
		userInput = buildAgentChannelResumeInput(effectiveText, pendingInputs)
	}

	agentSvc := s.agentSvc
	if agentSvc == nil {
		agentSvc = getChatChannelAgentService()
	}
	if agentSvc == nil {
		agentSvc = NewAgentService()
	}

	runCtx := WithAgentSessionID(ctx, sessionID)
	events, err := agentSvc.RunAgent(runCtx, channel.TenantID, canvasRow.ID, sessionID, "", userInput, nil)
	if err != nil {
		common.Error("chat channel agent completion failed", err, zap.String("channel_id", channel.ID), zap.String("agent_id", canvasRow.ID))
		return &ChatChannelOutgoingReply{Answer: "Sorry, I couldn't process your message right now."}, nil
	}

	var (
		fullContent     string
		waitingPrompt   string
		waitingInputs   map[string]any
		pausedForInput  bool
		replyMsgID      string
		failedDuringRun bool
	)
	for ev := range events {
		if ev.MessageID != "" {
			replyMsgID = ev.MessageID
		}
		switch ev.Type {
		case "message":
			var me canvas.MessageEvent
			if json.Unmarshal([]byte(ev.Data), &me) == nil {
				fullContent += me.Content
			}
		case "waiting_for_user":
			pausedForInput = true
			var waitEv canvas.WaitingForUserEvent
			if json.Unmarshal([]byte(ev.Data), &waitEv) == nil {
				waitingPrompt = formatWaitingForUserPrompt(waitEv)
				if len(waitEv.Inputs) > 0 {
					waitingInputs = waitEv.Inputs
				}
			}
		case "user_inputs":
			pausedForInput = true
			var uiData map[string]any
			if json.Unmarshal([]byte(ev.Data), &uiData) == nil {
				waitingPrompt = formatLegacyUserInputsPrompt(uiData)
				if inputs, ok := uiData["inputs"].(map[string]any); ok && len(inputs) > 0 {
					waitingInputs = inputs
				}
			}
		case "error":
			failedDuringRun = true
		}
	}

	answer := strings.TrimSpace(fullContent)
	if answer == "" && strings.TrimSpace(waitingPrompt) != "" {
		answer = strings.TrimSpace(waitingPrompt)
	}
	if answer == "" && failedDuringRun {
		return &ChatChannelOutgoingReply{Answer: "Sorry, I couldn't process your message right now."}, nil
	}

	s.updateAgentChannelSessionMetadata(
		ctx,
		canvasRow.ID,
		sessionID,
		existingSession == nil,
		channelLabel,
		senderDisplay,
		msg,
		replyMsgID,
		pausedForInput,
		waitingInputs,
	)

	if replyMsgID != "" {
		rememberChannelReplySession(replyMsgID, sessionID)
	}
	var inlineButtons [][]core.InlineButton
	feedbackCfg := entity.ExtractFeedbackConfig(map[string]any(canvasRow.DSL))
	if feedbackCfg != nil && feedbackCfg.Enabled && replyMsgID != "" {
		inlineButtons = BuildTelegramRatingButtons(replyMsgID, feedbackCfg, 0)
	}
	return &ChatChannelOutgoingReply{
		Answer:        answer,
		MessageID:     replyMsgID,
		InlineButtons: inlineButtons,
	}, nil
}

// BuildTelegramRatingButtons constructs compact inline buttons whose callback_data stays <= 41 bytes (< 64-byte Telegram limit).
// Format: "fb:<message_id_32>:up", "fb:<message_id_32>:dn", or "fb:<message_id_32>:c<idx>:<val>".
func BuildTelegramRatingButtons(msgID string, cfg *entity.FeedbackConfig, criterionIdx int) [][]core.InlineButton {
	if cfg == nil || !cfg.Enabled || msgID == "" {
		return nil
	}
	if cfg.Mode == entity.FeedbackModeMultiCriteria && len(cfg.Criteria) > 0 {
		if criterionIdx < 0 || criterionIdx >= len(cfg.Criteria) {
			return nil
		}
		crit := cfg.Criteria[criterionIdx]
		if crit.Type == entity.CriterionTypeBinary {
			return [][]core.InlineButton{
				{
					{Text: fmt.Sprintf("👍 %s: Đúng", crit.Label), CallbackData: fmt.Sprintf("fb:%s:c%d:1", msgID, criterionIdx)},
					{Text: "👎 Chưa chính xác", CallbackData: fmt.Sprintf("fb:%s:c%d:0", msgID, criterionIdx)},
				},
			}
		}
		row := make([]core.InlineButton, 0, 5)
		for score := 1; score <= 5; score++ {
			row = append(row, core.InlineButton{
				Text:         fmt.Sprintf("%d⭐", score),
				CallbackData: fmt.Sprintf("fb:%s:c%d:%d", msgID, criterionIdx, score),
			})
		}
		return [][]core.InlineButton{row}
	}

	return [][]core.InlineButton{
		{
			{Text: "👍 Hữu ích", CallbackData: fmt.Sprintf("fb:%s:up", msgID)},
			{Text: "👎 Chưa tốt", CallbackData: fmt.Sprintf("fb:%s:dn", msgID)},
		},
	}
}

// HandleCallbackQuery processes an inline rating button tap from Telegram and updates the message feedback state.
func (s *ChatChannelService) HandleCallbackQuery(ctx context.Context, cb core.CallbackQuery) (*core.CallbackResult, error) {
	if !strings.HasPrefix(cb.Data, "fb:") {
		return &core.CallbackResult{ToastText: "Thao tác không hợp lệ."}, nil
	}
	parts := strings.Split(cb.Data, ":")
	if len(parts) < 3 {
		return &core.CallbackResult{ToastText: "Dữ liệu đánh giá không hợp lệ."}, nil
	}
	msgID := parts[1]
	action := parts[2]
	if action == "done" {
		return &core.CallbackResult{ToastText: "Bạn đã hoàn tất đánh giá tin nhắn này."}, nil
	}

	channel, err := s.chatChannelDAO.GetByID(ctx, dao.DB, cb.AccountID)
	if err != nil || channel == nil {
		return nil, errors.New("chat channel not found")
	}
	if channel.AgentID != nil && strings.TrimSpace(*channel.AgentID) != "" {
		return s.handleAgentCallbackQuery(ctx, channel, strings.TrimSpace(*channel.AgentID), msgID, action, parts, cb)
	}
	if channel.ChatID == nil || strings.TrimSpace(*channel.ChatID) == "" {
		return nil, errors.New("chat channel not bound to dialog")
	}
	dialog, err := s.chatDAO.GetByID(ctx, dao.DB, *channel.ChatID)
	if err != nil || dialog == nil {
		return nil, errors.New("dialog not found")
	}

	// Resolve session_id from the rated assistant message ID (supporting group reply-thread sessions),
	// falling back to the deterministic chat-level session_id.
	sessionID := s.resolveChannelCallbackSessionID(ctx, dialog.ID, channel.ID, cb.ChatID, msgID)
	feedbackCfg := entity.ExtractFeedbackConfig(dialog.PromptConfig)

	if action == "up" || action == "dn" {
		thumb := action == "up"
		req := map[string]interface{}{
			"thumbup": thumb,
			"channel": "telegram",
		}
		if _, _, uErr := s.chatSessionSvc.UpdateMessageFeedback(ctx, dialog.TenantID, dialog.ID, sessionID, msgID, req); uErr != nil {
			return nil, uErr
		}
		label := "✅ Đã đánh giá: 👍 Hữu ích"
		if !thumb {
			label = "✅ Đã đánh giá: 👎 Chưa tốt"
		}
		return &core.CallbackResult{
			ToastText: "Cảm ơn đánh giá của bạn!",
			InlineButtons: [][]core.InlineButton{
				{{Text: label, CallbackData: fmt.Sprintf("fb:%s:done", msgID)}},
			},
		}, nil
	}

	if strings.HasPrefix(action, "c") && len(parts) >= 4 {
		idx, idxErr := strconv.Atoi(strings.TrimPrefix(action, "c"))
		valInt, valErr := strconv.Atoi(parts[3])
		if idxErr != nil || valErr != nil || feedbackCfg == nil || idx < 0 || idx >= len(feedbackCfg.Criteria) {
			return &core.CallbackResult{ToastText: "Tiêu chí đánh giá không hợp lệ."}, nil
		}

		// Load existing criteria if user already completed earlier steps in this message.
		criteriaMap := make(map[string]interface{})
		if existingSession, sErr := s.chatSessionSvc.chatSessionDAO.GetByID(ctx, dao.DB, sessionID); sErr == nil && existingSession != nil {
			for _, m := range parseMessages(existingSession.Message) {
				if stringValue(m["id"]) == msgID && stringValue(m["role"]) == "assistant" {
					if detail, ok := m["feedback_detail"].(map[string]interface{}); ok {
						if prevCrit, ok := detail["criteria"].(map[string]interface{}); ok {
							for k, v := range prevCrit {
								criteriaMap[k] = v
							}
						}
					}
					break
				}
			}
		}

		crit := feedbackCfg.Criteria[idx]
		if crit.Type == entity.CriterionTypeBinary {
			criteriaMap[crit.Key] = valInt == 1
		} else {
			criteriaMap[crit.Key] = valInt
		}

		nextIdx := idx + 1
		req := map[string]interface{}{
			"criteria":      criteriaMap,
			"channel":       "telegram",
			"telegram_step": nextIdx,
		}
		if _, _, uErr := s.chatSessionSvc.UpdateMessageFeedback(ctx, dialog.TenantID, dialog.ID, sessionID, msgID, req); uErr != nil {
			return nil, uErr
		}

		if nextIdx < len(feedbackCfg.Criteria) {
			nextCrit := feedbackCfg.Criteria[nextIdx]
			return &core.CallbackResult{
				ToastText:     fmt.Sprintf("Đã lưu (%d/%d). Tiếp theo: %s", nextIdx, len(feedbackCfg.Criteria), nextCrit.Label),
				InlineButtons: BuildTelegramRatingButtons(msgID, feedbackCfg, nextIdx),
			}, nil
		}

		return &core.CallbackResult{
			ToastText: "Cảm ơn bạn đã hoàn tất đánh giá!",
			InlineButtons: [][]core.InlineButton{
				{{Text: "✅ Đã ghi nhận đánh giá", CallbackData: fmt.Sprintf("fb:%s:done", msgID)}},
			},
		}, nil
	}

	return &core.CallbackResult{ToastText: "Đã ghi nhận!"}, nil
}

func (s *ChatChannelService) DeleteChatChannel(ctx context.Context, userID, channelID string) (bool, common.ErrorCode, error) {
	if err := checkChatChannelPermission(ctx, userID, channelID, permission.OperationDelete); err != nil {
		code, permissionErr := normalizeChatChannelPermissionError(err)
		return false, code, permissionErr
	}
	channel, err := s.chatChannelDAO.GetByID(ctx, dao.DB, channelID)
	if err != nil {
		if dao.IsNotFoundErr(err) {
			code, permissionErr := normalizeChatChannelPermissionError(permission.ErrResourceNotFound)
			return false, code, permissionErr
		}
		return false, common.CodeServerError, err
	}

	if err = s.chatChannelDAO.DeleteByID(ctx, dao.DB, channelID, channel.TenantID); err != nil {
		return false, common.CodeDataError, err
	}
	return true, common.CodeSuccess, nil
}

// waitingRuntimeSnapshotMap returns the API fallback payload before a runtime instance starts.
func waitingRuntimeSnapshotMap(channelID string) map[string]any {
	return map[string]any{
		"account_id":       channelID,
		"session_key":      channelID,
		"status":           "waiting",
		"connected_at":     nil,
		"qr_updated_at":    nil,
		"qr_data_url":      nil,
		"last_error":       nil,
		"session_id":       nil,
		"last_snapshot_at": nil,
		"gateway_base_url": nil,
		"event_cursor":     int64(0),
	}
}

var (
	channelReplySessionMu    sync.RWMutex
	channelReplySessionByMsg = make(map[string]string)
	channelReplySessionOrder []string
)

const maxChannelReplySessionCache = 10000

func rememberChannelReplySession(messageID, sessionID string) {
	messageID = strings.TrimSpace(messageID)
	sessionID = strings.TrimSpace(sessionID)
	if messageID == "" || sessionID == "" {
		return
	}
	channelReplySessionMu.Lock()
	defer channelReplySessionMu.Unlock()
	if _, exists := channelReplySessionByMsg[messageID]; !exists {
		if len(channelReplySessionOrder) >= maxChannelReplySessionCache {
			evict := maxChannelReplySessionCache / 10
			if evict < 1 {
				evict = 1
			}
			for _, oldKey := range channelReplySessionOrder[:evict] {
				delete(channelReplySessionByMsg, oldKey)
			}
			channelReplySessionOrder = channelReplySessionOrder[evict:]
		}
		channelReplySessionOrder = append(channelReplySessionOrder, messageID)
	}
	channelReplySessionByMsg[messageID] = sessionID
}

func lookupChannelReplySession(messageID string) string {
	channelReplySessionMu.RLock()
	defer channelReplySessionMu.RUnlock()
	return channelReplySessionByMsg[strings.TrimSpace(messageID)]
}

func (s *ChatChannelService) resolveChannelCallbackSessionID(ctx context.Context, dialogID, channelID, chatID, msgID string) string {
	if cached := lookupChannelReplySession(msgID); cached != "" {
		return cached
	}
	if dao.DB != nil && strings.TrimSpace(msgID) != "" {
		var convID string
		err := dao.DB.WithContext(ctx).
			Table("conversation_message AS cm").
			Select("cm.conversation_id").
			Joins("JOIN conversation AS c ON c.id = cm.conversation_id").
			Where("c.dialog_id = ? AND cm.message_id = ? AND cm.role = ?", dialogID, msgID, "assistant").
			Limit(1).
			Scan(&convID).Error
		if err == nil && strings.TrimSpace(convID) != "" {
			rememberChannelReplySession(msgID, convID)
			return strings.TrimSpace(convID)
		}
	}
	return channelSessionID(dialogID, channelID, chatID)
}

func formatChannelSenderDisplay(username, fullName, senderID, chatID string) string {
	username = strings.TrimPrefix(strings.TrimSpace(username), "@")
	fullName = strings.TrimSpace(fullName)
	senderID = strings.TrimSpace(senderID)
	if username != "" && fullName != "" {
		return "@" + username + " (" + fullName + ")"
	}
	if username != "" {
		return "@" + username
	}
	if fullName != "" {
		return fullName
	}
	if senderID != "" && senderID != strings.TrimSpace(chatID) {
		return "uid:" + senderID
	}
	return ""
}

func formatChannelSessionLabel(channelName, chatType, chatTitle, senderDisplay string) string {
	base := strings.TrimSpace(channelName)
	chatTitle = strings.TrimSpace(chatTitle)
	senderDisplay = strings.TrimSpace(senderDisplay)

	if chatType == "group" {
		parts := make([]string, 0, 3)
		if base != "" {
			parts = append(parts, base)
		}
		if chatTitle != "" {
			parts = append(parts, "Nhóm: "+chatTitle)
		} else {
			parts = append(parts, "Nhóm")
		}
		if senderDisplay != "" {
			parts = append(parts, senderDisplay)
		}
		return strings.Join(parts, " | ")
	}

	if senderDisplay != "" && !strings.HasPrefix(senderDisplay, "uid:") {
		if base != "" {
			return base + " | " + senderDisplay
		}
		return senderDisplay
	}
	return base
}

func (s *ChatChannelService) resolveOrCreateChannelSession(
	ctx context.Context,
	dialogID, channelID, channelLabel, senderDisplay string,
	msg ChatChannelIncomingMessage,
) (*entity.ChatSession, error) {
	sessionChatKey := msg.ChatID
	if msg.ChatType == "group" {
		threadID := strings.TrimSpace(msg.ThreadID)
		if threadID == "" {
			threadID = strings.TrimSpace(msg.ReplyToMessageID)
		}
		if threadID == "" {
			threadID = strings.TrimSpace(msg.MessageID)
		}
		if threadID != "" {
			sessionChatKey = msg.ChatID + ":thread:" + threadID
		}

		candidateID := channelSessionID(dialogID, channelID, sessionChatKey)
		if existing, err := s.chatSessionSvc.chatSessionDAO.GetByID(ctx, dao.DB, candidateID); err == nil && existing != nil {
			s.ensureChannelSessionSenderUserID(ctx, existing, senderDisplay)
			return existing, nil
		}

		if strings.TrimSpace(msg.ReplyToMessageID) != "" || strings.TrimSpace(msg.ReplyToText) != "" {
			if recovered := s.findExistingGroupReplySession(ctx, dialogID, msg.ChatID, msg.ReplyToMessageID, msg.ReplyToText, msg.ReplyToIsBot); recovered != nil {
				s.ensureChannelSessionSenderUserID(ctx, recovered, senderDisplay)
				return recovered, nil
			}
		}
	}

	session, err := s.chatSessionSvc.GetOrCreateForChannel(ctx, dialogID, channelID, sessionChatKey, msg.Text, channelLabel)
	if err != nil {
		return nil, err
	}
	s.ensureChannelSessionSenderUserID(ctx, session, senderDisplay)
	return session, nil
}

func (s *ChatChannelService) ensureChannelSessionSenderUserID(ctx context.Context, session *entity.ChatSession, senderDisplay string) {
	senderDisplay = strings.TrimSpace(senderDisplay)
	if session == nil || senderDisplay == "" {
		return
	}
	if session.UserID != nil && strings.TrimSpace(*session.UserID) != "" {
		return
	}
	if len([]rune(senderDisplay)) > 255 {
		senderDisplay = string([]rune(senderDisplay)[:255])
	}
	_ = s.chatSessionSvc.chatSessionDAO.UpdateByID(ctx, dao.DB, session.ID, map[string]interface{}{
		"user_id": senderDisplay,
	})
	session.UserID = &senderDisplay
}

func (s *ChatChannelService) findExistingGroupReplySession(
	ctx context.Context,
	dialogID, chatID, replyToMsgID, replyToText string,
	replyToIsBot bool,
) *entity.ChatSession {
	if dao.DB == nil {
		return nil
	}
	replyToMsgID = strings.TrimSpace(replyToMsgID)
	replyToText = strings.TrimSpace(replyToText)

	if replyToMsgID != "" {
		pattern := fmt.Sprintf("%%\"telegram_message_id\":\"%s\"%%", replyToMsgID)
		var convID string
		err := dao.DB.WithContext(ctx).
			Table("conversation_message AS cm").
			Select("cm.conversation_id").
			Joins("JOIN conversation AS c ON c.id = cm.conversation_id").
			Where("c.dialog_id = ? AND cm.metadata LIKE ?", dialogID, pattern).
			Order("cm.position DESC").
			Limit(1).
			Scan(&convID).Error
		if err == nil && strings.TrimSpace(convID) != "" {
			if session, sErr := s.chatSessionSvc.chatSessionDAO.GetByID(ctx, dao.DB, strings.TrimSpace(convID)); sErr == nil && session != nil {
				return session
			}
		}
	}

	if replyToIsBot && replyToText != "" {
		var convID string
		q := dao.DB.WithContext(ctx).
			Table("conversation_message AS cm").
			Select("cm.conversation_id").
			Joins("JOIN conversation AS c ON c.id = cm.conversation_id").
			Where("c.dialog_id = ? AND cm.role = ? AND cm.content = ?", dialogID, "assistant", replyToText)
		if strings.TrimSpace(chatID) != "" {
			chatPattern := fmt.Sprintf("%%\"chat_id\":\"%s\"%%", strings.TrimSpace(chatID))
			q = q.Where("EXISTS (SELECT 1 FROM conversation_message AS um WHERE um.conversation_id = cm.conversation_id AND um.metadata LIKE ?)", chatPattern)
		}
		err := q.Order("c.update_time DESC").Limit(1).Scan(&convID).Error
		if err == nil && strings.TrimSpace(convID) != "" {
			if session, sErr := s.chatSessionSvc.chatSessionDAO.GetByID(ctx, dao.DB, strings.TrimSpace(convID)); sErr == nil && session != nil {
				return session
			}
		}
	}
	return nil
}

func buildContextualChannelQuery(msg ChatChannelIncomingMessage, existingMsgs []map[string]interface{}) string {
	userText := strings.TrimSpace(msg.Text)
	quoteText := strings.TrimSpace(msg.QuoteText)
	replyText := strings.TrimSpace(msg.ReplyToText)
	replySender := strings.TrimSpace(msg.ReplyToSender)

	if quoteText != "" {
		excerpt := truncateChannelContextExcerpt(quoteText, 400)
		if replySender != "" {
			return fmt.Sprintf("[Trích dẫn tin nhắn được trả lời từ %s: %q]\n\n%s", replySender, excerpt, userText)
		}
		return fmt.Sprintf("[Trích dẫn tin nhắn được trả lời: %q]\n\n%s", excerpt, userText)
	}

	if replyText == "" {
		return msg.Text
	}

	// If the session already has messages and the immediately preceding message in this thread
	// has the exact same content as replyText, the thread history already carries it directly.
	if len(existingMsgs) > 0 {
		lastContent := strings.TrimSpace(stringValue(existingMsgs[len(existingMsgs)-1]["content"]))
		if lastContent == replyText {
			return msg.Text
		}
	}

	excerpt := truncateChannelContextExcerpt(replyText, 500)
	if replySender != "" {
		return fmt.Sprintf("[Ngữ cảnh tin nhắn được trả lời từ %s: %q]\n\n%s", replySender, excerpt, userText)
	}
	return fmt.Sprintf("[Ngữ cảnh tin nhắn được trả lời: %q]\n\n%s", excerpt, userText)
}

func truncateChannelContextExcerpt(text string, maxRunes int) string {
	runes := []rune(strings.TrimSpace(text))
	if len(runes) <= maxRunes {
		return string(runes)
	}
	return string(runes[:maxRunes]) + "..."
}

func buildChannelUserMessagePayload(msg ChatChannelIncomingMessage, effectiveText, senderDisplay string) map[string]interface{} {
	userMsg := map[string]interface{}{
		"role":    "user",
		"content": effectiveText,
	}
	if msg.Channel != "" {
		userMsg["channel"] = msg.Channel
	}
	if msg.ChatID != "" {
		userMsg["chat_id"] = msg.ChatID
	}
	if msg.ChatType != "" {
		userMsg["chat_type"] = msg.ChatType
	}
	if msg.ChatTitle != "" {
		userMsg["chat_title"] = msg.ChatTitle
	}
	if msg.MessageID != "" {
		userMsg["telegram_message_id"] = msg.MessageID
	}
	if msg.ThreadID != "" {
		userMsg["thread_id"] = msg.ThreadID
	}
	if msg.SenderID != "" {
		userMsg["sender_id"] = msg.SenderID
	}
	if msg.SenderName != "" {
		userMsg["sender_name"] = msg.SenderName
	}
	if msg.SenderUsername != "" {
		userMsg["sender_username"] = msg.SenderUsername
	}
	if senderDisplay != "" {
		userMsg["sender_display"] = senderDisplay
	}
	if msg.ReplyToMessageID != "" {
		userMsg["reply_to_message_id"] = msg.ReplyToMessageID
	}
	if msg.ReplyToSender != "" {
		userMsg["reply_to_sender"] = msg.ReplyToSender
	}
	return userMsg
}

func (s *ChatChannelService) handleAgentCallbackQuery(
	ctx context.Context,
	channel *entity.ChatChannel,
	agentID, msgID, action string,
	parts []string,
	cb core.CallbackQuery,
) (*core.CallbackResult, error) {
	canvasRow, _, err := s.validateChannelAgentTarget(ctx, channel.TenantID, channel.TenantID, agentID, permission.OperationUse)
	if err != nil || canvasRow == nil {
		return nil, errors.New("agent not found")
	}

	sessionID := s.resolveAgentChannelCallbackSessionID(ctx, canvasRow.ID, channel.ID, cb.ChatID, msgID)
	conv, err := s.api4ConversationDAO.GetBySessionID(ctx, dao.DB, sessionID, canvasRow.ID)
	if err != nil || conv == nil {
		return nil, errors.New("agent session not found")
	}

	feedbackCfg := entity.ExtractFeedbackConfig(map[string]any(canvasRow.DSL))
	tenantID := canvasRow.EffectiveTenantID()
	agentSvc := s.agentSvc
	if agentSvc == nil {
		agentSvc = getChatChannelAgentService()
	}

	if action == "up" || action == "dn" {
		thumb := action == "up"
		req := map[string]interface{}{
			"thumbup": thumb,
			"channel": "telegram",
		}
		if _, _, uErr := updateAPI4ConversationFeedback(
			ctx,
			s.api4ConversationDAO,
			conv,
			tenantID,
			"agent",
			canvasRow.ID,
			msgID,
			req,
			feedbackCfg,
			"telegram",
			channel.TenantID,
			agentSvc,
		); uErr != nil {
			return nil, uErr
		}
		label := "✅ Đã đánh giá: 👍 Hữu ích"
		if !thumb {
			label = "✅ Đã đánh giá: 👎 Chưa tốt"
		}
		return &core.CallbackResult{
			ToastText: "Cảm ơn đánh giá của bạn!",
			InlineButtons: [][]core.InlineButton{
				{{Text: label, CallbackData: fmt.Sprintf("fb:%s:done", msgID)}},
			},
		}, nil
	}

	if strings.HasPrefix(action, "c") && len(parts) >= 4 {
		idx, idxErr := strconv.Atoi(strings.TrimPrefix(action, "c"))
		valInt, valErr := strconv.Atoi(parts[3])
		if idxErr != nil || valErr != nil || feedbackCfg == nil || idx < 0 || idx >= len(feedbackCfg.Criteria) {
			return &core.CallbackResult{ToastText: "Tiêu chí đánh giá không hợp lệ."}, nil
		}

		criteriaMap := make(map[string]interface{})
		for _, m := range parseMessages(conv.Message) {
			if stringValue(m["id"]) == msgID && stringValue(m["role"]) == "assistant" {
				if detail, ok := m["feedback_detail"].(map[string]interface{}); ok {
					if prevCrit, ok := detail["criteria"].(map[string]interface{}); ok {
						for k, v := range prevCrit {
							criteriaMap[k] = v
						}
					}
				}
				break
			}
		}

		crit := feedbackCfg.Criteria[idx]
		if crit.Type == entity.CriterionTypeBinary {
			criteriaMap[crit.Key] = valInt == 1
		} else {
			criteriaMap[crit.Key] = valInt
		}

		nextIdx := idx + 1
		req := map[string]interface{}{
			"criteria":      criteriaMap,
			"channel":       "telegram",
			"telegram_step": nextIdx,
		}
		if _, _, uErr := updateAPI4ConversationFeedback(
			ctx,
			s.api4ConversationDAO,
			conv,
			tenantID,
			"agent",
			canvasRow.ID,
			msgID,
			req,
			feedbackCfg,
			"telegram",
			channel.TenantID,
			agentSvc,
		); uErr != nil {
			return nil, uErr
		}

		if nextIdx < len(feedbackCfg.Criteria) {
			nextCrit := feedbackCfg.Criteria[nextIdx]
			return &core.CallbackResult{
				ToastText:     fmt.Sprintf("Đã lưu (%d/%d). Tiếp theo: %s", nextIdx, len(feedbackCfg.Criteria), nextCrit.Label),
				InlineButtons: BuildTelegramRatingButtons(msgID, feedbackCfg, nextIdx),
			}, nil
		}

		return &core.CallbackResult{
			ToastText: "Cảm ơn bạn đã hoàn tất đánh giá!",
			InlineButtons: [][]core.InlineButton{
				{{Text: "✅ Đã ghi nhận đánh giá", CallbackData: fmt.Sprintf("fb:%s:done", msgID)}},
			},
		}, nil
	}

	return &core.CallbackResult{ToastText: "Đã ghi nhận!"}, nil
}

func (s *ChatChannelService) resolveAgentChannelCallbackSessionID(ctx context.Context, agentID, channelID, chatID, msgID string) string {
	if cached := lookupChannelReplySession(msgID); cached != "" {
		return cached
	}
	if dao.DB != nil && strings.TrimSpace(msgID) != "" {
		var convID string
		err := dao.DB.WithContext(ctx).
			Table("api_4_conversation_message AS cm").
			Select("cm.conversation_id").
			Joins("JOIN api_4_conversation AS c ON c.id = cm.conversation_id").
			Where("c.dialog_id = ? AND cm.message_id = ? AND cm.role = ?", agentID, msgID, "assistant").
			Limit(1).
			Scan(&convID).Error
		if err == nil && strings.TrimSpace(convID) != "" {
			rememberChannelReplySession(msgID, convID)
			return strings.TrimSpace(convID)
		}
	}
	return channelSessionID(agentID, channelID, chatID)
}

func (s *ChatChannelService) resolveOrCreateAgentChannelSession(
	ctx context.Context,
	agentID, channelID string,
	msg ChatChannelIncomingMessage,
) (string, *entity.API4Conversation, error) {
	sessionChatKey := msg.ChatID
	if msg.ChatType == "group" {
		threadID := strings.TrimSpace(msg.ThreadID)
		if threadID == "" {
			threadID = strings.TrimSpace(msg.ReplyToMessageID)
		}
		if threadID == "" {
			threadID = strings.TrimSpace(msg.MessageID)
		}
		if threadID != "" {
			sessionChatKey = msg.ChatID + ":thread:" + threadID
		}

		candidateID := channelSessionID(agentID, channelID, sessionChatKey)
		if existing, err := s.api4ConversationDAO.GetBySessionID(ctx, dao.DB, candidateID, agentID); err == nil && existing != nil {
			return existing.ID, existing, nil
		}

		if strings.TrimSpace(msg.ReplyToMessageID) != "" || strings.TrimSpace(msg.ReplyToText) != "" {
			if recovered := s.findExistingAgentGroupReplySession(ctx, agentID, msg.ChatID, msg.ReplyToMessageID, msg.ReplyToText, msg.ReplyToIsBot); recovered != nil {
				return recovered.ID, recovered, nil
			}
		}
	}

	candidateID := channelSessionID(agentID, channelID, sessionChatKey)
	if existing, err := s.api4ConversationDAO.GetBySessionID(ctx, dao.DB, candidateID, agentID); err != nil {
		return "", nil, err
	} else if existing != nil {
		return existing.ID, existing, nil
	}
	return candidateID, nil, nil
}

func (s *ChatChannelService) findExistingAgentGroupReplySession(
	ctx context.Context,
	agentID, chatID, replyToMsgID, replyToText string,
	replyToIsBot bool,
) *entity.API4Conversation {
	if dao.DB == nil {
		return nil
	}
	replyToMsgID = strings.TrimSpace(replyToMsgID)
	replyToText = strings.TrimSpace(replyToText)

	if replyToMsgID != "" {
		pattern := fmt.Sprintf("%%\"telegram_message_id\":\"%s\"%%", replyToMsgID)
		var convID string
		err := dao.DB.WithContext(ctx).
			Table("api_4_conversation_message AS cm").
			Select("cm.conversation_id").
			Joins("JOIN api_4_conversation AS c ON c.id = cm.conversation_id").
			Where("c.dialog_id = ? AND cm.metadata LIKE ?", agentID, pattern).
			Order("cm.position DESC").
			Limit(1).
			Scan(&convID).Error
		if err == nil && strings.TrimSpace(convID) != "" {
			if session, sErr := s.api4ConversationDAO.GetBySessionID(ctx, dao.DB, strings.TrimSpace(convID), agentID); sErr == nil && session != nil {
				return session
			}
		}
	}

	if replyToIsBot && replyToText != "" {
		var convID string
		q := dao.DB.WithContext(ctx).
			Table("api_4_conversation_message AS cm").
			Select("cm.conversation_id").
			Joins("JOIN api_4_conversation AS c ON c.id = cm.conversation_id").
			Where("c.dialog_id = ? AND cm.role = ? AND cm.content = ?", agentID, "assistant", replyToText)
		if strings.TrimSpace(chatID) != "" {
			chatPattern := fmt.Sprintf("%%\"chat_id\":\"%s\"%%", strings.TrimSpace(chatID))
			q = q.Where("EXISTS (SELECT 1 FROM api_4_conversation_message AS um WHERE um.conversation_id = cm.conversation_id AND um.metadata LIKE ?)", chatPattern)
		}
		err := q.Order("c.update_time DESC").Limit(1).Scan(&convID).Error
		if err == nil && strings.TrimSpace(convID) != "" {
			if session, sErr := s.api4ConversationDAO.GetBySessionID(ctx, dao.DB, strings.TrimSpace(convID), agentID); sErr == nil && session != nil {
				return session
			}
		}
	}
	return nil
}

const pendingAgentChannelInputsDSLKey = "_channel_pending_inputs"

func extractPendingAgentChannelInputs(dsl entity.JSONMap) map[string]any {
	if len(dsl) == 0 {
		return nil
	}
	if raw, ok := dsl[pendingAgentChannelInputsDSLKey].(map[string]any); ok && len(raw) > 0 {
		return raw
	}
	return nil
}

func buildAgentChannelResumeInput(text string, pendingInputs map[string]any) map[string]any {
	out := make(map[string]any, len(pendingInputs))
	for k, v := range pendingInputs {
		if m, ok := v.(map[string]any); ok {
			copied := make(map[string]any, len(m)+1)
			for fk, fv := range m {
				copied[fk] = fv
			}
			copied["value"] = text
			out[k] = copied
		} else {
			out[k] = text
		}
	}
	return out
}

func formatWaitingForUserPrompt(ev canvas.WaitingForUserEvent) string {
	if tips := strings.TrimSpace(ev.Tips); tips != "" {
		return tips
	}
	return formatInputsPromptMap(ev.Inputs)
}

func formatLegacyUserInputsPrompt(data map[string]any) string {
	if len(data) == 0 {
		return ""
	}
	if tips, _ := data["tips"].(string); strings.TrimSpace(tips) != "" {
		return strings.TrimSpace(tips)
	}
	if inputs, ok := data["inputs"].(map[string]any); ok {
		return formatInputsPromptMap(inputs)
	}
	return ""
}

func formatInputsPromptMap(inputs map[string]any) string {
	if len(inputs) == 0 {
		return ""
	}
	names := make([]string, 0, len(inputs))
	for k, v := range inputs {
		label := strings.TrimSpace(k)
		if m, ok := v.(map[string]any); ok {
			if n, _ := m["name"].(string); strings.TrimSpace(n) != "" {
				label = strings.TrimSpace(n)
			}
		}
		if label != "" {
			names = append(names, label)
		}
	}
	if len(names) == 0 {
		return ""
	}
	return "Please provide: " + strings.Join(names, ", ")
}

func (s *ChatChannelService) updateAgentChannelSessionMetadata(
	ctx context.Context,
	agentID, sessionID string,
	isFirstTurn bool,
	channelLabel, senderDisplay string,
	msg ChatChannelIncomingMessage,
	replyMsgID string,
	pausedForInput bool,
	waitingInputs map[string]any,
) {
	if dao.DB == nil || strings.TrimSpace(sessionID) == "" {
		return
	}
	conv, err := s.api4ConversationDAO.GetMetadataBySessionID(ctx, dao.DB, sessionID, agentID)
	if err != nil || conv == nil {
		return
	}

	updates := make(map[string]interface{})
	if isFirstTurn {
		sessionName := channelSessionName(msg.Text, channelLabel)
		updates["name"] = sessionName
	}
	if trimmedSender := strings.TrimSpace(senderDisplay); trimmedSender != "" {
		if len([]rune(trimmedSender)) > 255 {
			trimmedSender = string([]rune(trimmedSender)[:255])
		}
		updates["exp_user_id"] = trimmedSender
	}

	if conv.DSL != nil || (pausedForInput && len(waitingInputs) > 0) {
		dslCopy := make(entity.JSONMap, len(conv.DSL)+1)
		for k, v := range conv.DSL {
			dslCopy[k] = v
		}
		if pausedForInput && len(waitingInputs) > 0 {
			dslCopy[pendingAgentChannelInputsDSLKey] = waitingInputs
			updates["dsl"] = dslCopy
		} else if _, hadPending := dslCopy[pendingAgentChannelInputsDSLKey]; hadPending {
			delete(dslCopy, pendingAgentChannelInputsDSLKey)
			updates["dsl"] = dslCopy
		}
	}

	if len(updates) > 0 {
		_ = dao.DB.WithContext(ctx).
			Model(&entity.API4Conversation{}).
			Where("id = ? AND dialog_id = ?", sessionID, agentID).
			Updates(updates).Error
	}

	if strings.TrimSpace(replyMsgID) == "" {
		return
	}
	var userRow entity.ConversationMessage
	if err := dao.DB.WithContext(ctx).
		Table("api_4_conversation_message").
		Where("conversation_id = ? AND message_id = ? AND role = ?", sessionID, replyMsgID, "user").
		Order("position DESC").
		Take(&userRow).Error; err == nil {
		meta := make(map[string]interface{})
		if strings.TrimSpace(userRow.Metadata) != "" {
			_ = json.Unmarshal([]byte(userRow.Metadata), &meta)
		}
		for k, v := range buildChannelUserMessagePayload(msg, "", senderDisplay) {
			if k == "role" || k == "content" {
				continue
			}
			meta[k] = v
		}
		if rawMeta, mErr := json.Marshal(meta); mErr == nil {
			_ = dao.DB.WithContext(ctx).
				Table("api_4_conversation_message").
				Where("conversation_id = ? AND position = ?", sessionID, userRow.Position).
				Update("metadata", string(rawMeta)).Error
		}
	}
}
