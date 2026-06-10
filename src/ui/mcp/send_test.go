package mcp

import (
	"context"
	"testing"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/mark3labs/mcp-go/mcp"
	"go.mau.fi/whatsmeow"
)

type stubSendUsecase struct {
	sendTextErr      error
	lastTextRequest  domainSend.MessageRequest
	lastFileRequest  domainSend.FileRequest
	lastVideoRequest domainSend.VideoRequest
	lastAudioRequest domainSend.AudioRequest
}

func (s *stubSendUsecase) SendText(_ context.Context, request domainSend.MessageRequest) (domainSend.GenericResponse, error) {
	s.lastTextRequest = request
	if s.sendTextErr != nil {
		return domainSend.GenericResponse{}, s.sendTextErr
	}
	return domainSend.GenericResponse{MessageID: "text-msg", Status: "ok"}, nil
}

func (s *stubSendUsecase) SendImage(context.Context, domainSend.ImageRequest) (domainSend.GenericResponse, error) {
	return domainSend.GenericResponse{}, nil
}

func (s *stubSendUsecase) SendFile(_ context.Context, request domainSend.FileRequest) (domainSend.GenericResponse, error) {
	s.lastFileRequest = request
	return domainSend.GenericResponse{MessageID: "file-msg", Status: "ok"}, nil
}

func (s *stubSendUsecase) SendVideo(_ context.Context, request domainSend.VideoRequest) (domainSend.GenericResponse, error) {
	s.lastVideoRequest = request
	return domainSend.GenericResponse{MessageID: "video-msg", Status: "ok"}, nil
}

func (s *stubSendUsecase) SendAudio(_ context.Context, request domainSend.AudioRequest) (domainSend.GenericResponse, error) {
	s.lastAudioRequest = request
	return domainSend.GenericResponse{MessageID: "audio-msg", Status: "ok"}, nil
}

func (s *stubSendUsecase) SendSticker(context.Context, domainSend.StickerRequest) (domainSend.GenericResponse, error) {
	return domainSend.GenericResponse{}, nil
}

func (s *stubSendUsecase) SendContact(context.Context, domainSend.ContactRequest) (domainSend.GenericResponse, error) {
	return domainSend.GenericResponse{}, nil
}

func (s *stubSendUsecase) SendLink(context.Context, domainSend.LinkRequest) (domainSend.GenericResponse, error) {
	return domainSend.GenericResponse{}, nil
}

func (s *stubSendUsecase) SendLocation(context.Context, domainSend.LocationRequest) (domainSend.GenericResponse, error) {
	return domainSend.GenericResponse{}, nil
}

func (s *stubSendUsecase) SendPoll(context.Context, domainSend.PollRequest) (domainSend.GenericResponse, error) {
	return domainSend.GenericResponse{}, nil
}

func (s *stubSendUsecase) SendPresence(context.Context, domainSend.PresenceRequest) (domainSend.GenericResponse, error) {
	return domainSend.GenericResponse{}, nil
}

func (s *stubSendUsecase) SendChatPresence(context.Context, domainSend.ChatPresenceRequest) (domainSend.GenericResponse, error) {
	return domainSend.GenericResponse{}, nil
}

func TestHandleSendTextReturnsStructuredServiceError(t *testing.T) {
	prepareMCPDefaultDevice()

	service := &stubSendUsecase{
		sendTextErr: pkgError.InvalidJID("Phone 5588996420094 is not on whatsapp"),
	}
	handler := &SendHandler{sendService: service}

	result, err := handler.handleSendText(context.Background(), newSendToolRequest(map[string]any{
		"phone":        "5588996420094",
		"message":      "teste",
		"mentions":     []interface{}{},
		"is_forwarded": false,
	}))
	if err != nil {
		t.Fatalf("handleSendText() unexpected rpc error: %v", err)
	}
	if !result.IsError {
		t.Fatal("expected structured tool error result")
	}
	if service.lastTextRequest.Phone != "5588996420094" {
		t.Fatalf("expected phone to be forwarded, got %q", service.lastTextRequest.Phone)
	}

	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content map, got %T", result.StructuredContent)
	}
	if structured["status"] != "failed" {
		t.Fatalf("expected failed status, got %#v", structured["status"])
	}

	resultPayload, ok := structured["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result payload map, got %T", structured["result"])
	}
	errorPayload, ok := resultPayload["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error payload map, got %#v", resultPayload["error"])
	}
	if errorPayload["message"] != "Phone 5588996420094 is not on whatsapp" {
		t.Fatalf("expected detailed error message, got %#v", errorPayload["message"])
	}
	if errorPayload["code"] != "INVALID_JID" {
		t.Fatalf("expected INVALID_JID code, got %#v", errorPayload["code"])
	}
	if errorPayload["http_status"] != 400 {
		t.Fatalf("expected 400 http status, got %#v", errorPayload["http_status"])
	}
}

