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
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"ragflow/internal/channels/core"
)

type telegramRoundTripperFunc func(*http.Request) (*http.Response, error)

func (f telegramRoundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewTelegramChannelFromConfigRequiresToken(t *testing.T) {
	if _, err := newTelegramChannelFromConfig("account-1", map[string]any{}); err == nil {
		t.Fatal("newTelegramChannelFromConfig accepted a missing token")
	}
}

func TestTelegramChatType(t *testing.T) {
	tests := map[string]string{
		"private":    "p2p",
		"group":      "group",
		"supergroup": "group",
		"channel":    "channel",
		"":           "unknown",
		"custom":     "custom",
	}
	for input, want := range tests {
		if got := telegramChatType(input); got != want {
			t.Fatalf("telegramChatType(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestTelegramHandleUpdateMapsCaptionAndIgnoresBots(t *testing.T) {
	ch := newTelegramChannel(telegramAccount{
		AccountID:         "account-1",
		Token:             "token",
		AllowGroupPrivacy: true,
	})
	messages := make(chan core.IncomingMessage, 1)
	ch.SetMessageHandler(func(_ context.Context, msg core.IncomingMessage) error {
		messages <- msg
		return nil
	})

	ch.handleUpdate(context.Background(), telegramUpdate{
		UpdateID: 1,
		Message: &telegramMessage{
			MessageID: 42,
			From:      &telegramUser{ID: 7},
			Chat:      telegramChat{ID: -10, Type: "supergroup"},
			Caption:   "caption text",
		},
	})
	ch.handleUpdate(context.Background(), telegramUpdate{
		UpdateID: 2,
		Message: &telegramMessage{
			MessageID: 43,
			From:      &telegramUser{ID: 8, IsBot: true},
		},
	})

	select {
	case msg := <-messages:
		if msg.ChatID != "-10" || msg.ChatType != "group" || msg.MessageID != "42" || msg.SenderID != "7" || msg.Text != "caption text" {
			t.Fatalf("unexpected incoming message: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for incoming message")
	}

	select {
	case msg := <-messages:
		t.Fatalf("bot message was dispatched: %+v", msg)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestTelegramHandleUpdateMapsChannelPostWithoutSender(t *testing.T) {
	ch := newTelegramChannel(telegramAccount{AccountID: "account-1", Token: "token"})
	messages := make(chan core.IncomingMessage, 1)
	ch.SetMessageHandler(func(_ context.Context, msg core.IncomingMessage) error {
		messages <- msg
		return nil
	})

	ch.handleUpdate(context.Background(), telegramUpdate{
		ChannelPost: &telegramMessage{
			MessageID: 42,
			Chat:      telegramChat{ID: -100123, Type: "channel"},
			Text:      "channel post",
		},
	})

	select {
	case msg := <-messages:
		if msg.ChatID != "-100123" || msg.ChatType != "channel" || msg.MessageID != "42" || msg.SenderID != "-100123" || msg.Text != "channel post" {
			t.Fatalf("unexpected incoming message: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for channel post")
	}
}

func TestTelegramCallAPISanitizesTransportError(t *testing.T) {
	const token = "sensitive-bot-token"
	transportErr := &url.Error{
		Op:  "Post",
		URL: "https://api.telegram.org/bot" + token + "/sendMessage",
		Err: errors.New("connection refused"),
	}
	ch := newTelegramChannel(telegramAccount{AccountID: "account-1", Token: token})
	ch.client = &http.Client{Transport: telegramRoundTripperFunc(func(*http.Request) (*http.Response, error) {
		return nil, transportErr
	})}

	err := ch.callAPI(context.Background(), "sendMessage", map[string]any{}, nil)
	if err == nil {
		t.Fatal("callAPI() error = nil")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("callAPI() exposed token in error: %v", err)
	}
	if !strings.Contains(err.Error(), "sendMessage") {
		t.Fatalf("callAPI() error = %q, want method context", err)
	}
	if !errors.Is(err, transportErr) {
		t.Fatalf("callAPI() error does not unwrap transport error: %v", err)
	}
}

func TestTelegramSendUsesReplyParameters(t *testing.T) {
	request := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/sendMessage") {
			t.Errorf("unexpected Telegram API path: %s", r.URL.Path)
			http.Error(w, "unexpected path", http.StatusNotFound)
			return
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		request <- payload
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer server.Close()

	ch := newTelegramChannel(telegramAccount{
		AccountID:  "account-1",
		Token:      "token",
		APIBaseURL: server.URL,
	})
	if err := ch.Send(context.Background(), core.OutgoingMessage{
		ChatID:           "-100",
		Text:             "answer",
		ReplyToMessageID: "12",
	}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	payload := <-request
	if payload["chat_id"] != float64(-100) || payload["text"] != "answer" {
		t.Fatalf("unexpected send payload: %+v", payload)
	}
	reply, ok := payload["reply_parameters"].(map[string]any)
	if !ok || reply["message_id"] != float64(12) || reply["allow_sending_without_reply"] != true {
		t.Fatalf("unexpected reply parameters: %+v", payload["reply_parameters"])
	}
}

func TestTelegramSendIgnoresInvalidChatID(t *testing.T) {
	ch := newTelegramChannel(telegramAccount{AccountID: "account-1", Token: "token"})
	if err := ch.Send(context.Background(), core.OutgoingMessage{ChatID: "invalid", Text: "answer"}); err != nil {
		t.Fatalf("Send() error = %v, want nil", err)
	}
}

func TestTelegramStartAndStopPollsUpdates(t *testing.T) {
	updateReturned := false
	typingActions := make(chan map[string]any, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/deleteWebhook"):
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			if !updateReturned {
				updateReturned = true
				_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":9,"message":{"message_id":3,"from":{"id":4,"is_bot":false},"chat":{"id":5,"type":"private"},"text":"hello"}}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		case strings.HasSuffix(r.URL.Path, "/sendChatAction"):
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
				select {
				case typingActions <- payload:
				default:
				}
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		case strings.HasSuffix(r.URL.Path, "/sendMessage"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
		default:
			t.Errorf("unexpected Telegram API path: %s", r.URL.Path)
			http.Error(w, "unexpected path", http.StatusNotFound)
		}
	}))
	defer server.Close()

	ch := newTelegramChannel(telegramAccount{
		AccountID:  "account-1",
		Token:      "token",
		APIBaseURL: server.URL,
	})
	message := make(chan core.IncomingMessage, 1)
	ch.SetMessageHandler(func(_ context.Context, msg core.IncomingMessage) error {
		message <- msg
		return nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := ch.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case msg := <-message:
		if msg.Text != "hello" || msg.MessageID != "3" || ch.offset != 10 {
			t.Fatalf("unexpected polled message: %+v, offset=%d", msg, ch.offset)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for polled message")
	}

	select {
	case action := <-typingActions:
		if action["chat_id"] != float64(5) || action["action"] != "typing" {
			t.Fatalf("unexpected sendChatAction payload: %+v", action)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sendChatAction")
	}

	if err := ch.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestTelegramHandleUpdateRefreshesTypingActionWhileProcessing(t *testing.T) {
	typingCalls := make(chan map[string]any, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sendChatAction") {
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err == nil {
				typingCalls <- payload
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer server.Close()

	ch := newTelegramChannel(telegramAccount{
		AccountID:      "account-1",
		Token:          "token",
		APIBaseURL:     server.URL,
		TypingInterval: 15 * time.Millisecond,
	})
	ch.SetMessageHandler(func(_ context.Context, _ core.IncomingMessage) error {
		time.Sleep(40 * time.Millisecond)
		return nil
	})

	ch.handleUpdate(context.Background(), telegramUpdate{
		UpdateID: 1,
		Message: &telegramMessage{
			MessageID: 10,
			From:      &telegramUser{ID: 7},
			Chat:      telegramChat{ID: 99, Type: "private"},
			Text:      "hi",
		},
	})

	if len(typingCalls) < 2 {
		t.Fatalf("expected at least 2 sendChatAction calls during processing, got %d", len(typingCalls))
	}
	first := <-typingCalls
	if first["chat_id"] != float64(99) || first["action"] != "typing" {
		t.Fatalf("unexpected typing payload: %+v", first)
	}
}

func TestTelegramGroupPrivacyDefaultBlocksUnmentionedAndStripsMention(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":777,"is_bot":true,"username":"riversidegarden_bot"}}`))
		case strings.HasSuffix(r.URL.Path, "/sendChatAction"):
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	defer server.Close()

	ch, err := newTelegramChannelFromConfig("account-1", map[string]any{
		"token":        "token",
		"api_base_url": server.URL,
	})
	if err != nil {
		t.Fatalf("newTelegramChannelFromConfig() error = %v", err)
	}
	if ch.account.AllowGroupPrivacy {
		t.Fatal("expected AllowGroupPrivacy to default to false")
	}

	messages := make(chan core.IncomingMessage, 4)
	ch.SetMessageHandler(func(_ context.Context, msg core.IncomingMessage) error {
		messages <- msg
		return nil
	})

	// 1. Unmentioned message in group -> blocked
	ch.handleUpdate(context.Background(), telegramUpdate{
		UpdateID: 1,
		Message: &telegramMessage{
			MessageID: 10,
			From:      &telegramUser{ID: 7},
			Chat:      telegramChat{ID: -100, Type: "group"},
			Text:      "Mọi người ơi hôm nay họp lúc mấy giờ?",
		},
	})

	// 2. Prefix mention of a different bot (@riversidegarden_bot_v2) -> blocked
	ch.handleUpdate(context.Background(), telegramUpdate{
		UpdateID: 2,
		Message: &telegramMessage{
			MessageID: 11,
			From:      &telegramUser{ID: 7},
			Chat:      telegramChat{ID: -100, Type: "supergroup"},
			Text:      "@riversidegarden_bot_v2 Quy trình kế toán là gì?",
		},
	})

	select {
	case msg := <-messages:
		t.Fatalf("unmentioned group message should be blocked when allowGroupPrivacy=false, got: %+v", msg)
	case <-time.After(20 * time.Millisecond):
	}

	// 3. Exact mention (@RiversideGarden_Bot, case-insensitive) in group -> allowed & stripped
	ch.handleUpdate(context.Background(), telegramUpdate{
		UpdateID: 3,
		Message: &telegramMessage{
			MessageID: 12,
			From:      &telegramUser{ID: 7},
			Chat:      telegramChat{ID: -100, Type: "supergroup"},
			Text:      "@RiversideGarden_Bot Quy trình kế toán thu tiền phí thi công như thế nào?",
		},
	})

	select {
	case msg := <-messages:
		if msg.Text != "Quy trình kế toán thu tiền phí thi công như thế nào?" {
			t.Fatalf("expected stripped mention text, got %q", msg.Text)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mentioned group message")
	}

	// 4. DM (private) message without mention -> always allowed
	ch.handleUpdate(context.Background(), telegramUpdate{
		UpdateID: 4,
		Message: &telegramMessage{
			MessageID: 13,
			From:      &telegramUser{ID: 7},
			Chat:      telegramChat{ID: 7, Type: "private"},
			Text:      "Câu hỏi trực tiếp qua DM",
		},
	})

	select {
	case msg := <-messages:
		if msg.Text != "Câu hỏi trực tiếp qua DM" || msg.ChatType != "p2p" {
			t.Fatalf("unexpected DM message: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for DM message")
	}
}

func TestTelegramGroupPrivacyAllowedRespondsWithoutMention(t *testing.T) {
	ch, err := newTelegramChannelFromConfig("account-1", map[string]any{
		"token":             "token",
		"allowGroupPrivacy": true,
	})
	if err != nil {
		t.Fatalf("newTelegramChannelFromConfig() error = %v", err)
	}
	if !ch.account.AllowGroupPrivacy {
		t.Fatal("expected AllowGroupPrivacy to be true")
	}

	messages := make(chan core.IncomingMessage, 1)
	ch.SetMessageHandler(func(_ context.Context, msg core.IncomingMessage) error {
		messages <- msg
		return nil
	})

	ch.handleUpdate(context.Background(), telegramUpdate{
		UpdateID: 1,
		Message: &telegramMessage{
			MessageID: 20,
			From:      &telegramUser{ID: 7},
			Chat:      telegramChat{ID: -200, Type: "group"},
			Text:      "Tin nhắn trong group không cần tag bot",
		},
	})

	select {
	case msg := <-messages:
		if msg.Text != "Tin nhắn trong group không cần tag bot" {
			t.Fatalf("unexpected group message: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for group message with allowGroupPrivacy=true")
	}
}

func TestTelegramGroupReplyToBotAllowedWithoutMentionAndTracksReplyChain(t *testing.T) {
	var nextBotMsgID int64 = 100
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bottoken/getMe":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"id":       999001,
					"is_bot":   true,
					"username": "riversidegarden_bot",
				},
			})
		case "/bottoken/sendChatAction":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":     true,
				"result": true,
			})
		case "/bottoken/sendMessage":
			nextBotMsgID++
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok": true,
				"result": map[string]any{
					"message_id": nextBotMsgID,
				},
			})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	ch, err := newTelegramChannelFromConfig("account-1", map[string]any{
		"token":        "token",
		"api_base_url": server.URL,
	})
	if err != nil {
		t.Fatalf("newTelegramChannelFromConfig() error = %v", err)
	}

	messages := make(chan core.IncomingMessage, 8)
	ch.SetMessageHandler(func(ctx context.Context, msg core.IncomingMessage) error {
		messages <- msg
		return ch.Send(ctx, core.OutgoingMessage{
			ChatID:           msg.ChatID,
			Text:             "Bot answer for " + msg.MessageID,
			ReplyToMessageID: msg.MessageID,
		})
	})

	// Turn 1: User A starts a topic in group (#100) with @mention -> bot replies (#101)
	ch.handleUpdate(context.Background(), telegramUpdate{
		UpdateID: 1,
		Message: &telegramMessage{
			MessageID: 100,
			From:      &telegramUser{ID: 11, Username: "nguyenvana", FirstName: "Nguyen", LastName: "Van A"},
			Chat:      telegramChat{ID: -100555, Type: "supergroup", Title: "BQL Riverside"},
			Text:      "@riversidegarden_bot Giá căn 2PN là bao nhiêu?",
		},
	})

	turn1 := <-messages
	if turn1.ThreadID != "100" || turn1.Text != "Giá căn 2PN là bao nhiêu?" {
		t.Fatalf("unexpected turn 1: %+v", turn1)
	}
	if turn1.ChatTitle != "BQL Riverside" || turn1.SenderUsername != "nguyenvana" || turn1.SenderName != "Nguyen Van A" {
		t.Fatalf("unexpected turn 1 metadata: %+v", turn1)
	}

	// Turn 2: User B replies directly to Bot's message (#101) WITHOUT @mention -> allowed & stays on ThreadID "100"
	nextBotMsgID = 101 // bot's next reply will become #102
	ch.handleUpdate(context.Background(), telegramUpdate{
		UpdateID: 2,
		Message: &telegramMessage{
			MessageID: 105,
			From:      &telegramUser{ID: 22, Username: "lethib", FirstName: "Le", LastName: "Thi B"},
			Chat:      telegramChat{ID: -100555, Type: "supergroup", Title: "BQL Riverside"},
			ReplyToMessage: &telegramMessage{
				MessageID: 101,
				From:      &telegramUser{ID: 999001, IsBot: true, Username: "riversidegarden_bot"},
				Text:      "Giá căn 2PN là 3.5 tỷ.",
			},
			Text: "Giá đó đã gồm VAT chưa?",
		},
	})

	select {
	case turn2 := <-messages:
		if turn2.ThreadID != "100" {
			t.Fatalf("expected reply chain to stay on root ThreadID 100, got %q", turn2.ThreadID)
		}
		if !turn2.ReplyToIsBot || turn2.ReplyToMessageID != "101" || turn2.ReplyToText != "Giá căn 2PN là 3.5 tỷ." {
			t.Fatalf("unexpected turn 2 reply metadata: %+v", turn2)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for direct reply to bot in group")
	}

	// Turn 3: User A replies to Bot's 2nd reply (#102) WITHOUT @mention -> still stays on ThreadID "100"
	ch.handleUpdate(context.Background(), telegramUpdate{
		UpdateID: 3,
		Message: &telegramMessage{
			MessageID: 108,
			From:      &telegramUser{ID: 11, Username: "nguyenvana", FirstName: "Nguyen", LastName: "Van A"},
			Chat:      telegramChat{ID: -100555, Type: "supergroup", Title: "BQL Riverside"},
			ReplyToMessage: &telegramMessage{
				MessageID: 102,
				From:      &telegramUser{ID: 999001, IsBot: true, Username: "riversidegarden_bot"},
				Text:      "Giá 3.5 tỷ đã bao gồm VAT.",
			},
			Text: "Còn tiến độ thanh toán thì sao?",
		},
	})

	select {
	case turn3 := <-messages:
		if turn3.ThreadID != "100" {
			t.Fatalf("expected 3rd turn in chain to keep root ThreadID 100, got %q", turn3.ThreadID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for 3rd turn in reply chain")
	}

	// Turn 4: User C replies to another human member (#50) WITHOUT @mention -> MUST BE BLOCKED
	ch.handleUpdate(context.Background(), telegramUpdate{
		UpdateID: 4,
		Message: &telegramMessage{
			MessageID: 109,
			From:      &telegramUser{ID: 33, Username: "userc"},
			Chat:      telegramChat{ID: -100555, Type: "supergroup", Title: "BQL Riverside"},
			ReplyToMessage: &telegramMessage{
				MessageID: 50,
				From:      &telegramUser{ID: 44, IsBot: false, Username: "userd"},
				Text:      "Tối nay họp tổ dân phố nhé",
			},
			Text: "Ok mình nhớ rồi",
		},
	})

	select {
	case unexpected := <-messages:
		t.Fatalf("reply to human without @mention should be blocked, got: %+v", unexpected)
	case <-time.After(20 * time.Millisecond):
	}

	// Turn 5: User C replies to human member (#50) WITH @mention -> allowed, starts thread "50" with ReplyToText
	ch.handleUpdate(context.Background(), telegramUpdate{
		UpdateID: 5,
		Message: &telegramMessage{
			MessageID: 110,
			From:      &telegramUser{ID: 33, Username: "userc", FirstName: "User C"},
			Chat:      telegramChat{ID: -100555, Type: "supergroup", Title: "BQL Riverside"},
			ReplyToMessage: &telegramMessage{
				MessageID: 50,
				From:      &telegramUser{ID: 44, IsBot: false, Username: "userd", FirstName: "User D"},
				Text:      "Khách ở căn A12 báo hỏng điều hòa trung tâm",
			},
			Text: "@riversidegarden_bot quy trình bảo hành thiết bị này thế nào?",
		},
	})

	select {
	case turn5 := <-messages:
		if turn5.ThreadID != "50" {
			t.Fatalf("expected thread root 50, got %q", turn5.ThreadID)
		}
		if turn5.Text != "quy trình bảo hành thiết bị này thế nào?" {
			t.Fatalf("expected stripped mention, got %q", turn5.Text)
		}
		if turn5.ReplyToText != "Khách ở căn A12 báo hỏng điều hòa trung tâm" || turn5.ReplyToIsBot {
			t.Fatalf("unexpected reply context on turn 5: %+v", turn5)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for mentioned reply to human message")
	}
}
