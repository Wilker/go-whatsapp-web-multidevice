package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
)

type stubMessageUsecase struct {
	downloadResults map[string][]stubDownloadResult
	downloadCalls   map[string]int
	batchResponse   domainMessage.RecoverMediaBatchResponse
	batchErr        error
}

type stubDownloadResult struct {
	response domainMessage.DownloadMediaResponse
	err      error
}

func (s *stubMessageUsecase) MarkAsRead(context.Context, domainMessage.MarkAsReadRequest) (domainMessage.GenericResponse, error) {
	return domainMessage.GenericResponse{}, nil
}

func (s *stubMessageUsecase) ReactMessage(context.Context, domainMessage.ReactionRequest) (domainMessage.GenericResponse, error) {
	return domainMessage.GenericResponse{}, nil
}

func (s *stubMessageUsecase) RevokeMessage(context.Context, domainMessage.RevokeRequest) (domainMessage.GenericResponse, error) {
	return domainMessage.GenericResponse{}, nil
}

func (s *stubMessageUsecase) UpdateMessage(context.Context, domainMessage.UpdateMessageRequest) (domainMessage.GenericResponse, error) {
	return domainMessage.GenericResponse{}, nil
}

func (s *stubMessageUsecase) DeleteMessage(context.Context, domainMessage.DeleteRequest) error {
	return nil
}

func (s *stubMessageUsecase) StarMessage(context.Context, domainMessage.StarRequest) error {
	return nil
}

func (s *stubMessageUsecase) DownloadMedia(context.Context, domainMessage.DownloadMediaRequest) (domainMessage.DownloadMediaResponse, error) {
	return domainMessage.DownloadMediaResponse{}, errors.New("DownloadMedia should not be used in export tests")
}

func (s *stubMessageUsecase) DownloadMediaForExport(_ context.Context, request domainMessage.DownloadMediaRequest) (domainMessage.DownloadMediaResponse, error) {
	if s.downloadCalls == nil {
		s.downloadCalls = map[string]int{}
	}
	s.downloadCalls[request.MessageID]++

	queue := s.downloadResults[request.MessageID]
	if len(queue) == 0 {
		return domainMessage.DownloadMediaResponse{}, errors.New("unexpected DownloadMediaForExport call")
	}
	next := queue[0]
	s.downloadResults[request.MessageID] = queue[1:]
	return next.response, next.err
}

func (s *stubMessageUsecase) RecoverMediaBatch(context.Context, domainMessage.RecoverMediaBatchRequest) (domainMessage.RecoverMediaBatchResponse, error) {
	return s.batchResponse, s.batchErr
}

func TestResolveMediaArchiveFilenameUsesStoredFilenameWhenAvailable(t *testing.T) {
	used := map[string]struct{}{}
	msg := domainChat.MessageInfo{
		ID:        "ACB469979A7D1B23649C158AA70E3636",
		MediaType: "audio",
		Filename:  "audio_20260309_192823.ogg",
	}

	archiveFilename, originalFilename, filenameSource := resolveMediaArchiveFilename(msg, used)

	if archiveFilename != "audio_20260309_192823.ogg" {
		t.Fatalf("expected stored filename to be preserved, got %q", archiveFilename)
	}
	if originalFilename != "audio_20260309_192823.ogg" {
		t.Fatalf("expected original filename to be preserved, got %q", originalFilename)
	}
	if filenameSource != "stored_filename" {
		t.Fatalf("expected filename source 'stored_filename', got %q", filenameSource)
	}
}

