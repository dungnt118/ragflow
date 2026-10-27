package service

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"ragflow/internal/channels/core"
	"ragflow/internal/common"
	"ragflow/internal/dao"
	"ragflow/internal/entity"
	"ragflow/internal/permission"
)

func setupChatChannelServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		TranslateError: true,
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	if err = db.AutoMigrate(
		&entity.ChatChannel{},
		&entity.Chat{},
		&entity.UserTenant{},
		&entity.User{},
		&entity.Tenant{},
		&entity.UserCanvas{},
		&entity.UserCanvasVersion{},
		&entity.APIToken{},
		&entity.API4Conversation{},
		&entity.API4ConversationMessage{},
		&entity.API4ConversationReference{},
	); err != nil {
		t.Fatalf("failed to migrate test schema: %v", err)
	}

	origDB := dao.DB
	dao.DB = db
	t.Cleanup(func() {
		dao.DB = origDB
	})

	return db
}

func createServiceTestDialog(t *testing.T, db *gorm.DB, id, tenantID, name string) *entity.Chat {
	t.Helper()

	dialogName := name
	status := string(entity.StatusValid)
	dialog := &entity.Chat{
		ID:           id,
		TenantID:     tenantID,
		Name:         &dialogName,
		LLMID:        "model-a",
		LLMSetting:   entity.JSONMap{"temperature": 0.1},
		PromptType:   "simple",
		PromptConfig: entity.JSONMap{"system": "sys"},
		KBIDs:        entity.JSONSlice{"kb-1"},
		Status:       &status,
	}
	if err := db.Create(dialog).Error; err != nil {
		t.Fatalf("failed to create dialog: %v", err)
	}
	return dialog
}

func createServiceTestChannel(t *testing.T, db *gorm.DB, channel *entity.ChatChannel) *entity.ChatChannel {
	t.Helper()

	if err := db.Create(channel).Error; err != nil {
		t.Fatalf("failed to create chat channel: %v", err)
	}
	return channel
}

func createServiceTestMembership(t *testing.T, db *gorm.DB, userID, tenantID string) {
	t.Helper()

	status := "1"
	member := &entity.UserTenant{
		ID:       userID + "_" + tenantID,
		UserID:   userID,
		TenantID: tenantID,
		Role:     "normal",
		Status:   &status,
	}
	if err := db.Create(member).Error; err != nil {
		t.Fatalf("failed to create tenant membership: %v", err)
	}
}

func TestChatChannelServiceCreateAndList(t *testing.T) {
	db := setupChatChannelServiceTestDB(t)
	createServiceTestDialog(t, db, "dialog-1", "tenant-1", "Assistant A")

	svc := NewChatChannelService()
	chatID := "dialog-1"

	ctx := t.Context()
	channel, err := svc.CreateChatChannel(
		ctx,
		"tenant-1",
		"bot-a",
		"dingtalk",
		entity.JSONMap{"token": "abc"},
		&chatID,
		nil,
	)
	if err != nil {
		t.Fatalf("CreateChatChannel failed: %v", err)
	}
	if channel.ID == "" {
		t.Fatal("expected generated id")
	}
	if channel.TenantID != "tenant-1" || channel.Name != "bot-a" || channel.Channel != "dingtalk" {
		t.Fatalf("unexpected created channel: %+v", channel)
	}

	list, err := svc.List(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 channel, got %d", len(list))
	}
	if list[0].DialogName == nil || *list[0].DialogName != "Assistant A" {
		t.Fatalf("expected joined dialog name, got %+v", list[0])
	}
}

func TestChatChannelServiceGetChatChannelAllowsJoinedTenant(t *testing.T) {
	db := setupChatChannelServiceTestDB(t)
	createServiceTestChannel(t, db, &entity.ChatChannel{
		ID:       "cc-1",
		TenantID: "tenant-1",
		Name:     "bot-a",
		Channel:  "wecom",
		Config:   entity.JSONMap{"token": "abc"},
		Status:   1,
	})
	createServiceTestMembership(t, db, "user-2", "tenant-1")

	ctx := t.Context()
	channel, code, err := NewChatChannelService().GetChatChannel(ctx, "user-2", "cc-1")
	if err != nil {
		t.Fatalf("GetChatChannel failed: %v", err)
	}
	if code != common.CodeSuccess {
		t.Fatalf("expected success code, got %v", code)
	}
	if channel == nil || channel.ID != "cc-1" {
		t.Fatalf("unexpected channel: %+v", channel)
	}
}

