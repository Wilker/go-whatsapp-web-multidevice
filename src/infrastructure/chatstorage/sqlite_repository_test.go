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
		"msg-1",
		"120363424157959439@g.us",
		"5511999999999@s.whatsapp.net",
		"5511888888888@s.whatsapp.net",
		nil,
		now,
		false,
		nil,
		nil,
		nil,
		nil,
		[]byte{1, 2, 3},
		[]byte{4, 5, 6},
		[]byte{7, 8, 9},
		uint64(42),
		now,
		now,
	}})
	if err != nil {
		t.Fatalf("scanMessage() unexpected error: %v", err)
	}

	if message.Content != "" || message.MediaType != "" || message.Filename != "" || message.URL != "" || message.DirectPath != "" {
		t.Fatalf("expected nullable text fields to be normalized to empty strings, got %+v", message)
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
		ID:         "msg-preserve-direct-path",
		ChatJID:    "120363424157959439@g.us",
		DeviceID:   "5511999999999@s.whatsapp.net",
		Sender:     "5511888888888@s.whatsapp.net",
		Timestamp:  now,
		MediaType:  "audio",
		DirectPath: "/mms/audio/original",
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
}

func TestStoreMessagesBatchPreservesExistingDirectPathOnEmptyUpdate(t *testing.T) {
	repo := newTestSQLiteRepository(t)
	now := time.Now()

	original := &domainChatStorage.Message{
		ID:         "msg-batch-preserve-direct-path",
		ChatJID:    "120363424157959439@g.us",
		DeviceID:   "5511999999999@s.whatsapp.net",
		Sender:     "5511888888888@s.whatsapp.net",
		Timestamp:  now,
		MediaType:  "document",
		DirectPath: "/mms/document/original",
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