func TestResolveMediaArchiveFilenameAddsSuffixOnCollision(t *testing.T) {
	used := map[string]struct{}{}
	first := domainChat.MessageInfo{
		ID:        "ACB469979A7D1B23649C158AA70E3636",
		MediaType: "audio",
		Filename:  "audio_20260309_192823.ogg",
	}
	second := domainChat.MessageInfo{
		ID:        "ACC72D8271446896408FAFEA626F110D",
		MediaType: "audio",
		Filename:  "audio_20260309_192823.ogg",
	}

	firstName, _, firstSource := resolveMediaArchiveFilename(first, used)
	secondName, _, secondSource := resolveMediaArchiveFilename(second, used)

	if firstName != "audio_20260309_192823.ogg" {
		t.Fatalf("unexpected first archive filename %q", firstName)
	}
	if firstSource != "stored_filename" {
		t.Fatalf("unexpected first filename source %q", firstSource)
	}
	if secondName != "audio_20260309_192823__acc72d82.ogg" {
		t.Fatalf("expected collision suffix based on message id, got %q", secondName)
	}
	if secondSource != "stored_filename_with_collision_suffix" {
		t.Fatalf("unexpected second filename source %q", secondSource)
	}
}

func TestResolveMediaArchiveFilenameFallsBackToMessageID(t *testing.T) {
	used := map[string]struct{}{}
	msg := domainChat.MessageInfo{
		ID:        "ACDF1443B84982D37354C624C4C1A9DE",
		MediaType: "audio",
		Filename:  "",
	}

	archiveFilename, originalFilename, filenameSource := resolveMediaArchiveFilename(msg, used)

	if archiveFilename != "msg_acdf1443.ogg" {
		t.Fatalf("expected message id fallback filename, got %q", archiveFilename)
	}
	if originalFilename != "" {
		t.Fatalf("expected empty original filename, got %q", originalFilename)
	}
	if filenameSource != "message_id_fallback" {
		t.Fatalf("expected filename source 'message_id_fallback', got %q", filenameSource)
	}
}