func TestChatChannelServiceListUsesPermissionScope(t *testing.T) {
	db := setupChatChannelServiceTestDB(t)
	createServiceTestChannel(t, db, &entity.ChatChannel{
		ID: "channel-team", TenantID: "tenant-1", Name: "team", Channel: "wecom", Config: entity.JSONMap{}, Status: 1,
	})
	createServiceTestChannel(t, db, &entity.ChatChannel{
		ID: "channel-other", TenantID: "tenant-2", Name: "other", Channel: "wecom", Config: entity.JSONMap{}, Status: 1,
	})
	createServiceTestMembership(t, db, "user-2", "tenant-1")

	channels, err := NewChatChannelService().List(t.Context(), "user-2")
	if err != nil {
		t.Fatalf("List() failed: %v", err)
	}
	if len(channels) != 1 || channels[0].ID != "channel-team" {
		t.Fatalf("List() = %+v, want only member-visible channel-team", channels)
	}
}

func TestChatChannelServiceUpdateChatChannelSuccess(t *testing.T) {
	db := setupChatChannelServiceTestDB(t)
	createServiceTestDialog(t, db, "dialog-1", "tenant-1", "Assistant A")
	createServiceTestDialog(t, db, "dialog-2", "tenant-1", "Assistant B")
	chatID := "dialog-1"
	createServiceTestChannel(t, db, &entity.ChatChannel{
		ID:       "cc-1",
		TenantID: "tenant-1",
		Name:     "bot-a",
		Channel:  "wecom",
		Config:   entity.JSONMap{"token": "old"},
		ChatID:   &chatID,
		Status:   1,
	})

	ctx := t.Context()
	updated, code, err := NewChatChannelService().UpdateChatChannel(ctx, "tenant-1", "cc-1", map[string]interface{}{
		"name":    "bot-b",
		"config":  map[string]interface{}{"token": "new"},
		"chat_id": "dialog-2",
	})
	if err != nil {
		t.Fatalf("UpdateChatChannel failed: %v", err)
	}
	if code != common.CodeSuccess {
		t.Fatalf("expected success code, got %v", code)
	}
	if updated.Name != "bot-b" {
		t.Fatalf("expected updated name, got %+v", updated)
	}
	if updated.ChatID == nil || *updated.ChatID != "dialog-2" {
		t.Fatalf("expected updated chat_id, got %+v", updated.ChatID)
	}
	if updated.Config["token"] != "new" {
		t.Fatalf("expected updated config, got %+v", updated.Config)
	}
}

func TestChatChannelServiceUpdateChatChannelRejectsCrossTenantDialog(t *testing.T) {
	db := setupChatChannelServiceTestDB(t)
	createServiceTestDialog(t, db, "dialog-2", "tenant-2", "Assistant B")
	createServiceTestChannel(t, db, &entity.ChatChannel{
		ID:       "cc-1",
		TenantID: "tenant-1",
		Name:     "bot-a",
		Channel:  "wecom",
		Config:   entity.JSONMap{"token": "old"},
		Status:   1,
	})

	ctx := t.Context()
	_, code, err := NewChatChannelService().UpdateChatChannel(ctx, "tenant-1", "cc-1", map[string]interface{}{
		"chat_id": "dialog-2",
	})
	if code != common.CodeNotFound {
		t.Fatalf("expected hidden not-found error, got %v", code)
	}
	if !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("expected permission denial, got %v", err)
	}
}

func TestChatChannelServiceUpdateChatChannelRequiresChannelOwnerAccess(t *testing.T) {
	db := setupChatChannelServiceTestDB(t)
	createServiceTestDialog(t, db, "dialog-2", "tenant-2", "Assistant B")
	createServiceTestChannel(t, db, &entity.ChatChannel{
		ID:       "cc-1",
		TenantID: "tenant-1",
		Name:     "bot-a",
		Channel:  "wecom",
		Config:   entity.JSONMap{"token": "old"},
		Status:   1,
	})
	createServiceTestMembership(t, db, "user-1", "tenant-1")
	createServiceTestMembership(t, db, "user-1", "tenant-2")

	_, code, err := NewChatChannelService().UpdateChatChannel(t.Context(), "user-1", "cc-1", map[string]interface{}{
		"chat_id": "dialog-2",
	})
	if code != common.CodeNotFound || !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("UpdateChatChannel() = (%d, %v), want hidden denial because channel owner cannot use Chat", code, err)
	}
}

