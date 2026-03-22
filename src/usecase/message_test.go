package usecase

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestHasUsableStoredMediaURL(t *testing.T) {
	tests := []struct {
		name    string
		message *domainChatStorage.Message
		want    bool
	}{
		{
			name: "usable mmg url",
			message: &domainChatStorage.Message{
				URL: "https://mmg.whatsapp.net/media",
			},
			want: true,
		},
		{
			name: "web whatsapp url is not usable",
			message: &domainChatStorage.Message{
				URL: "https://web.whatsapp.net/temporary",
			},
			want: false,
		},
		{
			name:    "empty url",
			message: &domainChatStorage.Message{},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasUsableStoredMediaURL(tt.message); got != tt.want {
				t.Fatalf("hasUsableStoredMediaURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildStoredDownloadableMessageUsesDirectPath(t *testing.T) {
	message := &domainChatStorage.Message{
		MediaType:     "document",
		Filename:      "relatorio.pdf",
		MediaKey:      []byte{1, 2, 3},
		FileSHA256:    []byte{4, 5, 6},
		FileEncSHA256: []byte{7, 8, 9},
		FileLength:    42,
	}

	downloadable, err := buildStoredDownloadableMessage(message, "", "/mms/document/123")
	if err != nil {
		t.Fatalf("buildStoredDownloadableMessage() unexpected error: %v", err)
	}

	doc, ok := downloadable.(*waE2E.DocumentMessage)
	if !ok {
		t.Fatalf("expected DocumentMessage, got %T", downloadable)
	}
	if doc.GetDirectPath() != "/mms/document/123" {
		t.Fatalf("expected direct path to be preserved, got %q", doc.GetDirectPath())
	}
	if doc.GetFileName() != "relatorio.pdf" {
		t.Fatalf("expected original filename to be preserved, got %q", doc.GetFileName())
	}
}

func TestIsRetryableMediaDownloadError(t *testing.T) {
	retryable := []error{
		whatsmeow.ErrMediaDownloadFailedWith403,
		whatsmeow.ErrMediaDownloadFailedWith404,
		whatsmeow.ErrMediaDownloadFailedWith410,
		whatsmeow.ErrNoURLPresent,
	}

	for _, err := range retryable {
		if !isRetryableMediaDownloadError(err) {
			t.Fatalf("expected %v to be retryable", err)
		}
	}

	if isRetryableMediaDownloadError(whatsmeow.ErrInvalidMediaSHA256) {
		t.Fatal("expected ErrInvalidMediaSHA256 to be non-retryable")
	}
}

func TestDownloadMessageMediaViaRetryTimesOutAfterConfiguredAttempts(t *testing.T) {
	originalSend := sendMediaRetryReceiptFunc
	originalDecrypt := decryptMediaRetryNotificationFunc
	originalDownload := downloadStoredMessageMediaFunc
	t.Cleanup(func() {
		sendMediaRetryReceiptFunc = originalSend
		decryptMediaRetryNotificationFunc = originalDecrypt
		downloadStoredMessageMediaFunc = originalDownload
	})

	sendCalls := 0
	sendMediaRetryReceiptFunc = func(_ context.Context, _ *whatsmeow.Client, _ *types.MessageInfo, _ []byte) error {
		sendCalls++
		return nil
	}
	decryptMediaRetryNotificationFunc = originalDecrypt
	downloadStoredMessageMediaFunc = originalDownload

	instance := whatsapp.NewDeviceInstance("dev", &whatsmeow.Client{}, nil)
	ctx := whatsapp.ContextWithDevice(context.Background(), instance)
	service := serviceMessage{}
	message := &domainChatStorage.Message{
		ID:            "msg-timeout",
		ChatJID:       "5511999999999@s.whatsapp.net",
		Sender:        "5511888888888@s.whatsapp.net",
		MediaType:     "audio",
		MediaKey:      []byte{1, 2, 3},
		FileSHA256:    []byte{4, 5, 6},
		FileEncSHA256: []byte{7, 8, 9},
		FileLength:    10,
	}

	result, err := service.downloadMessageMediaViaRetry(
		ctx,
		&whatsmeow.Client{},
		message,
		t.TempDir(),
		mediaDownloadProfile{
			allowMediaRetry: true,
			retryAttempts:   2,
			retryTimeout:    5 * time.Millisecond,
		},
	)
	if err == nil {
		t.Fatal("expected retry timeout error")
	}
	if result.failureReason != domainMessage.MediaFailureReasonRetryTimeout {
		t.Fatalf("expected retry timeout failure reason, got %q", result.failureReason)
	}
	if sendCalls != 2 {
		t.Fatalf("expected 2 retry receipts, got %d", sendCalls)
	}
}

func TestDownloadMessageMediaViaRetryStopsWhenMediaNotAvailableOnPhone(t *testing.T) {
	originalSend := sendMediaRetryReceiptFunc
	originalDecrypt := decryptMediaRetryNotificationFunc
	originalDownload := downloadStoredMessageMediaFunc
	t.Cleanup(func() {
		sendMediaRetryReceiptFunc = originalSend
		decryptMediaRetryNotificationFunc = originalDecrypt
		downloadStoredMessageMediaFunc = originalDownload
	})

	instance := whatsapp.NewDeviceInstance("dev", &whatsmeow.Client{}, nil)
	sendCalls := 0
	sendMediaRetryReceiptFunc = func(_ context.Context, _ *whatsmeow.Client, messageInfo *types.MessageInfo, _ []byte) error {
		sendCalls++
		go instance.DeliverPendingMediaRetry(&events.MediaRetry{MessageID: messageInfo.ID})
		return nil
	}
	decryptMediaRetryNotificationFunc = func(_ *events.MediaRetry, _ []byte) (*waMmsRetry.MediaRetryNotification, error) {
		return nil, whatsmeow.ErrMediaNotAvailableOnPhone
	}
	downloadStoredMessageMediaFunc = originalDownload

	ctx := whatsapp.ContextWithDevice(context.Background(), instance)
	service := serviceMessage{}
	message := &domainChatStorage.Message{
		ID:            "msg-not-found",
		ChatJID:       "5511999999999@s.whatsapp.net",
		Sender:        "5511888888888@s.whatsapp.net",
		MediaType:     "image",
		MediaKey:      []byte{1, 2, 3},
		FileSHA256:    []byte{4, 5, 6},
		FileEncSHA256: []byte{7, 8, 9},
		FileLength:    10,
	}

	result, err := service.downloadMessageMediaViaRetry(
		ctx,
		&whatsmeow.Client{},
		message,
		t.TempDir(),
		mediaDownloadProfile{
			allowMediaRetry: true,
			retryAttempts:   2,
			retryTimeout:    25 * time.Millisecond,
		},
	)
	if err == nil {
		t.Fatal("expected media-not-available error")
	}
	if result.failureReason != domainMessage.MediaFailureReasonNotAvailableOnPhone {
		t.Fatalf("expected not_available_on_phone failure reason, got %q", result.failureReason)
	}
	if sendCalls != 1 {
		t.Fatalf("expected retry flow to stop after first not-available response, got %d sends", sendCalls)
	}
}

func TestResolveMediaDownloadDirUsesDefaultPathMedia(t *testing.T) {
	message := &domainChatStorage.Message{
		ChatJID:   "120363424157959439@g.us",
		Timestamp: time.Date(2026, 3, 10, 11, 14, 0, 0, time.UTC),
	}

	baseDir, dateDir, err := resolveMediaDownloadDir("", message)
	if err != nil {
		t.Fatalf("resolveMediaDownloadDir() unexpected error: %v", err)
	}

	if baseDir != filepath.Clean("statics/media") {
		t.Fatalf("expected default media base dir, got %q", baseDir)
	}
	expectedDateDir := filepath.Join("statics/media", "120363424157959439", "2026-03-10")
	if dateDir != expectedDateDir {
		t.Fatalf("expected date dir %q, got %q", expectedDateDir, dateDir)
	}
}

func TestResolveMediaDownloadDirExpandsCustomOutputDir(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	message := &domainChatStorage.Message{
		ChatJID:   "5511999999999@s.whatsapp.net",
		Timestamp: time.Date(2026, 3, 10, 11, 14, 0, 0, time.UTC),
	}

	baseDir, dateDir, err := resolveMediaDownloadDir("~/Downloads/whatsapp", message)
	if err != nil {
		t.Fatalf("resolveMediaDownloadDir() unexpected error: %v", err)
	}

	expectedBaseDir := filepath.Join(homeDir, "Downloads", "whatsapp")
	if baseDir != expectedBaseDir {
		t.Fatalf("expected expanded base dir %q, got %q", expectedBaseDir, baseDir)
	}
	expectedDateDir := filepath.Join(expectedBaseDir, "5511999999999", "2026-03-10")
	if dateDir != expectedDateDir {
		t.Fatalf("expected date dir %q, got %q", expectedDateDir, dateDir)
	}
}
