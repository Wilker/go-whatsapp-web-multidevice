package chatstorage

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	_ "github.com/mattn/go-sqlite3"
)

type fakeMessageScanner struct {
	values []any
}

func (f fakeMessageScanner) Scan(dest ...any) error {
	if len(dest) != len(f.values) {
		return fmt.Errorf("dest len %d != values len %d", len(dest), len(f.values))
	}

	for i, value := range f.values {
		switch target := dest[i].(type) {
		case *string:
			if value == nil {
				return fmt.Errorf("cannot scan nil into *string at index %d", i)
			}
			typed, ok := value.(string)
			if !ok {
				return fmt.Errorf("unexpected type %T for *string at index %d", value, i)
			}
			*target = typed
		case *sql.NullString:
			if value == nil {
				*target = sql.NullString{}
				continue
			}
			typed, ok := value.(string)
			if !ok {
				return fmt.Errorf("unexpected type %T for *sql.NullString at index %d", value, i)
			}
			*target = sql.NullString{String: typed, Valid: true}
		case *sql.NullTime:
			if value == nil {
				*target = sql.NullTime{}
				continue
			}
			typed, ok := value.(time.Time)
			if !ok {
				return fmt.Errorf("unexpected type %T for *sql.NullTime at index %d", value, i)
			}
			*target = sql.NullTime{Time: typed, Valid: true}
		case *time.Time:
			typed, ok := value.(time.Time)
			if !ok {
				return fmt.Errorf("unexpected type %T for *time.Time at index %d", value, i)
			}
			*target = typed
		case *bool:
			typed, ok := value.(bool)
			if !ok {
				return fmt.Errorf("unexpected type %T for *bool at index %d", value, i)
			}
			*target = typed
		case *[]byte:
			if value == nil {
				*target = nil
				continue
			}
			typed, ok := value.([]byte)
			if !ok {
				return fmt.Errorf("unexpected type %T for *[]byte at index %d", value, i)
			}
			*target = typed
		case *uint64:
			typed, ok := value.(uint64)
			if !ok {
				return fmt.Errorf("unexpected type %T for *uint64 at index %d", value, i)
			}
			*target = typed
		default:
			return fmt.Errorf("unsupported destination type %T at index %d", dest[i], i)
		}
	}

	return nil
}

func TestScanMessageAcceptsNullOptionalTextColumns(t *testing.T) {
	repo := &SQLiteRepository{}
	now := time.Now()

	message, err := repo.scanMessage(fakeMessageScanner{values: []any{
		"msg-1",                        // id
		"120363424157959439@g.us",      // chat_jid
		"5511999999999@s.whatsapp.net", // device_id
		"5511888888888@s.whatsapp.net", // sender
		nil,                            // content
		now,                            // timestamp
		false,                          // is_from_me
		nil,                            // media_type
		nil,                            // call_metadata
		nil,                            // filename
		nil,                            // url
		nil,                            // direct_path
		nil,                            // local_media_path
		nil,                            // reply_to_message_id
		nil,                            // quoted_text
		nil,                            // quoted_sender
		[]byte{1, 2, 3},                // media_key
		[]byte{4, 5, 6},                // file_sha256
		[]byte{7, 8, 9},                // file_enc_sha256
		uint64(42),                     // file_length
		nil,                            // referral_metadata
		nil,                            // deleted_at
		now,                            // created_at
		now,                            // updated_at
	}})
	if err != nil {
		t.Fatalf("scanMessage() unexpected error: %v", err)
	}

	if message.Content != "" || message.MediaType != "" || message.CallMetadata != "" || message.Filename != "" || message.URL != "" || message.DirectPath != "" ||
		message.LocalMediaPath != "" ||
		message.ReplyToMessageID != "" || message.QuotedText != "" || message.QuotedSender != "" || message.ReferralMetadata != "" {
		t.Fatalf("expected nullable text fields to be normalized to empty strings, got %+v", message)
	}
	if !message.DeletedAt.IsZero() {
		t.Fatalf("expected deleted_at to default to zero, got %+v", message)
	}
}