func TestChatChannelServiceDeleteChatChannel(t *testing.T) {
	db := setupChatChannelServiceTestDB(t)
	createServiceTestChannel(t, db, &entity.ChatChannel{
		ID:       "cc-1",
		TenantID: "tenant-1",
		Name:     "bot-a",
		Channel:  "wecom",
		Config:   entity.JSONMap{"token": "old"},
		Status:   1,
	})

	svc := NewChatChannelService()

	ctx := t.Context()
	deleted, code, err := svc.DeleteChatChannel(ctx, "user-2", "cc-1")
	if code != common.CodeNotFound || !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("expected hidden permission denial, got code=%v err=%v", code, err)
	}
	if err == nil {
		t.Fatal("expected authorization error")
	}
	if deleted {
		t.Fatal("expected delete to be rejected")
	}

	deleted, code, err = svc.DeleteChatChannel(ctx, "tenant-1", "cc-1")
	if err != nil {
		t.Fatalf("DeleteChatChannel failed: %v", err)
	}
	if code != common.CodeSuccess || !deleted {
		t.Fatalf("unexpected delete result: deleted=%v code=%v err=%v", deleted, code, err)
	}

	if _, err = dao.NewChatChannel().GetByID(ctx, dao.DB, "cc-1"); err == nil {
		t.Fatal("expected deleted record to be gone")
	}
}

func countChannelSessions(t *testing.T, db *gorm.DB) int64 {
	t.Helper()

	var count int64
	if err := db.Model(&entity.ChatSession{}).Count(&count).Error; err != nil {
		t.Fatalf("failed to count sessions: %v", err)
	}
	return count
}

func TestGetOrCreateForChannelNamesFromFirstQueryAndChannelName(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)

	session, err := NewChatSessionService().GetOrCreateForChannel(
		t.Context(), "dialog-1", "cc-1", "discord-channel-1", "你好，帮我查一下退款政策", "Discord 客服",
	)
	if err != nil {
		t.Fatalf("GetOrCreateForChannel failed: %v", err)
	}
	if session.ID != channelSessionID("dialog-1", "cc-1", "discord-channel-1") {
		t.Fatalf("unexpected session id: %s", session.ID)
	}
	if session.DialogID != "dialog-1" {
		t.Fatalf("unexpected dialog id: %s", session.DialogID)
	}
	if session.Name == nil || *session.Name != "(channel: Discord 客服) 你好，帮我查一下退款政策" {
		t.Fatalf("unexpected session name: %v", session.Name)
	}
}

func TestGetOrCreateForChannelKeepsNameAfterFirstMessage(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	svc := NewChatSessionService()
	ctx := t.Context()

	first, err := svc.GetOrCreateForChannel(ctx, "dialog-1", "cc-1", "discord-channel-1", "第一句", "客服")
	if err != nil {
		t.Fatalf("first GetOrCreateForChannel failed: %v", err)
	}

	second, err := svc.GetOrCreateForChannel(ctx, "dialog-1", "cc-1", "discord-channel-1", "第二句", "改过的名字")
	if err != nil {
		t.Fatalf("second GetOrCreateForChannel failed: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected the same session, got %s and %s", first.ID, second.ID)
	}
	if second.Name == nil || *second.Name != "(channel: 客服) 第一句" {
		t.Fatalf("expected the first name to stick, got %v", second.Name)
	}
	if count := countChannelSessions(t, db); count != 1 {
		t.Fatalf("expected 1 session, got %d", count)
	}
}

func TestGetOrCreateForChannelTruncatesLongFirstQuery(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)

	query := strings.Repeat("长", 300)
	session, err := NewChatSessionService().GetOrCreateForChannel(
		t.Context(), "dialog-1", "cc-1", "discord-channel-1", query, "客服",
	)
	if err != nil {
		t.Fatalf("GetOrCreateForChannel failed: %v", err)
	}
	if session.Name == nil {
		t.Fatal("expected a session name")
	}
	name := *session.Name
	if utf8.RuneCountInString(name) != channelSessionNameMaxRunes {
		t.Fatalf("expected %d runes, got %d", channelSessionNameMaxRunes, utf8.RuneCountInString(name))
	}
	if !strings.HasPrefix(name, "(channel: 客服) ") {
		t.Fatalf("expected the channel prefix to survive, got %q", name)
	}
	wantPrefix := "(channel: 客服) " + string([]rune(query)[:channelSessionNameMaxRunes-utf8.RuneCountInString("(channel: 客服) ")])
	if name != wantPrefix {
		t.Fatalf("unexpected truncated name: %q", name)
	}
}

func TestGetOrCreateForChannelNormalizesWhitespace(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)

	session, err := NewChatSessionService().GetOrCreateForChannel(
		t.Context(), "dialog-1", "cc-1", "discord-channel-1", "你好\n\n帮我\t查一下", "客服",
	)
	if err != nil {
		t.Fatalf("GetOrCreateForChannel failed: %v", err)
	}
	if session.Name == nil || *session.Name != "(channel: 客服) 你好 帮我 查一下" {
		t.Fatalf("unexpected session name: %v", session.Name)
	}
}

