package usecase

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	_ "github.com/mattn/go-sqlite3"
)

func TestChatMediaPolicyLifecycle(t *testing.T) {
	repo := newChatMediaTestRepository(t)
	service := NewChatService(repo)
	ctx := chatMediaTestContext("5511999999999@s.whatsapp.net")
	chatJID := "120363424157959439@g.us"

	defaultPolicy, err := service.GetChatMediaPolicy(ctx, domainChat.GetChatMediaPolicyRequest{ChatJID: chatJID})
	if err != nil {
		t.Fatalf("GetChatMediaPolicy(default) unexpected error: %v", err)
	}
	if !defaultPolicy.IsDefault || defaultPolicy.Mode != domainChat.MediaPolicyModePermanent || defaultPolicy.Ephemeral {
		t.Fatalf("expected default permanent policy, got %+v", defaultPolicy)
	}

	ephemeral, err := service.SetChatMediaPolicy(ctx, domainChat.SetChatMediaPolicyRequest{ChatJID: chatJID})
	if err != nil {
		t.Fatalf("SetChatMediaPolicy() unexpected error: %v", err)
	}
	if ephemeral.IsDefault || !ephemeral.Ephemeral || ephemeral.RetentionDays != 5 {
		t.Fatalf("expected ephemeral policy, got %+v", ephemeral)
	}

	reset, err := service.ResetChatMediaPolicy(ctx, domainChat.ResetChatMediaPolicyRequest{ChatJID: chatJID})
	if err != nil {
		t.Fatalf("ResetChatMediaPolicy() unexpected error: %v", err)
	}
	if !reset.IsDefault || reset.Mode != domainChat.MediaPolicyModePermanent {
		t.Fatalf("expected reset to default policy, got %+v", reset)
	}
}

func TestDeleteChatLocalMediaDryRunAndExecutePreservesAudio(t *testing.T) {
	repo := newChatMediaTestRepository(t)
	service := NewChatService(repo)
	deviceID := "5511999999999@s.whatsapp.net"
	chatJID := "120363424157959439@g.us"
	ctx := chatMediaTestContext(deviceID)
	now := time.Now()

	imagePath := writeChatMediaTestFile(t, "image.jpg", "image-bytes")
	videoPath := writeChatMediaTestFile(t, "video.mp4", "video-bytes")
	audioPath := writeChatMediaTestFile(t, "audio.ogg", "audio-bytes")

	for _, message := range []*domainChatStorage.Message{
		{
			ID:             "img-1",
			ChatJID:        chatJID,
			DeviceID:       deviceID,
			Sender:         "5511888888888@s.whatsapp.net",
			Timestamp:      now,
			MediaType:      "image",
			DirectPath:     "/mms/image/1",
			LocalMediaPath: imagePath,
		},
		{
			ID:             "vid-1",
			ChatJID:        chatJID,
			DeviceID:       deviceID,
			Sender:         "5511888888888@s.whatsapp.net",
			Timestamp:      now,
			MediaType:      "video",
			DirectPath:     "/mms/video/1",
			LocalMediaPath: videoPath,
		},
		{
			ID:             "aud-1",
			ChatJID:        chatJID,
			DeviceID:       deviceID,
			Sender:         "5511888888888@s.whatsapp.net",
			Timestamp:      now,
			MediaType:      "audio",
			DirectPath:     "/mms/audio/1",
			LocalMediaPath: audioPath,
		},
	} {
		if err := repo.StoreMessage(message); err != nil {
			t.Fatalf("StoreMessage(%s) unexpected error: %v", message.ID, err)
		}
	}

	dryRun, err := service.DeleteChatLocalMedia(ctx, domainChat.DeleteChatLocalMediaRequest{ChatJID: chatJID, DryRun: true})
	if err != nil {
		t.Fatalf("DeleteChatLocalMedia(dry-run) unexpected error: %v", err)
	}
	if dryRun.FilesFound != 2 || dryRun.FilesDeleted != 0 || dryRun.PathsCleared != 0 {
		t.Fatalf("unexpected dry-run response: %+v", dryRun)
	}
	if _, err := os.Stat(imagePath); err != nil {
		t.Fatalf("expected image to remain after dry-run: %v", err)
	}

	executed, err := service.DeleteChatLocalMedia(ctx, domainChat.DeleteChatLocalMediaRequest{ChatJID: chatJID, DryRun: false})
	if err != nil {
		t.Fatalf("DeleteChatLocalMedia(execute) unexpected error: %v", err)
	}
	if executed.FilesDeleted != 2 || executed.PathsCleared != 2 || len(executed.Errors) != 0 {
		t.Fatalf("unexpected execute response: %+v", executed)
	}
	if _, err := os.Stat(imagePath); !os.IsNotExist(err) {
		t.Fatalf("expected image file to be deleted, stat err=%v", err)
	}
	if _, err := os.Stat(videoPath); !os.IsNotExist(err) {
		t.Fatalf("expected video file to be deleted, stat err=%v", err)
	}
	if _, err := os.Stat(audioPath); err != nil {
		t.Fatalf("expected audio to be preserved, got %v", err)
	}

	imageMessage, err := repo.GetMessageByIDByDevice(deviceID, "img-1")
	if err != nil {
		t.Fatalf("GetMessageByIDByDevice(image) unexpected error: %v", err)
	}
	if imageMessage.LocalMediaPath != "" || imageMessage.DirectPath == "" {
		t.Fatalf("expected local path cleared and direct path preserved, got %+v", imageMessage)
	}

	audioMessage, err := repo.GetMessageByIDByDevice(deviceID, "aud-1")
	if err != nil {
		t.Fatalf("GetMessageByIDByDevice(audio) unexpected error: %v", err)
	}
	if audioMessage.LocalMediaPath != audioPath {
		t.Fatalf("expected audio local path to be preserved, got %q", audioMessage.LocalMediaPath)
	}
}