func TestGetMessageByIDByDeviceScopesLookup(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now()

	first := &domainChatStorage.Message{
		ID:         "shared-msg-id",
		ChatJID:    "120363424157959439@g.us",
		DeviceID:   "5511999999999@s.whatsapp.net",
		Sender:     "5511888888888@s.whatsapp.net",
		Content:    "device one",
		Timestamp:  now,
		IsFromMe:   false,
		MediaType:  "document",
		DirectPath: "/mms/document/device-1",
	}
	second := &domainChatStorage.Message{
		ID:         "shared-msg-id",
		ChatJID:    "120363424157959439@g.us",
		DeviceID:   "5521999999999@s.whatsapp.net",
		Sender:     "5521888888888@s.whatsapp.net",
		Content:    "device two",
		Timestamp:  now,
		IsFromMe:   false,
		MediaType:  "document",
		DirectPath: "/mms/document/device-2",
	}

	if err := repo.StoreMessage(first); err != nil {
		t.Fatalf("StoreMessage(first) unexpected error: %v", err)
	}
	if err := repo.StoreMessage(second); err != nil {
		t.Fatalf("StoreMessage(second) unexpected error: %v", err)
	}

	message, err := repo.GetMessageByIDByDevice(second.DeviceID, second.ID)
	if err != nil {
		t.Fatalf("GetMessageByIDByDevice() unexpected error: %v", err)
	}
	if message == nil {
		t.Fatal("expected scoped message, got nil")
	}
	if message.DeviceID != second.DeviceID || message.Content != second.Content || message.DirectPath != second.DirectPath {
		t.Fatalf("expected scoped lookup to return second device row, got %+v", message)
	}
}

func TestStoreMessagePreservesExistingDirectPathOnEmptyUpdate(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now()

	original := &domainChatStorage.Message{
		ID:               "msg-preserve-direct-path",
		ChatJID:          "120363424157959439@g.us",
		DeviceID:         "5511999999999@s.whatsapp.net",
		Sender:           "5511888888888@s.whatsapp.net",
		Timestamp:        now,
		MediaType:        "audio",
		DirectPath:       "/mms/audio/original",
		LocalMediaPath:   "statics/media/120363424157959439/2026-03-10/original.ogg",
		ReplyToMessageID: "quoted-1",
		QuotedText:       "Mensagem anterior",
		QuotedSender:     "5511777777777@s.whatsapp.net",
	}
	if err := repo.StoreMessage(original); err != nil {
		t.Fatalf("StoreMessage(original) unexpected error: %v", err)
	}

	update := &domainChatStorage.Message{
		ID:        original.ID,
		ChatJID:   original.ChatJID,
		DeviceID:  original.DeviceID,
		Sender:    original.Sender,
		Timestamp: original.Timestamp,
		MediaType: original.MediaType,
	}
	if err := repo.StoreMessage(update); err != nil {
		t.Fatalf("StoreMessage(update) unexpected error: %v", err)
	}

	stored, err := repo.GetMessageByIDByDevice(original.DeviceID, original.ID)
	if err != nil {
		t.Fatalf("GetMessageByIDByDevice() unexpected error: %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored message after update, got nil")
	}
	if stored.DirectPath != original.DirectPath {
		t.Fatalf("expected direct path %q to be preserved, got %q", original.DirectPath, stored.DirectPath)
	}
	if stored.LocalMediaPath != original.LocalMediaPath {
		t.Fatalf("expected local media path %q to be preserved, got %q", original.LocalMediaPath, stored.LocalMediaPath)
	}
	if stored.ReplyToMessageID != original.ReplyToMessageID || stored.QuotedText != original.QuotedText || stored.QuotedSender != original.QuotedSender {
		t.Fatalf("expected reply metadata to be preserved, got %+v", stored)
	}
}

func TestStoreMessagesBatchPreservesExistingDirectPathOnEmptyUpdate(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now()

	original := &domainChatStorage.Message{
		ID:               "msg-batch-preserve-direct-path",
		ChatJID:          "120363424157959439@g.us",
		DeviceID:         "5511999999999@s.whatsapp.net",
		Sender:           "5511888888888@s.whatsapp.net",
		Timestamp:        now,
		MediaType:        "document",
		DirectPath:       "/mms/document/original",
		LocalMediaPath:   "statics/media/120363424157959439/2026-03-10/original.pdf",
		ReplyToMessageID: "quoted-2",
		QuotedText:       "Contexto batch",
		QuotedSender:     "5511666666666@s.whatsapp.net",
	}
	if err := repo.StoreMessage(original); err != nil {
		t.Fatalf("StoreMessage(original) unexpected error: %v", err)
	}

	update := &domainChatStorage.Message{
		ID:        original.ID,
		ChatJID:   original.ChatJID,
		DeviceID:  original.DeviceID,
		Sender:    original.Sender,
		Timestamp: original.Timestamp,
		MediaType: original.MediaType,
	}
	if err := repo.StoreMessagesBatch([]*domainChatStorage.Message{update}); err != nil {
		t.Fatalf("StoreMessagesBatch(update) unexpected error: %v", err)
	}

	stored, err := repo.GetMessageByIDByDevice(original.DeviceID, original.ID)
	if err != nil {
		t.Fatalf("GetMessageByIDByDevice() unexpected error: %v", err)
	}
	if stored == nil {
		t.Fatal("expected stored message after batch update, got nil")
	}
	if stored.DirectPath != original.DirectPath {
		t.Fatalf("expected direct path %q to be preserved after batch update, got %q", original.DirectPath, stored.DirectPath)
	}
	if stored.LocalMediaPath != original.LocalMediaPath {
		t.Fatalf("expected local media path %q to be preserved after batch update, got %q", original.LocalMediaPath, stored.LocalMediaPath)
	}
	if stored.ReplyToMessageID != original.ReplyToMessageID || stored.QuotedText != original.QuotedText || stored.QuotedSender != original.QuotedSender {
		t.Fatalf("expected reply metadata to be preserved after batch update, got %+v", stored)
	}
}