func TestGenerateLocalChatExportRecoversMediaAfterBatchRetry(t *testing.T) {
	sourceDir := t.TempDir()
	sourcePath := filepath.Join(sourceDir, "audio.ogg")
	if err := os.WriteFile(sourcePath, []byte("audio-data"), 0o600); err != nil {
		t.Fatalf("WriteFile() unexpected error: %v", err)
	}

	msgID := "ACB469979A7D1B23649C158AA70E3636"
	messageService := &stubMessageUsecase{
		downloadResults: map[string][]stubDownloadResult{
			msgID: {
				{
					response: domainMessage.DownloadMediaResponse{
						MessageID:     msgID,
						MediaType:     "audio",
						Filename:      "audio.ogg",
						FailureReason: domainMessage.MediaFailureReasonDownloadFailed,
					},
					err: errors.New("media download failed"),
				},
				{
					response: domainMessage.DownloadMediaResponse{
						MessageID:      msgID,
						MediaType:      "audio",
						Filename:       "audio.ogg",
						FilePath:       sourcePath,
						RecoveryMethod: domainMessage.MediaRecoveryMethodStoredDirectPath,
					},
				},
			},
		},
		batchResponse: domainMessage.RecoverMediaBatchResponse{
			Items: []domainMessage.RecoverMediaBatchItem{
				{
					MessageID:         msgID,
					RecoveryMethod:    domainMessage.MediaRecoveryMethodMediaRetry,
					UpdatedDirectPath: "/mms/audio/refreshed",
				},
			},
		},
	}

	handler := &QueryHandler{messageService: messageService}
	resultPayload, _, err := handler.generateLocalChatExport(context.Background(), chatExportOptions{
		ChatJID:      "120363424157959439@g.us",
		ExportType:   exportTypeFull,
		IncludeMedia: true,
		OutputDir:    t.TempDir(),
	}, chatExportCollected{
		ChatInfo: domainChat.ChatInfo{
			JID:  "120363424157959439@g.us",
			Name: "Diretoria - Gesso Casa Branca",
		},
		Messages: []domainChat.MessageInfo{
			{
				ID:         msgID,
				ChatJID:    "120363424157959439@g.us",
				SenderJID:  "5511888888888@s.whatsapp.net",
				Content:    "Audio enviado",
				Timestamp:  "2026-03-09T11:13:00-03:00",
				MediaType:  "audio",
				Filename:   "audio.ogg",
				FileLength: 10,
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("generateLocalChatExport() unexpected error: %v", err)
	}

	stats := resultPayload["stats"].(map[string]any)
	if stats["media_files_included_in_archive"] != 1 {
		t.Fatalf("expected 1 included media file, got %#v", stats["media_files_included_in_archive"])
	}
	if stats["media_files_recovered_via_retry"] != 1 {
		t.Fatalf("expected 1 media recovered via retry, got %#v", stats["media_files_recovered_via_retry"])
	}
	if stats["media_files_recovered_after_batch_retry"] != 1 {
		t.Fatalf("expected 1 media recovered after batch retry, got %#v", stats["media_files_recovered_after_batch_retry"])
	}
	if messageService.downloadCalls[msgID] != 2 {
		t.Fatalf("expected 2 export download attempts, got %d", messageService.downloadCalls[msgID])
	}

	files := resultPayload["files"].(map[string]any)
	llmJSONPath := files["llm_json"].(string)
	rawJSON, err := os.ReadFile(llmJSONPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) unexpected error: %v", llmJSONPath, err)
	}

	var payload map[string]any
	if err := json.Unmarshal(rawJSON, &payload); err != nil {
		t.Fatalf("json.Unmarshal() unexpected error: %v", err)
	}

	messages := payload["messages"].([]any)
	media := messages[0].(map[string]any)["media"].(map[string]any)
	if media["download_status"] != "included_after_batch_recovery" {
		t.Fatalf("expected included_after_batch_recovery, got %#v", media["download_status"])
	}
	if media["recovery_method"] != domainMessage.MediaRecoveryMethodMediaRetry {
		t.Fatalf("expected recovery_method media_retry, got %#v", media["recovery_method"])
	}
}

func TestGenerateLocalChatExportKeepsUnavailableMediaAsFailed(t *testing.T) {
	msgID := "ACC72D8271446896408FAFEA626F110D"
	messageService := &stubMessageUsecase{
		downloadResults: map[string][]stubDownloadResult{
			msgID: {
				{
					response: domainMessage.DownloadMediaResponse{
						MessageID:     msgID,
						MediaType:     "document",
						Filename:      "arquivo.pdf",
						FailureReason: domainMessage.MediaFailureReasonDownloadFailed,
					},
					err: errors.New("media download failed"),
				},
			},
		},
		batchResponse: domainMessage.RecoverMediaBatchResponse{
			Items: []domainMessage.RecoverMediaBatchItem{
				{
					MessageID:      msgID,
					FailureReason:  domainMessage.MediaFailureReasonNotAvailableOnPhone,
					RecoveryMethod: domainMessage.MediaRecoveryMethodNone,
				},
			},
		},
	}

	handler := &QueryHandler{messageService: messageService}
	resultPayload, _, err := handler.generateLocalChatExport(context.Background(), chatExportOptions{
		ChatJID:      "120363424157959439@g.us",
		ExportType:   exportTypeFull,
		IncludeMedia: true,
		OutputDir:    t.TempDir(),
	}, chatExportCollected{
		ChatInfo: domainChat.ChatInfo{
			JID:  "120363424157959439@g.us",
			Name: "Diretoria - Gesso Casa Branca",
		},
		Messages: []domainChat.MessageInfo{
			{
				ID:         msgID,
				ChatJID:    "120363424157959439@g.us",
				SenderJID:  "5511888888888@s.whatsapp.net",
				Content:    "Documento enviado",
				Timestamp:  "2026-03-09T11:20:00-03:00",
				MediaType:  "document",
				Filename:   "arquivo.pdf",
				FileLength: 10,
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("generateLocalChatExport() unexpected error: %v", err)
	}

	stats := resultPayload["stats"].(map[string]any)
	if stats["media_files_failed"] != 1 {
		t.Fatalf("expected 1 failed media file, got %#v", stats["media_files_failed"])
	}
	if stats["media_files_unavailable_on_phone"] != 1 {
		t.Fatalf("expected 1 unavailable media file, got %#v", stats["media_files_unavailable_on_phone"])
	}
	if messageService.downloadCalls[msgID] != 1 {
		t.Fatalf("expected export to skip second download pass, got %d calls", messageService.downloadCalls[msgID])
	}
}
