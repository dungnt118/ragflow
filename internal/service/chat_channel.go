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
	Channel   string
	AccountID string
	ChatID    string
	ChatType  string
	MessageID string
	SenderID  string
	Text      string
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

	session, err := s.chatSessionSvc.GetOrCreateForChannel(ctx, dialog.ID, channel.ID, msg.ChatID, msg.Text, channel.Name)
	if err != nil {
		return nil, err
	}

	result, err := s.chatSessionSvc.ChatCompletions(
		ctx,
		dialog.TenantID,
		dialog.ID,
		session.ID,
		[]map[string]interface{}{{"role": "user", "content": msg.Text}},
		"",
		nil,
		"",
		nil,
		map[string]interface{}{"quote": false},
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

	// Deterministic session_id derivation (FND-06) keeps callback_data well under 64 bytes.
	sessionID := channelSessionID(dialog.ID, channel.ID, cb.ChatID)
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