func TestHandleSendFileMapsLocalPathRequest(t *testing.T) {
	prepareMCPDefaultDevice()

	service := &stubSendUsecase{}
	handler := &SendHandler{sendService: service}

	result, err := handler.handleSendFile(context.Background(), newSendToolRequest(map[string]any{
		"phone":        "5511999999999",
		"file_path":    "/tmp/relatorio.pdf",
		"caption":      "Relatório mensal",
		"is_forwarded": true,
	}))
	if err != nil {
		t.Fatalf("handleSendFile() unexpected error: %v", err)
	}

	if service.lastFileRequest.FilePath == nil || *service.lastFileRequest.FilePath != "/tmp/relatorio.pdf" {
		t.Fatalf("expected file_path to be forwarded, got %#v", service.lastFileRequest.FilePath)
	}
	if service.lastFileRequest.Caption != "Relatório mensal" {
		t.Fatalf("expected caption to be forwarded, got %q", service.lastFileRequest.Caption)
	}
	if !service.lastFileRequest.IsForwarded {
		t.Fatal("expected is_forwarded to be forwarded")
	}

	assertSendResult(t, result, "file-msg", "file")
}

func TestHandleSendVideoMapsLocalPathRequest(t *testing.T) {
	prepareMCPDefaultDevice()

	service := &stubSendUsecase{}
	handler := &SendHandler{sendService: service}

	result, err := handler.handleSendVideo(context.Background(), newSendToolRequest(map[string]any{
		"phone":        "5511999999999",
		"video_path":   "/tmp/demo.mp4",
		"caption":      "Demo",
		"view_once":    true,
		"compress":     false,
		"is_forwarded": true,
	}))
	if err != nil {
		t.Fatalf("handleSendVideo() unexpected error: %v", err)
	}

	if service.lastVideoRequest.VideoPath == nil || *service.lastVideoRequest.VideoPath != "/tmp/demo.mp4" {
		t.Fatalf("expected video_path to be forwarded, got %#v", service.lastVideoRequest.VideoPath)
	}
	if service.lastVideoRequest.Caption != "Demo" {
		t.Fatalf("expected caption to be forwarded, got %q", service.lastVideoRequest.Caption)
	}
	if !service.lastVideoRequest.ViewOnce {
		t.Fatal("expected view_once to be forwarded")
	}
	if service.lastVideoRequest.Compress {
		t.Fatal("expected compress=false to be forwarded")
	}
	if !service.lastVideoRequest.IsForwarded {
		t.Fatal("expected is_forwarded to be forwarded")
	}

	assertSendResult(t, result, "video-msg", "video")
}

func TestHandleSendAudioMapsLocalPathRequest(t *testing.T) {
	prepareMCPDefaultDevice()

	service := &stubSendUsecase{}
	handler := &SendHandler{sendService: service}

	result, err := handler.handleSendAudio(context.Background(), newSendToolRequest(map[string]any{
		"phone":        "5511999999999",
		"audio_path":   "/tmp/audio.ogg",
		"ptt":          true,
		"is_forwarded": true,
	}))
	if err != nil {
		t.Fatalf("handleSendAudio() unexpected error: %v", err)
	}

	if service.lastAudioRequest.AudioPath == nil || *service.lastAudioRequest.AudioPath != "/tmp/audio.ogg" {
		t.Fatalf("expected audio_path to be forwarded, got %#v", service.lastAudioRequest.AudioPath)
	}
	if !service.lastAudioRequest.PTT {
		t.Fatal("expected ptt to be forwarded")
	}
	if !service.lastAudioRequest.IsForwarded {
		t.Fatal("expected is_forwarded to be forwarded")
	}

	assertSendResult(t, result, "audio-msg", "audio")
}

func prepareMCPDefaultDevice() {
	manager := whatsapp.InitializeDeviceManager(nil, nil, nil)
	manager.AddDevice(whatsapp.NewDeviceInstance("test-device", &whatsmeow.Client{}, nil))
}

func newSendToolRequest(arguments map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: arguments,
		},
	}
}

func assertSendResult(t *testing.T, result *mcp.CallToolResult, expectedMessageID, payloadKey string) {
	t.Helper()

	if result.IsError {
		t.Fatal("expected successful tool result")
	}

	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("expected structured content map, got %T", result.StructuredContent)
	}

	resultPayload, ok := structured["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result payload map, got %T", structured["result"])
	}
	if resultPayload["message_id"] != expectedMessageID {
		t.Fatalf("expected message_id %q, got %#v", expectedMessageID, resultPayload["message_id"])
	}
	if _, ok := resultPayload[payloadKey].(map[string]any); !ok {
		t.Fatalf("expected %s payload, got %#v", payloadKey, resultPayload[payloadKey])
	}
}
