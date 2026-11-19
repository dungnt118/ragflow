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

package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"ragflow/internal/channels/core"
	"ragflow/internal/common"
)

const (
	defaultTelegramAPIBaseURL = "https://api.telegram.org"
	telegramPollTimeout       = 30 * time.Second
	telegramHTTPTimeout       = 40 * time.Second
	telegramReconnectDelay    = 3 * time.Second
	telegramTypingInterval    = 4 * time.Second
	maxTelegramThreadCache    = 10000
)

type telegramAccount struct {
	AccountID         string
	Token             string
	APIBaseURL        string
	TypingInterval    time.Duration
	AllowGroupPrivacy bool
	BotUsername       string
}

type telegramChannel struct {
	account telegramAccount

	mu              sync.Mutex
	cancel          context.CancelFunc
	done            chan struct{}
	handler         core.MessageHandler
	callbackHandler core.CallbackHandler
	client          *http.Client
	offset          int64
	botUsername     string
	botID           int64
	threadRootByMsg map[string]string
	threadOrder     []string
}

type telegramAPIResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
}

type telegramTransportError struct {
	method string
	err    error
}

func (e *telegramTransportError) Error() string {
	return fmt.Sprintf("Telegram API %s transport request failed", e.method)
}

func (e *telegramTransportError) Unwrap() error {
	return e.err
}

type telegramUpdate struct {
	UpdateID          int64                  `json:"update_id"`
	Message           *telegramMessage       `json:"message"`
	EditedMessage     *telegramMessage       `json:"edited_message"`
	ChannelPost       *telegramMessage       `json:"channel_post"`
	EditedChannelPost *telegramMessage       `json:"edited_channel_post"`
	CallbackQuery     *telegramCallbackQuery `json:"callback_query"`
	Raw               map[string]any
}

type telegramCallbackQuery struct {
	ID      string           `json:"id"`
	From    *telegramUser    `json:"from"`
	Message *telegramMessage `json:"message"`
	Data    string           `json:"data"`
}

type telegramMessage struct {
	MessageID       int64                   `json:"message_id"`
	MessageThreadID int64                   `json:"message_thread_id"`
	From            *telegramUser           `json:"from"`
	Chat            telegramChat            `json:"chat"`
	ReplyToMessage  *telegramMessage        `json:"reply_to_message"`
	Quote           *telegramTextQuote      `json:"quote"`
	Text            string                  `json:"text"`
	Caption         string                  `json:"caption"`
	Entities        []telegramMessageEntity `json:"entities"`
	CaptionEntities []telegramMessageEntity `json:"caption_entities"`
}

type telegramTextQuote struct {
	Text string `json:"text"`
}

type telegramMessageEntity struct {
	Type   string        `json:"type"`
	Offset int           `json:"offset"`
	Length int           `json:"length"`
	User   *telegramUser `json:"user"`
}