func TestDeleteMessageByDeviceSoftDeletesMessage(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now()

	original := &domainChatStorage.Message{
		ID:        "msg-soft-delete",
		ChatJID:   "120363424157959439@g.us",
		DeviceID:  "5511999999999@s.whatsapp.net",
		Sender:    "5511888888888@s.whatsapp.net",
		Content:   "Mensagem que deve continuar armazenada",
		Timestamp: now,
	}
	if err := repo.StoreMessage(original); err != nil {
		t.Fatalf("StoreMessage(original) unexpected error: %v", err)
	}

	if err := repo.DeleteMessageByDevice(original.DeviceID, original.ID, original.ChatJID); err != nil {
		t.Fatalf("DeleteMessageByDevice() unexpected error: %v", err)
	}

	stored, err := repo.GetMessageByIDByDevice(original.DeviceID, original.ID)
	if err != nil {
		t.Fatalf("GetMessageByIDByDevice() unexpected error: %v", err)
	}
	if stored == nil {
		t.Fatal("expected soft-deleted message to remain in storage, got nil")
	}
	if stored.DeletedAt.IsZero() {
		t.Fatalf("expected deleted_at to be set, got %+v", stored)
	}
	if stored.Content != original.Content {
		t.Fatalf("expected original content to be preserved, got %q", stored.Content)
	}
}

func TestChatMediaPolicyIsDeviceScoped(t *testing.T) {
	repo := newTestSQLiteRepository(t)

	first := &domainChatStorage.ChatMediaPolicy{
		DeviceID:      "5511999999999@s.whatsapp.net",
		ChatJID:       "120363424157959439@g.us",
		Mode:          domainChatStorage.ChatMediaPolicyModeEphemeral,
		RetentionDays: 5,
	}
	second := &domainChatStorage.ChatMediaPolicy{
		DeviceID:      "5521999999999@s.whatsapp.net",
		ChatJID:       first.ChatJID,
		Mode:          domainChatStorage.ChatMediaPolicyModeEphemeral,
		RetentionDays: 5,
	}

	if err := repo.StoreChatMediaPolicy(first); err != nil {
		t.Fatalf("StoreChatMediaPolicy(first) unexpected error: %v", err)
	}
	if err := repo.StoreChatMediaPolicy(second); err != nil {
		t.Fatalf("StoreChatMediaPolicy(second) unexpected error: %v", err)
	}

	stored, err := repo.GetChatMediaPolicyByDevice(first.DeviceID, first.ChatJID)
	if err != nil {
		t.Fatalf("GetChatMediaPolicyByDevice() unexpected error: %v", err)
	}
	if stored == nil || stored.DeviceID != first.DeviceID {
		t.Fatalf("expected first device policy, got %+v", stored)
	}

	if err := repo.DeleteChatMediaPolicyByDevice(first.DeviceID, first.ChatJID); err != nil {
		t.Fatalf("DeleteChatMediaPolicyByDevice() unexpected error: %v", err)
	}

	deleted, err := repo.GetChatMediaPolicyByDevice(first.DeviceID, first.ChatJID)
	if err != nil {
		t.Fatalf("GetChatMediaPolicyByDevice(deleted) unexpected error: %v", err)
	}
	if deleted != nil {
		t.Fatalf("expected first device policy to be deleted, got %+v", deleted)
	}

	remaining, err := repo.GetChatMediaPolicyByDevice(second.DeviceID, second.ChatJID)
	if err != nil {
		t.Fatalf("GetChatMediaPolicyByDevice(remaining) unexpected error: %v", err)
	}
	if remaining == nil || remaining.DeviceID != second.DeviceID {
		t.Fatalf("expected second device policy to remain, got %+v", remaining)
	}
}

