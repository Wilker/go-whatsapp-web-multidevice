package mcp

import (
	"context"
	"testing"

	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/mark3labs/mcp-go/mcp"
	"go.mau.fi/whatsmeow"
)

func TestHandleDownloadMediaAcceptsOutputDirAndReturnsResolvedPath(t *testing.T) {
	manager := whatsapp.InitializeDeviceManager(nil, nil, nil)
	manager.AddDevice(whatsapp.NewDeviceInstance("test-device", &whatsmeow.Client{}, nil))

	messageService := &stubMessageUsecase{
		downloadMediaResp: domainMessage.DownloadMediaResponse{
			MessageID:      "msg-123",
			Status:         "ok",
			MediaType:      "document",
			Filename:       "relatorio.pdf",
			FilePath:       "/Users/wilker/Downloads/whatsapp/5511999999999/2026-03-10/relatorio.pdf",
			FileSize:       1234,
			OutputDirUsed:  "/Users/wilker/Downloads/whatsapp",
			RecoveryMethod: domainMessage.MediaRecoveryMethodDirectURL,
		},
	}
	handler := &QueryHandler{messageService: messageService}

	result, err := handler.handleDownloadMedia(context.Background(), mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]any{
				"message_id": "msg-123",
				"phone":      "5511999999999",
				"output_dir": "~/Downloads/whatsapp",
			},
		},
	})
	if err != nil {
		t.Fatalf("handleDownloadMedia() unexpected error: %v", err)
	}
	if result.IsError {
		t.Fatal("expected successful tool result")
	}
	if messageService.lastDownloadRequest.OutputDir != "~/Downloads/whatsapp" {
		t.Fatalf("expected output_dir to be forwarded, got %q", messageService.lastDownloadRequest.OutputDir)
	}

	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content map, got %T", result.StructuredContent)
	}
	resultPayload, ok := structured["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result payload map, got %T", structured["result"])
	}
	media, ok := resultPayload["media"].(map[string]any)
	if !ok {
		t.Fatalf("expected media payload map, got %T", resultPayload["media"])
	}
	if media["output_dir_used"] != "/Users/wilker/Downloads/whatsapp" {
		t.Fatalf("expected output_dir_used in media payload, got %#v", media["output_dir_used"])
	}
}