func TestCleanupExpiredLocalMediaRemovesOnlyOldVisualMediaForEphemeralChats(t *testing.T) {
	repo := newChatMediaTestRepository(t)
	service := NewChatService(repo)
	deviceID := "5511999999999@s.whatsapp.net"
	chatJID := "120363424157959439@g.us"
	now := time.Date(2026, time.June, 10, 12, 0, 0, 0, time.UTC)
	originalNow := localMediaNow
	localMediaNow = func() time.Time { return now }
	t.Cleanup(func() { localMediaNow = originalNow })

	if err := repo.StoreChatMediaPolicy(&domainChatStorage.ChatMediaPolicy{
		DeviceID:      deviceID,
		ChatJID:       chatJID,
		Mode:          domainChatStorage.ChatMediaPolicyModeEphemeral,
		RetentionDays: 5,
	}); err != nil {
		t.Fatalf("StoreChatMediaPolicy() unexpected error: %v", err)
	}

	oldImagePath := writeChatMediaTestFile(t, "old-image.jpg", "old-image")
	recentVideoPath := writeChatMediaTestFile(t, "recent-video.mp4", "recent-video")
	oldAudioPath := writeChatMediaTestFile(t, "old-audio.ogg", "old-audio")

	for _, message := range []*domainChatStorage.Message{
		{
			ID:             "old-img",
			ChatJID:        chatJID,
			DeviceID:       deviceID,
			Sender:         "5511888888888@s.whatsapp.net",
			Timestamp:      now.AddDate(0, 0, -6),
			MediaType:      "image",
			LocalMediaPath: oldImagePath,
		},
		{
			ID:             "recent-video",
			ChatJID:        chatJID,
			DeviceID:       deviceID,
			Sender:         "5511888888888@s.whatsapp.net",
			Timestamp:      now.AddDate(0, 0, -4),
			MediaType:      "video",
			LocalMediaPath: recentVideoPath,
		},
		{
			ID:             "old-audio",
			ChatJID:        chatJID,
			DeviceID:       deviceID,
			Sender:         "5511888888888@s.whatsapp.net",
			Timestamp:      now.AddDate(0, 0, -10),
			MediaType:      "audio",
			LocalMediaPath: oldAudioPath,
		},
	} {
		if err := repo.StoreMessage(message); err != nil {
			t.Fatalf("StoreMessage(%s) unexpected error: %v", message.ID, err)
		}
	}

	cleanup, err := service.CleanupExpiredLocalMedia(context.Background())
	if err != nil {
		t.Fatalf("CleanupExpiredLocalMedia() unexpected error: %v", err)
	}
	if cleanup.FilesDeleted != 1 || cleanup.PathsCleared != 1 || cleanup.MatchedMessages != 1 {
		t.Fatalf("expected only old image to be cleaned, got %+v", cleanup)
	}
	if _, err := os.Stat(oldImagePath); !os.IsNotExist(err) {
		t.Fatalf("expected old image to be deleted, stat err=%v", err)
	}
	if _, err := os.Stat(recentVideoPath); err != nil {
		t.Fatalf("expected recent video to be preserved, got %v", err)
	}
	if _, err := os.Stat(oldAudioPath); err != nil {
		t.Fatalf("expected old audio to be preserved, got %v", err)
	}
}

func newChatMediaTestRepository(t *testing.T) domainChatStorage.IChatStorageRepository {
	t.Helper()

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	if err != nil {
		t.Fatalf("sql.Open() unexpected error: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = db.Close()
	})

	repo := chatstorage.NewStorageRepository(db)
	if err := repo.InitializeSchema(); err != nil {
		t.Fatalf("InitializeSchema() unexpected error: %v", err)
	}
	return repo
}

func chatMediaTestContext(deviceID string) context.Context {
	instance := whatsapp.NewDeviceInstance(deviceID, nil, nil)
	return whatsapp.ContextWithDevice(context.Background(), instance)
}

func writeChatMediaTestFile(t *testing.T, name string, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}
	return path
}