func TestGetOrCreateForChannelStripsMentionMarkupFromName(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)

	session, err := NewChatSessionService().GetOrCreateForChannel(
		t.Context(), "dialog-1", "cc-1", "discord-channel-1", "<@1562356664286445598> 林黛玉是谁", "cc",
	)
	if err != nil {
		t.Fatalf("GetOrCreateForChannel failed: %v", err)
	}
	if session.Name == nil || *session.Name != "(channel: cc) 林黛玉是谁" {
		t.Fatalf("unexpected session name: %v", session.Name)
	}
}

func TestChannelSessionNameStripsMentionMarkup(t *testing.T) {
	tests := []struct {
		name        string
		firstQuery  string
		channelName string
		want        string
	}{
		{
			name:        "user mention prefix",
			firstQuery:  "<@1562356664286445598> 林黛玉是谁",
			channelName: "cc",
			want:        "(channel: cc) 林黛玉是谁",
		},
		{
			name:        "nickname mention and role mention",
			firstQuery:  "<@!42> <@&7> 查一下退款政策",
			channelName: "客服",
			want:        "(channel: 客服) 查一下退款政策",
		},
		{
			name:        "channel mention",
			firstQuery:  "<#99> 历史消息",
			channelName: "客服",
			want:        "(channel: 客服) 历史消息",
		},
		{
			name:        "mention only falls back to the channel label",
			firstQuery:  "<@1562356664286445598>",
			channelName: "cc",
			want:        "(channel: cc)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := channelSessionName(tt.firstQuery, tt.channelName); got != tt.want {
				t.Fatalf("channelSessionName(%q, %q) = %q, want %q", tt.firstQuery, tt.channelName, got, tt.want)
			}
		})
	}
}

func TestGetOrCreateForChannelFallbackNames(t *testing.T) {
	t.Run("blank query uses the channel label", func(t *testing.T) {
		db := setupServiceTestDB(t)
		pushServiceDB(t, db)

		session, err := NewChatSessionService().GetOrCreateForChannel(
			t.Context(), "dialog-1", "cc-1", "discord-channel-1", "  \n ", "客服",
		)
		if err != nil {
			t.Fatalf("GetOrCreateForChannel failed: %v", err)
		}
		if session.Name == nil || *session.Name != "(channel: 客服)" {
			t.Fatalf("unexpected session name: %v", session.Name)
		}
	})

	t.Run("blank query and channel name use the legacy name", func(t *testing.T) {
		db := setupServiceTestDB(t)
		pushServiceDB(t, db)

		session, err := NewChatSessionService().GetOrCreateForChannel(
			t.Context(), "dialog-1", "cc-1", "discord-channel-1", "", "",
		)
		if err != nil {
			t.Fatalf("GetOrCreateForChannel failed: %v", err)
		}
		if session.Name == nil || *session.Name != "channel:cc-1:discord-channel-1" {
			t.Fatalf("unexpected session name: %v", session.Name)
		}
	})
}