func TestListAndClearLocalMediaByDeviceChat(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now()
	deviceID := "5511999999999@s.whatsapp.net"
	chatJID := "120363424157959439@g.us"

	messages := []*domainChatStorage.Message{
		{
			ID:             "img-1",
			ChatJID:        chatJID,
			DeviceID:       deviceID,
			Sender:         "5511888888888@s.whatsapp.net",
			Timestamp:      now,
			MediaType:      "image",
			DirectPath:     "/mms/image/1",
			LocalMediaPath: "statics/media/chat/img.jpg",
		},
		{
			ID:             "aud-1",
			ChatJID:        chatJID,
			DeviceID:       deviceID,
			Sender:         "5511888888888@s.whatsapp.net",
			Timestamp:      now,
			MediaType:      "audio",
			DirectPath:     "/mms/audio/1",
			LocalMediaPath: "statics/media/chat/audio.ogg",
		},
		{
			ID:             "img-other-device",
			ChatJID:        chatJID,
			DeviceID:       "5521999999999@s.whatsapp.net",
			Sender:         "5521888888888@s.whatsapp.net",
			Timestamp:      now,
			MediaType:      "image",
			DirectPath:     "/mms/image/2",
			LocalMediaPath: "statics/media/chat/other.jpg",
		},
	}
	for _, message := range messages {
		if err := repo.StoreMessage(message); err != nil {
			t.Fatalf("StoreMessage(%s) unexpected error: %v", message.ID, err)
		}
	}

	records, err := repo.ListLocalMediaByDeviceChat(deviceID, chatJID, []string{"image", "video", "video_note"})
	if err != nil {
		t.Fatalf("ListLocalMediaByDeviceChat() unexpected error: %v", err)
	}
	if len(records) != 1 || records[0].MessageID != "img-1" {
		t.Fatalf("expected only image from target device/chat, got %+v", records)
	}

	if err := repo.ClearLocalMediaPathByDevice(deviceID, chatJID, "img-1", "statics/media/chat/img.jpg"); err != nil {
		t.Fatalf("ClearLocalMediaPathByDevice() unexpected error: %v", err)
	}

	stored, err := repo.GetMessageByIDByDevice(deviceID, "img-1")
	if err != nil {
		t.Fatalf("GetMessageByIDByDevice() unexpected error: %v", err)
	}
	if stored.LocalMediaPath != "" {
		t.Fatalf("expected local media path to be cleared, got %q", stored.LocalMediaPath)
	}
	if stored.DirectPath != "/mms/image/1" {
		t.Fatalf("expected direct path to be preserved, got %q", stored.DirectPath)
	}
}

func TestListLocalMediaForEphemeralPolicies(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now()
	deviceID := "5511999999999@s.whatsapp.net"
	ephemeralChat := "120363424157959439@g.us"
	permanentChat := "120363424157959440@g.us"

	if err := repo.StoreChatMediaPolicy(&domainChatStorage.ChatMediaPolicy{
		DeviceID:      deviceID,
		ChatJID:       ephemeralChat,
		Mode:          domainChatStorage.ChatMediaPolicyModeEphemeral,
		RetentionDays: 5,
	}); err != nil {
		t.Fatalf("StoreChatMediaPolicy() unexpected error: %v", err)
	}

	messages := []*domainChatStorage.Message{
		{
			ID:             "ephemeral-video",
			ChatJID:        ephemeralChat,
			DeviceID:       deviceID,
			Sender:         "5511888888888@s.whatsapp.net",
			Timestamp:      now,
			MediaType:      "video",
			LocalMediaPath: "statics/media/chat/video.mp4",
		},
		{
			ID:             "ephemeral-audio",
			ChatJID:        ephemeralChat,
			DeviceID:       deviceID,
			Sender:         "5511888888888@s.whatsapp.net",
			Timestamp:      now,
			MediaType:      "audio",
			LocalMediaPath: "statics/media/chat/audio.ogg",
		},
		{
			ID:             "permanent-image",
			ChatJID:        permanentChat,
			DeviceID:       deviceID,
			Sender:         "5511888888888@s.whatsapp.net",
			Timestamp:      now,
			MediaType:      "image",
			LocalMediaPath: "statics/media/chat/permanent.jpg",
		},
	}
	for _, message := range messages {
		if err := repo.StoreMessage(message); err != nil {
			t.Fatalf("StoreMessage(%s) unexpected error: %v", message.ID, err)
		}
	}

	records, err := repo.ListLocalMediaForEphemeralPolicies([]string{"image", "video", "video_note"})
	if err != nil {
		t.Fatalf("ListLocalMediaForEphemeralPolicies() unexpected error: %v", err)
	}
	if len(records) != 1 || records[0].MessageID != "ephemeral-video" || records[0].RetentionDays != 5 {
		t.Fatalf("expected only visual media from ephemeral chat, got %+v", records)
	}
}

func newTestSQLiteRepository(t *testing.T) *SQLiteRepository {
	t.Helper()

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	if err != nil {
		t.Fatalf("sql.Open() unexpected error: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = db.Close()
	})

	repo := &SQLiteRepository{db: db}
	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("InitializeSchema() unexpected error: %v", err)
	}

	return repo
}