type telegramUser struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type telegramChat struct {
	ID    int64  `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

// newTelegramChannel creates a Telegram channel with default API settings.
func newTelegramChannel(account telegramAccount) *telegramChannel {
	account.Token = strings.TrimSpace(account.Token)
	account.APIBaseURL = strings.TrimSpace(account.APIBaseURL)
	if account.APIBaseURL == "" {
		account.APIBaseURL = defaultTelegramAPIBaseURL
	}
	account.BotUsername = normalizeTelegramUsername(account.BotUsername)
	return &telegramChannel{
		account:         account,
		client:          &http.Client{Timeout: telegramHTTPTimeout},
		botUsername:     account.BotUsername,
		threadRootByMsg: make(map[string]string),
	}
}

// newTelegramChannelFromConfig builds a Telegram channel from chat_channel.config.credential.
func newTelegramChannelFromConfig(accountID string, cfg map[string]any) (*telegramChannel, error) {
	token := strings.TrimSpace(fmt.Sprint(cfg["token"]))
	if token == "" || token == "<nil>" {
		return nil, fmt.Errorf("telegram account %q is missing token", accountID)
	}

	// APIBaseURL is intentionally optional and is useful for Bot API-compatible
	// proxies and tests. The Python implementation defaults to api.telegram.org.
	baseURL := firstString(cfg, "api_base_url", "base_url")
	allowGroupPrivacy := telegramBoolFromConfig(cfg, false, "allowGroupPrivacy", "allow_group_privacy")
	botUsername := firstString(cfg, "bot_username", "username")
	return newTelegramChannel(telegramAccount{
		AccountID:         accountID,
		Token:             token,
		APIBaseURL:        baseURL,
		AllowGroupPrivacy: allowGroupPrivacy,
		BotUsername:       botUsername,
	}), nil
}

// ChannelID returns the platform identifier used by the chat-channel runtime.
func (c *telegramChannel) ChannelID() string {
	return "telegram"
}

// AccountID returns the chat_channel.id bound to this Telegram runtime instance.
func (c *telegramChannel) AccountID() string {
	return c.account.AccountID
}

// SetMessageHandler installs the RAGFlow bridge invoked for inbound messages.
func (c *telegramChannel) SetMessageHandler(handler core.MessageHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handler = handler
}

// SetCallbackHandler installs the callback_query handler for inline rating buttons.
func (c *telegramChannel) SetCallbackHandler(handler core.CallbackHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.callbackHandler = handler
}

// Start starts Telegram long polling in the background.
func (c *telegramChannel) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.cancel != nil {
		c.mu.Unlock()
		return nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	c.cancel = cancel
	c.done = done
	c.offset = 0
	c.mu.Unlock()

	go func() {
		defer close(done)
		c.run(runCtx)

		c.mu.Lock()
		if c.done == done {
			c.cancel = nil
		}
		c.mu.Unlock()
	}()
	return nil
}

// Stop cancels Telegram long polling and waits for the poll request to finish.
func (c *telegramChannel) Stop(ctx context.Context) error {
	c.mu.Lock()
	cancel := c.cancel
	done := c.done
	c.mu.Unlock()
	if cancel == nil {
		return nil
	}

	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Send sends a Telegram message and optionally replies to an existing message.
// Ai -> user
func (c *telegramChannel) Send(ctx context.Context, msg core.OutgoingMessage) error {
	chatID, err := strconv.ParseInt(strings.TrimSpace(msg.ChatID), 10, 64)
	if err != nil {
		common.Warn("telegram: invalid chat_id", zap.String("account_id", c.account.AccountID), zap.String("chat_id", msg.ChatID), zap.Error(err))
		return nil
	}

	payload := map[string]any{
		"chat_id": chatID,
		"text":    msg.Text,
	}
	replyID := strings.TrimSpace(msg.ReplyToMessageID)
	if replyID != "" {
		if messageID, parseErr := strconv.ParseInt(replyID, 10, 64); parseErr == nil {
			payload["reply_parameters"] = map[string]any{
				"message_id":                  messageID,
				"allow_sending_without_reply": true,
			}
		}
	}
	if len(msg.InlineButtons) > 0 {
		payload["reply_markup"] = map[string]any{
			"inline_keyboard": buildTelegramInlineKeyboard(msg.InlineButtons),
		}
	}
	var sentMsg struct {
		MessageID int64 `json:"message_id"`
	}
	if err := c.callAPI(ctx, "sendMessage", payload, &sentMsg); err != nil {
		common.Error("telegram: send failed", err, zap.String("account_id", c.account.AccountID))
		return nil
	}
	if sentMsg.MessageID > 0 && replyID != "" {
		c.bindBotReplyThread(chatID, replyID, strconv.FormatInt(sentMsg.MessageID, 10))
	}
	return nil
}

func buildTelegramInlineKeyboard(rows [][]core.InlineButton) [][]map[string]any {
	keyboard := make([][]map[string]any, 0, len(rows))
	for _, row := range rows {
		btnRow := make([]map[string]any, 0, len(row))
		for _, btn := range row {
			if btn.Text == "" || btn.CallbackData == "" {
				continue
			}
			cbData := btn.CallbackData
			if len(cbData) > 64 {
				cbData = cbData[:64]
			}
			btnRow = append(btnRow, map[string]any{
				"text":          btn.Text,
				"callback_data": cbData,
			})
		}
		if len(btnRow) > 0 {
			keyboard = append(keyboard, btnRow)
		}
	}
	return keyboard
}

// run drops queued updates and consumes Telegram updates until stopped.
func (c *telegramChannel) run(ctx context.Context) {
	for ctx.Err() == nil {
		err := c.callAPI(ctx, "deleteWebhook", map[string]any{
			"drop_pending_updates": true,
		}, nil)
		if err == nil {
			break
		}
		common.Error("telegram: failed to start polling", err, zap.String("account_id", c.account.AccountID))
		if !waitForTelegramRetry(ctx) {
			return
		}
	}
	if ctx.Err() != nil {
		return
	}

	for ctx.Err() == nil {
		updates, err := c.getUpdates(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			common.Error("telegram: polling failed", err, zap.String("account_id", c.account.AccountID))
			if !waitForTelegramRetry(ctx) {
				return
			}
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= c.offset {
				c.offset = update.UpdateID + 1
			}
			c.handleUpdate(ctx, update)
		}
	}
}

// getUpdates performs one Telegram long-poll request and decodes raw updates.
func (c *telegramChannel) getUpdates(ctx context.Context) ([]telegramUpdate, error) {
	params := map[string]any{
		"offset":  c.offset,
		"timeout": int(telegramPollTimeout / time.Second),
	}
	var rawUpdates []json.RawMessage
	if err := c.callAPI(ctx, "getUpdates", params, &rawUpdates); err != nil {
		return nil, err
	}

	updates := make([]telegramUpdate, 0, len(rawUpdates))
	for _, raw := range rawUpdates {
		var update telegramUpdate
		if err := json.Unmarshal(raw, &update); err != nil {
			common.Warn("telegram: ignored invalid update", zap.String("account_id", c.account.AccountID), zap.Error(err))
			continue
		}
		_ = json.Unmarshal(raw, &update.Raw)
		updates = append(updates, update)
	}
	return updates, nil
}

// handleUpdate maps Telegram's effective message into the common channel model.
// Telegram -> AI
func (c *telegramChannel) handleUpdate(ctx context.Context, update telegramUpdate) {
	if update.CallbackQuery != nil {
		c.handleCallbackUpdate(ctx, update.CallbackQuery)
		return
	}
	msg := update.effectiveMessage()
	if msg == nil {
		return
	}
	if msg.From == nil && msg != update.ChannelPost && msg != update.EditedChannelPost {
		return
	}
	if msg.From != nil && msg.From.IsBot {
		return
	}

	text := msg.Text
	entities := msg.Entities
	if text == "" {
		text = msg.Caption
		entities = msg.CaptionEntities
	}
	chatType := telegramChatType(msg.Chat.Type)
	effectiveReply := isEffectiveTelegramReply(msg)
	repliedToBot := false
	if chatType == "group" {
		needBotIdentity := !c.account.AllowGroupPrivacy || strings.Contains(text, "@") || effectiveReply
		var botUsername string
		var botID int64
		if needBotIdentity {
			botUsername, botID = c.ensureBotIdentity(ctx)
		}
		mentioned, cleanedText := matchAndStripTelegramBotMention(text, entities, botUsername, botID)
		if effectiveReply {
			repliedToBot = isTelegramReplyToBot(msg.ReplyToMessage, botUsername, botID)
		}
		if !c.account.AllowGroupPrivacy && !mentioned && !repliedToBot {
			return
		}
		if mentioned && strings.TrimSpace(cleanedText) != "" {
			text = cleanedText
		}
	}
	senderID := strconv.FormatInt(msg.Chat.ID, 10)
	senderName := ""
	senderUsername := ""
	if msg.From != nil {
		senderID = strconv.FormatInt(msg.From.ID, 10)
		senderName = telegramUserFullName(msg.From)
		senderUsername = strings.TrimPrefix(strings.TrimSpace(msg.From.Username), "@")
	}

	var replyToMsgID, replyToSender, replyToText, quoteText string
	if msg.Quote != nil {
		quoteText = strings.TrimSpace(msg.Quote.Text)
	}
	if effectiveReply && msg.ReplyToMessage != nil {
		reply := msg.ReplyToMessage
		replyToMsgID = strconv.FormatInt(reply.MessageID, 10)
		replyToText = strings.TrimSpace(reply.Text)
		if replyToText == "" {
			replyToText = strings.TrimSpace(reply.Caption)
		}
		replyToSender = telegramUserDisplayLabel(reply.From)
	}

	threadID := ""
	if chatType == "group" {
		threadID = c.resolveGroupThreadID(msg.Chat.ID, msg.MessageID, replyToMsgID)
	}

	incoming := core.IncomingMessage{
		Channel:          c.ChannelID(),
		AccountID:        c.account.AccountID,
		ChatID:           strconv.FormatInt(msg.Chat.ID, 10),
		ChatType:         chatType,
		ChatTitle:        strings.TrimSpace(msg.Chat.Title),
		MessageID:        strconv.FormatInt(msg.MessageID, 10),
		ThreadID:         threadID,
		SenderID:         senderID,
		SenderName:       senderName,
		SenderUsername:   senderUsername,
		ReplyToMessageID: replyToMsgID,
		ReplyToSender:    replyToSender,
		ReplyToText:      replyToText,
		ReplyToIsBot:     repliedToBot,
		QuoteText:        quoteText,
		Text:             text,
		Raw:              update.Raw,
	}

	c.mu.Lock()
	handler := c.handler
	c.mu.Unlock()
	if handler == nil {
		return
	}

	stopTyping := c.startTyping(ctx, msg.Chat.ID, msg.Chat.Type)
	defer stopTyping()

	if err := handler(ctx, incoming); err != nil {
		common.Error("telegram: message handler error", err, zap.String("account_id", c.account.AccountID))
	}
}

// startTyping sends Telegram's "typing" chat action immediately and refreshes it
// periodically until the returned cancel function is called or ctx is done.
func (c *telegramChannel) startTyping(ctx context.Context, chatID int64, rawChatType string) context.CancelFunc {
	if rawChatType == "channel" {
		return func() {}
	}
	typingCtx, cancel := context.WithCancel(ctx)
	interval := c.account.TypingInterval
	if interval <= 0 {
		interval = telegramTypingInterval
	}

	c.sendTypingAction(typingCtx, chatID)

	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-typingCtx.Done():
				return
			case <-ticker.C:
				c.sendTypingAction(typingCtx, chatID)
			}
		}
	}()

	return func() {
		cancel()
		<-done
	}
}

func (c *telegramChannel) sendTypingAction(ctx context.Context, chatID int64) {
	if ctx.Err() != nil {
		return
	}
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = c.callAPI(reqCtx, "sendChatAction", map[string]any{
		"chat_id": chatID,
		"action":  "typing",
	}, nil)
}

func (c *telegramChannel) handleCallbackUpdate(ctx context.Context, cb *telegramCallbackQuery) {
	c.mu.Lock()
	cbHandler := c.callbackHandler
	c.mu.Unlock()
	if cbHandler == nil || cb == nil {
		return
	}
	var chatID, msgID, senderID string
	if cb.Message != nil {
		chatID = strconv.FormatInt(cb.Message.Chat.ID, 10)
		msgID = strconv.FormatInt(cb.Message.MessageID, 10)
	}
	if cb.From != nil {
		senderID = strconv.FormatInt(cb.From.ID, 10)
	}
	res, err := cbHandler(ctx, core.CallbackQuery{
		ID:        cb.ID,
		Channel:   c.ChannelID(),
		AccountID: c.account.AccountID,
		ChatID:    chatID,
		MessageID: msgID,
		SenderID:  senderID,
		Data:      cb.Data,
	})
	if err != nil {
		common.Error("telegram: callback handler error", err, zap.String("account_id", c.account.AccountID))
		_ = c.callAPI(ctx, "answerCallbackQuery", map[string]any{
			"callback_query_id": cb.ID,
			"text":              "Không thể ghi nhận đánh giá lúc này.",
		}, nil)
		return
	}
	toast := "Đã ghi nhận đánh giá!"
	if res != nil && res.ToastText != "" {
		toast = res.ToastText
	}
	_ = c.callAPI(ctx, "answerCallbackQuery", map[string]any{
		"callback_query_id": cb.ID,
		"text":              toast,
	}, nil)
	if res != nil && len(res.InlineButtons) > 0 && cb.Message != nil {
		_ = c.callAPI(ctx, "editMessageReplyMarkup", map[string]any{
			"chat_id":    cb.Message.Chat.ID,
			"message_id": cb.Message.MessageID,
			"reply_markup": map[string]any{
				"inline_keyboard": buildTelegramInlineKeyboard(res.InlineButtons),
			},
		}, nil)
	}
}

func (u telegramUpdate) effectiveMessage() *telegramMessage {
	if u.Message != nil {
		return u.Message
	}
	if u.EditedMessage != nil {
		return u.EditedMessage
	}
	if u.ChannelPost != nil {
		return u.ChannelPost
	}
	return u.EditedChannelPost
}

func telegramChatType(value string) string {
	switch value {
	case "private":
		return "p2p"
	case "group", "supergroup":
		return "group"
	case "channel":
		return "channel"
	default:
		if value == "" {
			return "unknown"
		}
		return value
	}
}

// callAPI invokes a Telegram Bot API method using the configured token.
func (c *telegramChannel) callAPI(ctx context.Context, method string, params map[string]any, out any) error {
	payload, err := json.Marshal(params)
	if err != nil {
		return err
	}
	baseURL := strings.TrimRight(c.account.APIBaseURL, "/")
	endpoint := baseURL + "/bot" + url.PathEscape(c.account.Token) + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return &telegramTransportError{method: method, err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return err
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("Telegram API %s returned status %d: %s", method, resp.StatusCode, string(body))
	}

	var response telegramAPIResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("Telegram API %s returned invalid JSON: %w", method, err)
	}
	if !response.OK {
		return fmt.Errorf("Telegram API %s failed: %s", method, response.Description)
	}
	if out != nil && len(response.Result) > 0 {
		if err := json.Unmarshal(response.Result, out); err != nil {
			return fmt.Errorf("Telegram API %s returned invalid result: %w", method, err)
		}
	}
	return nil
}

func waitForTelegramRetry(ctx context.Context) bool {
	timer := time.NewTimer(telegramReconnectDelay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (c *telegramChannel) ensureBotIdentity(ctx context.Context) (string, int64) {
	c.mu.Lock()
	username := c.botUsername
	botID := c.botID
	c.mu.Unlock()
	if username != "" || botID != 0 {
		return username, botID
	}

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var me telegramUser
	if err := c.callAPI(reqCtx, "getMe", map[string]any{}, &me); err != nil {
		common.Warn("telegram: failed to fetch bot identity via getMe", zap.String("account_id", c.account.AccountID), zap.Error(err))
		return "", 0
	}

	username = normalizeTelegramUsername(me.Username)
	botID = me.ID

	c.mu.Lock()
	if c.botUsername == "" {
		c.botUsername = username
	}
	if c.botID == 0 {
		c.botID = botID
	}
	username = c.botUsername
	botID = c.botID
	c.mu.Unlock()

	return username, botID
}

func normalizeTelegramUsername(username string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(username), "@"))
}

func isTelegramUsernameChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

func matchAndStripTelegramBotMention(
	text string,
	entities []telegramMessageEntity,
	botUsername string,
	botID int64,
) (bool, string) {
	botUsername = normalizeTelegramUsername(botUsername)
	mentioned := false

	if botID != 0 {
		for _, ent := range entities {
			if ent.Type == "text_mention" && ent.User != nil && ent.User.ID == botID {
				mentioned = true
				break
			}
		}
	}

	if botUsername == "" {
		return mentioned, text
	}

	target := "@" + botUsername
	targetLen := len(target)
	if len(text) < targetLen {
		return mentioned, text
	}

	var b strings.Builder
	b.Grow(len(text))
	lastCopied := 0
	i := 0
	for i <= len(text)-targetLen {
		if text[i] == '@' && strings.EqualFold(text[i:i+targetLen], target) {
			endIdx := i + targetLen
			afterOK := endIdx == len(text) || !isTelegramUsernameChar(text[endIdx])

			beforeOK := i == 0 || !isTelegramUsernameChar(text[i-1])
			if !beforeOK {
				wordStart := i - 1
				for wordStart > 0 && isTelegramUsernameChar(text[wordStart-1]) {
					wordStart--
				}
				if wordStart > 0 && text[wordStart-1] == '/' {
					beforeOK = true
				}
			}

			if beforeOK && afterOK {
				mentioned = true
				b.WriteString(text[lastCopied:i])
				lastCopied = endIdx
				i = endIdx
				continue
			}
		}
		i++
	}

	if !mentioned {
		return false, text
	}
	b.WriteString(text[lastCopied:])
	cleaned := strings.TrimSpace(b.String())
	return true, cleaned
}

func telegramBoolFromConfig(cfg map[string]any, defaultValue bool, keys ...string) bool {
	if cfg == nil {
		return defaultValue
	}
	for _, key := range keys {
		raw, exists := cfg[key]
		if !exists || raw == nil {
			continue
		}
		switch v := raw.(type) {
		case bool:
			return v
		case string:
			trimmed := strings.ToLower(strings.TrimSpace(v))
			switch trimmed {
			case "true", "1", "yes", "on":
				return true
			case "false", "0", "no", "off":
				return false
			}
		case int:
			return v != 0
		case int64:
			return v != 0
		case float64:
			return v != 0
		}
	}
	return defaultValue
}

func isEffectiveTelegramReply(msg *telegramMessage) bool {
	if msg == nil || msg.ReplyToMessage == nil || msg.ReplyToMessage.MessageID == 0 {
		return false
	}
	reply := msg.ReplyToMessage
	replyText := strings.TrimSpace(reply.Text)
	if replyText == "" {
		replyText = strings.TrimSpace(reply.Caption)
	}
	// In Telegram forum supergroups, un-replied messages inside a topic carry a
	// synthetic ReplyToMessage whose message_id equals message_thread_id and has no text.
	if msg.MessageThreadID != 0 && reply.MessageID == msg.MessageThreadID && replyText == "" {
		return false
	}
	return true
}

func isTelegramReplyToBot(reply *telegramMessage, botUsername string, botID int64) bool {
	if reply == nil || reply.From == nil {
		return false
	}
	if botID != 0 && reply.From.ID == botID {
		return true
	}
	botUsername = normalizeTelegramUsername(botUsername)
	if botUsername != "" && normalizeTelegramUsername(reply.From.Username) == botUsername {
		return true
	}
	return false
}

func telegramUserFullName(u *telegramUser) string {
	if u == nil {
		return ""
	}
	first := strings.TrimSpace(u.FirstName)
	last := strings.TrimSpace(u.LastName)
	if first != "" && last != "" {
		return first + " " + last
	}
	if first != "" {
		return first
	}
	return last
}

func telegramUserDisplayLabel(u *telegramUser) string {
	if u == nil {
		return ""
	}
	username := strings.TrimPrefix(strings.TrimSpace(u.Username), "@")
	fullName := telegramUserFullName(u)
	if username != "" && fullName != "" {
		return "@" + username + " (" + fullName + ")"
	}
	if username != "" {
		return "@" + username
	}
	if fullName != "" {
		return fullName
	}
	if u.ID != 0 {
		return strconv.FormatInt(u.ID, 10)
	}
	return ""
}

func (c *telegramChannel) resolveGroupThreadID(chatID int64, messageID int64, replyToMsgID string) string {
	chatIDStr := strconv.FormatInt(chatID, 10)
	msgIDStr := strconv.FormatInt(messageID, 10)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.threadRootByMsg == nil {
		c.threadRootByMsg = make(map[string]string)
	}

	replyToMsgID = strings.TrimSpace(replyToMsgID)
	if replyToMsgID != "" {
		parentKey := chatIDStr + ":" + replyToMsgID
		rootID := c.threadRootByMsg[parentKey]
		if rootID == "" {
			rootID = replyToMsgID
			c.rememberThreadRootLocked(parentKey, rootID)
		}
		if msgIDStr != "0" {
			c.rememberThreadRootLocked(chatIDStr+":"+msgIDStr, rootID)
		}
		return rootID
	}

	if msgIDStr == "0" {
		return ""
	}
	c.rememberThreadRootLocked(chatIDStr+":"+msgIDStr, msgIDStr)
	return msgIDStr
}

func (c *telegramChannel) bindBotReplyThread(chatID int64, replyToMsgID, botMsgID string) {
	replyToMsgID = strings.TrimSpace(replyToMsgID)
	botMsgID = strings.TrimSpace(botMsgID)
	if replyToMsgID == "" || botMsgID == "" {
		return
	}
	chatIDStr := strconv.FormatInt(chatID, 10)

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.threadRootByMsg == nil {
		c.threadRootByMsg = make(map[string]string)
	}
	parentKey := chatIDStr + ":" + replyToMsgID
	rootID := c.threadRootByMsg[parentKey]
	if rootID == "" {
		rootID = replyToMsgID
		c.rememberThreadRootLocked(parentKey, rootID)
	}
	c.rememberThreadRootLocked(chatIDStr+":"+botMsgID, rootID)
}

func (c *telegramChannel) rememberThreadRootLocked(key, rootID string) {
	if _, exists := c.threadRootByMsg[key]; !exists {
		if len(c.threadOrder) >= maxTelegramThreadCache {
			evictCount := maxTelegramThreadCache / 10
			if evictCount < 1 {
				evictCount = 1
			}
			for _, oldKey := range c.threadOrder[:evictCount] {
				delete(c.threadRootByMsg, oldKey)
			}
			c.threadOrder = c.threadOrder[evictCount:]
		}
		c.threadOrder = append(c.threadOrder, key)
	}
	c.threadRootByMsg[key] = rootID
}