func TestTelegramGroupThreadSessionIsolationAndContextInjection(t *testing.T) {
	db := setupServiceTestDB(t)
	pushServiceDB(t, db)
	ctx := t.Context()

	svc := NewChatChannelService()

	// Thread 1 (Root Message #100 in group -100555)
	msg1 := ChatChannelIncomingMessage{
		Channel:        "telegram",
		AccountID:      "cc-tg",
		ChatID:         "-100555",
		ChatType:       "group",
		ChatTitle:      "BQL Cư Dân",
		MessageID:      "100",
		ThreadID:       "100",
		SenderID:       "11",
		SenderName:     "Nguyen Van A",
		SenderUsername: "nguyenvana",
		Text:           "Giá dịch vụ tháng này tính thế nào?",
	}
	sender1 := formatChannelSenderDisplay(msg1.SenderUsername, msg1.SenderName, msg1.SenderID, msg1.ChatID)
	label1 := formatChannelSessionLabel("Riverside Bot", msg1.ChatType, msg1.ChatTitle, sender1)
	sess1, err := svc.resolveOrCreateChannelSession(ctx, "dialog-1", "cc-tg", label1, sender1, msg1)
	if err != nil {
		t.Fatalf("resolveOrCreateChannelSession(msg1) error: %v", err)
	}
	if sess1.Name == nil || !strings.HasPrefix(*sess1.Name, "(channel: Riverside Bot | Nhóm: BQL Cư Dân | @nguyenvana (Nguyen Van A)) ") {
		t.Fatalf("unexpected group thread session name: %v", sess1.Name)
	}
	if sess1.UserID == nil || *sess1.UserID != "@nguyenvana (Nguyen Van A)" {
		t.Fatalf("expected session UserID to record sender display, got %v", sess1.UserID)
	}

	// Follow-up in Thread 1 (Message #105 replying to Bot #101 in same thread "100")
	msg1FollowUp := ChatChannelIncomingMessage{
		Channel:          "telegram",
		AccountID:        "cc-tg",
		ChatID:           "-100555",
		ChatType:         "group",
		ChatTitle:        "BQL Cư Dân",
		MessageID:        "105",
		ThreadID:         "100",
		SenderID:         "22",
		SenderName:       "Le Thi B",
		SenderUsername:   "lethib",
		ReplyToMessageID: "101",
		ReplyToText:      "Phí dịch vụ là 12.000đ/m2.",
		ReplyToIsBot:     true,
		Text:             "Đã bao gồm phí gửi xe chưa?",
	}
	sender1FollowUp := formatChannelSenderDisplay(msg1FollowUp.SenderUsername, msg1FollowUp.SenderName, msg1FollowUp.SenderID, msg1FollowUp.ChatID)
	label1FollowUp := formatChannelSessionLabel("Riverside Bot", msg1FollowUp.ChatType, msg1FollowUp.ChatTitle, sender1FollowUp)
	sess1FollowUp, err := svc.resolveOrCreateChannelSession(ctx, "dialog-1", "cc-tg", label1FollowUp, sender1FollowUp, msg1FollowUp)
	if err != nil {
		t.Fatalf("resolveOrCreateChannelSession(msg1FollowUp) error: %v", err)
	}
	if sess1FollowUp.ID != sess1.ID {
		t.Fatalf("expected reply in same thread to reuse session %s, got %s", sess1.ID, sess1FollowUp.ID)
	}

	// Thread 2 (Unrelated topic in the same group -100555, Message #200) -> MUST get a distinct Session!
	msg2 := ChatChannelIncomingMessage{
		Channel:          "telegram",
		AccountID:        "cc-tg",
		ChatID:           "-100555",
		ChatType:         "group",
		ChatTitle:        "BQL Cư Dân",
		MessageID:        "201",
		ThreadID:         "200",
		SenderID:         "33",
		SenderName:       "Tran Van C",
		SenderUsername:   "tranvanc",
		ReplyToMessageID: "200",
		ReplyToSender:    "@userd (User D)",
		ReplyToText:      "Khách ở căn A12 báo hỏng điều hòa trung tâm",
		ReplyToIsBot:     false,
		Text:             "quy trình bảo hành thiết bị này thế nào?",
	}
	sender2 := formatChannelSenderDisplay(msg2.SenderUsername, msg2.SenderName, msg2.SenderID, msg2.ChatID)
	label2 := formatChannelSessionLabel("Riverside Bot", msg2.ChatType, msg2.ChatTitle, sender2)
	sess2, err := svc.resolveOrCreateChannelSession(ctx, "dialog-1", "cc-tg", label2, sender2, msg2)
	if err != nil {
		t.Fatalf("resolveOrCreateChannelSession(msg2) error: %v", err)
	}
	if sess2.ID == sess1.ID {
		t.Fatalf("expected distinct session for new thread in same group, both got %s", sess2.ID)
	}

	// Verify contextual query injection when replying to a message not yet in the session
	enrichedQuery := buildContextualChannelQuery(msg2, nil)
	if !strings.Contains(enrichedQuery, "Khách ở căn A12 báo hỏng điều hòa trung tâm") || !strings.Contains(enrichedQuery, "quy trình bảo hành thiết bị này thế nào?") {
		t.Fatalf("expected buildContextualChannelQuery to inject replied-to text, got %q", enrichedQuery)
	}

	// Verify callback session resolution maps assistant message ID back to its thread session
	rememberChannelReplySession("assistant-msg-thread-2", sess2.ID)
	resolvedSessionID := svc.resolveChannelCallbackSessionID(ctx, "dialog-1", "cc-tg", "-100555", "assistant-msg-thread-2")
	if resolvedSessionID != sess2.ID {
		t.Fatalf("expected callback session ID %s, got %s", sess2.ID, resolvedSessionID)
	}
}

func createServiceTestCanvas(t *testing.T, db *gorm.DB, id, userID, title, category string, dsl entity.JSONMap) *entity.UserCanvas {
	t.Helper()
	tStr := title
	cType := "agent"
	uc := &entity.UserCanvas{
		ID:             id,
		UserID:         userID,
		Title:          &tStr,
		Permission:     "me",
		CanvasType:     &cType,
		CanvasCategory: category,
		DSL:            dsl,
	}
	if err := db.Create(uc).Error; err != nil {
		t.Fatalf("failed to create user canvas: %v", err)
	}
	return uc
}

