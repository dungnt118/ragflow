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
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"go.uber.org/zap"

	"gorm.io/gorm"
	"ragflow/internal/channels/core"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

type ChatChannelService struct {
	chatChannelDAO *dao.ChatChannelDAO
	chatDAO        *dao.ChatDAO
	chatSessionSvc *ChatSessionService
}

func NewChatChannelService() *ChatChannelService {
	return &ChatChannelService{
		chatChannelDAO: dao.NewChatChannel(),
		chatDAO:        dao.NewChatDAO(),
		chatSessionSvc: NewChatSessionService(),
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

func (s *ChatChannelService) CreateChatChannel(ctx context.Context, userID, name, channelType string, config entity.JSONMap, chatID *string) (*entity.ChatChannel, error) {
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
	if chatID != nil && *chatID != "" {
		if err := checkChatPermission(ctx, nil, userID, *chatID, permission.OperationUse); err != nil {
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

	if value, exists := req["chat_id"]; exists {
		if value == nil {
			updates["chat_id"] = nil
		} else {
			chatID, ok := value.(string)
			if !ok {
				return nil, common.CodeDataError, errors.New("chat_id must be string or null")
			}
			if chatID != "" {
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
			}
			updates["chat_id"] = chatID
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
	if err != nil || channel == nil || channel.ChatID == nil || strings.TrimSpace(*channel.ChatID) == "" {
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