func TestChatChannelServiceAgentBindingMutualExclusionAndAntiIDOR(t *testing.T) {
	db := setupChatChannelServiceTestDB(t)
	createServiceTestDialog(t, db, "dialog-1", "tenant-1", "Assistant A")
	createServiceTestCanvas(t, db, "agent-1", "tenant-1", "Agent QLTN", "agent_canvas", entity.JSONMap{})
	createServiceTestCanvas(t, db, "agent-other", "tenant-2", "Foreign Agent", "agent_canvas", entity.JSONMap{})
	createServiceTestCanvas(t, db, "dataflow-1", "tenant-1", "Dataflow Pipeline", "dataflow_canvas", entity.JSONMap{})
	createServiceTestCanvas(t, db, "dataflow-other", "tenant-2", "Foreign Dataflow", "dataflow_canvas", entity.JSONMap{})

	svc := NewChatChannelService()
	ctx := t.Context()

	// 1. Create with both chat_id and agent_id -> must reject with ChatChannelDataError
	chatID := "dialog-1"
	agentID := "agent-1"
	if _, err := svc.CreateChatChannel(ctx, "tenant-1", "bot-both", "telegram", entity.JSONMap{}, &chatID, &agentID); err == nil {
		t.Fatal("expected mutual exclusion error when both chat_id and agent_id are set")
	}

	// 2. Create with agent_id and whitespace chat_id -> normalizes whitespace to nil and succeeds
	blankChat := "   "
	ch, err := svc.CreateChatChannel(ctx, "tenant-1", "Thần Nông FAQ-Bot", "telegram", entity.JSONMap{"bot_token": "tg"}, &blankChat, &agentID)
	if err != nil {
		t.Fatalf("CreateChatChannel with agent_id failed: %v", err)
	}
	if ch.AgentID == nil || *ch.AgentID != "agent-1" || ch.ChatID != nil {
		t.Fatalf("expected agent_id=agent-1 and chat_id=nil, got agent_id=%v chat_id=%v", ch.AgentID, ch.ChatID)
	}

	// 3. List returns agent_id and agent_name
	list, err := svc.List(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(list) != 1 || list[0].AgentID == nil || *list[0].AgentID != "agent-1" || list[0].AgentName == nil || *list[0].AgentName != "Agent QLTN" {
		t.Fatalf("unexpected List response: %+v", list[0])
	}

	// 4. Switch from Agent to Chat Assistant -> clears agent_id = nil
	updated, code, err := svc.UpdateChatChannel(ctx, "tenant-1", ch.ID, map[string]interface{}{
		"chat_id": "dialog-1",
	})
	if err != nil || code != common.CodeSuccess {
		t.Fatalf("UpdateChatChannel to chat_id failed: code=%v err=%v", code, err)
	}
	if updated.ChatID == nil || *updated.ChatID != "dialog-1" || updated.AgentID != nil {
		t.Fatalf("expected chat_id=dialog-1 and agent_id=nil, got chat_id=%v agent_id=%v", updated.ChatID, updated.AgentID)
	}

	// 5. Switch from Chat Assistant back to Agent -> clears chat_id = nil
	updated, code, err = svc.UpdateChatChannel(ctx, "tenant-1", ch.ID, map[string]interface{}{
		"agent_id": "agent-1",
	})
	if err != nil || code != common.CodeSuccess {
		t.Fatalf("UpdateChatChannel to agent_id failed: code=%v err=%v", code, err)
	}
	if updated.AgentID == nil || *updated.AgentID != "agent-1" || updated.ChatID != nil {
		t.Fatalf("expected agent_id=agent-1 and chat_id=nil, got agent_id=%v chat_id=%v", updated.AgentID, updated.ChatID)
	}

	// 6. Anti-IDOR: Cross-tenant agent_canvas -> CodeNotFound
	if _, code, err = svc.UpdateChatChannel(ctx, "tenant-1", ch.ID, map[string]interface{}{
		"agent_id": "agent-other",
	}); code != common.CodeNotFound || !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("expected CodeNotFound for cross-tenant agent, got code=%v err=%v", code, err)
	}

	// 7. Anti-IDOR order: Cross-tenant dataflow_canvas -> MUST return CodeNotFound (permission check before category check)
	if _, code, err = svc.UpdateChatChannel(ctx, "tenant-1", ch.ID, map[string]interface{}{
		"agent_id": "dataflow-other",
	}); code != common.CodeNotFound || !errors.Is(err, permission.ErrPermissionDenied) {
		t.Fatalf("expected CodeNotFound for cross-tenant dataflow canvas (no category oracle), got code=%v err=%v", code, err)
	}

	// 8. Own dataflow_canvas -> CodeDataError ("Can't find this Agent!")
	if _, code, err = svc.UpdateChatChannel(ctx, "tenant-1", ch.ID, map[string]interface{}{
		"agent_id": "dataflow-1",
	}); code != common.CodeDataError || err == nil || !strings.Contains(err.Error(), "Can't find this Agent!") {
		t.Fatalf("expected CodeDataError 'Can't find this Agent!' for own dataflow_canvas, got code=%v err=%v", code, err)
	}

	// 9. Unbind Agent by passing agent_id: "" -> clears agent_id = nil
	updated, code, err = svc.UpdateChatChannel(ctx, "tenant-1", ch.ID, map[string]interface{}{
		"agent_id": "",
	})
	if err != nil || code != common.CodeSuccess || updated.AgentID != nil {
		t.Fatalf("expected agent_id=nil after unbind, got code=%v err=%v agent_id=%v", code, err, updated.AgentID)
	}
}

func TestChatChannelServiceHandleIncomingAgentMessageMultiTurnAndFeedback(t *testing.T) {
	db := setupChatChannelServiceTestDB(t)
	ctx := t.Context()

	dsl := map[string]any{
		"globals": map[string]any{},
		"components": map[string]any{
			"begin_0": map[string]any{
				"obj": map[string]any{
					"component_name": "Begin",
					"params":         map[string]any{},
				},
				"downstream": []any{"message_0"},
			},
			"message_0": map[string]any{
				"obj": map[string]any{
					"component_name": "Message",
					"params":         map[string]any{"text": "Agent trả lời: {{sys.query}}"},
				},
				"upstream": []any{"begin_0"},
			},
		},
		"path": []any{"begin_0", "message_0"},
		"feedback_config": map[string]any{
			"enabled": true,
			"mode":    "multi_criteria",
			"criteria": []any{
				map[string]any{"key": "accuracy", "label": "Độ chính xác", "type": "binary"},
				map[string]any{"key": "satisfaction", "label": "Mức độ hài lòng", "type": "stars"},
			},
		},
	}

	makeCanvasWithDSL(t, "agent-qltn", "tenant-1", "tenant-1", "v-qltn-1", dsl)
	// Also save feedback_config on UserCanvas.DSL so channel feedback button builder reads it
	if err := db.Model(&entity.UserCanvas{}).Where("id = ?", "agent-qltn").Update("dsl", entity.JSONMap(dsl)).Error; err != nil {
		t.Fatalf("update canvas dsl: %v", err)
	}

	agentID := "agent-qltn"
	createServiceTestChannel(t, db, &entity.ChatChannel{
		ID:       "cc-tg-agent",
		TenantID: "tenant-1",
		Name:     "Thần Nông FAQ-Bot",
		Channel:  "telegram",
		Config:   entity.JSONMap{"bot_token": "tg-token"},
		AgentID:  &agentID,
		Status:   1,
	})

	svc := NewChatChannelService()

	// Turn 1: Incoming Telegram message
	msg1 := ChatChannelIncomingMessage{
		Channel:        "telegram",
		AccountID:      "cc-tg-agent",
		ChatID:         "998877",
		ChatType:       "private",
		MessageID:      "501",
		SenderID:       "998877",
		SenderName:     "Hoàng Nông Dân",
		SenderUsername: "hoangnd",
		Text:           "Lúa bị vàng lá phải làm sao?",
	}
	reply1, err := svc.HandleIncomingChannelMessage(ctx, msg1)
	if err != nil {
		t.Fatalf("HandleIncomingChannelMessage turn 1 failed: %v", err)
	}
	if reply1 == nil || !strings.Contains(reply1.Answer, "Agent trả lời: Lúa bị vàng lá phải làm sao?") {
		t.Fatalf("unexpected reply1: %+v", reply1)
	}
	if reply1.MessageID == "" {
		t.Fatal("expected non-empty reply1.MessageID")
	}
	if len(reply1.InlineButtons) == 0 {
		t.Fatal("expected inline rating buttons when feedback_config is enabled on Agent DSL")
	}

	// Verify API4Conversation row preserves user_id = tenant-1 and sets exp_user_id + session name
	sessionID := channelSessionID("agent-qltn", "cc-tg-agent", "998877")
	conv, err := dao.NewAPI4ConversationDAO().GetBySessionID(ctx, db, sessionID, "agent-qltn")
	if err != nil || conv == nil {
		t.Fatalf("failed to load agent session %s: %v", sessionID, err)
	}
	if conv.UserID != "tenant-1" {
		t.Fatalf("expected conv.UserID to stay channel.TenantID ('tenant-1'), got %q", conv.UserID)
	}
	if conv.ExpUserID == nil || *conv.ExpUserID != "@hoangnd (Hoàng Nông Dân)" {
		t.Fatalf("expected conv.ExpUserID '@hoangnd (Hoàng Nông Dân)', got %v", conv.ExpUserID)
	}
	if conv.Name == nil || !strings.HasPrefix(*conv.Name, "(channel: Thần Nông FAQ-Bot | @hoangnd (Hoàng Nông Dân)) Lúa bị vàng lá phải làm sao?") {
		t.Fatalf("unexpected conv.Name: %v", conv.Name)
	}

	// Turn 2: Multi-turn follow-up on the same session MUST succeed without permission or UpdateHistory failure
	msg2 := ChatChannelIncomingMessage{
		Channel:        "telegram",
		AccountID:      "cc-tg-agent",
		ChatID:         "998877",
		ChatType:       "private",
		MessageID:      "502",
		SenderID:       "998877",
		SenderName:     "Hoàng Nông Dân",
		SenderUsername: "hoangnd",
		Text:           "Bón phân gì để phục hồi?",
	}
	reply2, err := svc.HandleIncomingChannelMessage(ctx, msg2)
	if err != nil {
		t.Fatalf("HandleIncomingChannelMessage turn 2 failed: %v", err)
	}
	if reply2 == nil || !strings.Contains(reply2.Answer, "Agent trả lời: Bón phân gì để phục hồi?") {
		t.Fatalf("unexpected reply2: %+v", reply2)
	}

	// Multi-criteria Telegram callback query on Turn 2 assistant message
	// Step 1: Criterion 0 (binary = 1)
	cb1, err := svc.HandleCallbackQuery(ctx, core.CallbackQuery{
		Channel:   "telegram",
		AccountID: "cc-tg-agent",
		ChatID:    "998877",
		MessageID: "600",
		SenderID:  "998877",
		Data:      "fb:" + reply2.MessageID + ":c0:1",
	})
	if err != nil {
		t.Fatalf("HandleCallbackQuery step 1 failed: %v", err)
	}
	if cb1 == nil || !strings.Contains(cb1.ToastText, "Đã lưu (1/2)") || len(cb1.InlineButtons) == 0 {
		t.Fatalf("unexpected cb1 result: %+v", cb1)
	}

	// Step 2: Criterion 1 (stars = 5)
	cb2, err := svc.HandleCallbackQuery(ctx, core.CallbackQuery{
		Channel:   "telegram",
		AccountID: "cc-tg-agent",
		ChatID:    "998877",
		MessageID: "600",
		SenderID:  "998877",
		Data:      "fb:" + reply2.MessageID + ":c1:5",
	})
	if err != nil {
		t.Fatalf("HandleCallbackQuery step 2 failed: %v", err)
	}
	if cb2 == nil || !strings.Contains(cb2.ToastText, "Cảm ơn bạn đã hoàn tất đánh giá!") {
		t.Fatalf("unexpected cb2 result: %+v", cb2)
	}

	// Verify both criteria were persisted in api_4_conversation_message
	convAfter, err := dao.NewAPI4ConversationDAO().GetBySessionID(ctx, db, sessionID, "agent-qltn")
	if err != nil || convAfter == nil {
		t.Fatalf("failed to reload conv: %v", err)
	}
	msgsAfter := parseMessages(convAfter.Message)
	foundFeedback := false
	for _, m := range msgsAfter {
		if stringValue(m["id"]) == reply2.MessageID && stringValue(m["role"]) == "assistant" {
			detail, _ := m["feedback_detail"].(map[string]interface{})
			crit, _ := detail["criteria"].(map[string]interface{})
			if crit["accuracy"] == true && crit["satisfaction"] == float64(5) {
				foundFeedback = true
			}
		}
	}
	if !foundFeedback {
		t.Fatalf("expected persisted multi-criteria feedback on message %s, got messages=%+v", reply2.MessageID, msgsAfter)
	}

	// DeleteAgent cleanup: deleting the bound agent must unlink chat_channel.agent_id = NULL
	agentSvc := NewAgentService()
	if err := agentSvc.DeleteAgent(ctx, "tenant-1", "agent-qltn"); err != nil {
		t.Fatalf("DeleteAgent failed: %v", err)
	}
	chAfter, err := dao.NewChatChannel().GetByID(ctx, db, "cc-tg-agent")
	if err != nil {
		t.Fatalf("GetByID channel after DeleteAgent failed: %v", err)
	}
	if chAfter.AgentID != nil {
		t.Fatalf("expected chat_channel.agent_id to be nil after DeleteAgent, got %v", chAfter.AgentID)
	}
}
