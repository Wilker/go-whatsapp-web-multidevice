package rest

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	"github.com/gofiber/fiber/v3"
)

type fakeChatUsecase struct {
	lastDeleteRequest domainChat.DeleteChatLocalMediaRequest
}

func (f *fakeChatUsecase) ListChats(context.Context, domainChat.ListChatsRequest) (domainChat.ListChatsResponse, error) {
	return domainChat.ListChatsResponse{}, nil
}

func (f *fakeChatUsecase) GetChatMessages(context.Context, domainChat.GetChatMessagesRequest) (domainChat.GetChatMessagesResponse, error) {
	return domainChat.GetChatMessagesResponse{}, nil
}

func (f *fakeChatUsecase) PinChat(context.Context, domainChat.PinChatRequest) (domainChat.PinChatResponse, error) {
	return domainChat.PinChatResponse{}, nil
}

func (f *fakeChatUsecase) SetDisappearingTimer(context.Context, domainChat.SetDisappearingTimerRequest) (domainChat.SetDisappearingTimerResponse, error) {
	return domainChat.SetDisappearingTimerResponse{}, nil
}

func (f *fakeChatUsecase) ArchiveChat(context.Context, domainChat.ArchiveChatRequest) (domainChat.ArchiveChatResponse, error) {
	return domainChat.ArchiveChatResponse{}, nil
}

func (f *fakeChatUsecase) GetChatMediaPolicy(_ context.Context, request domainChat.GetChatMediaPolicyRequest) (domainChat.ChatMediaPolicyResponse, error) {
	return domainChat.ChatMediaPolicyResponse{
		ChatJID:   request.ChatJID,
		Mode:      domainChat.MediaPolicyModePermanent,
		IsDefault: true,
	}, nil
}

func (f *fakeChatUsecase) SetChatMediaPolicy(_ context.Context, request domainChat.SetChatMediaPolicyRequest) (domainChat.ChatMediaPolicyResponse, error) {
	return domainChat.ChatMediaPolicyResponse{
		ChatJID:       request.ChatJID,
		Mode:          domainChat.MediaPolicyModeEphemeral,
		RetentionDays: domainChat.MediaPolicyRetentionDays,
		Ephemeral:     true,
	}, nil
}

func (f *fakeChatUsecase) ResetChatMediaPolicy(_ context.Context, request domainChat.ResetChatMediaPolicyRequest) (domainChat.ChatMediaPolicyResponse, error) {
	return domainChat.ChatMediaPolicyResponse{
		ChatJID:   request.ChatJID,
		Mode:      domainChat.MediaPolicyModePermanent,
		IsDefault: true,
	}, nil
}

func (f *fakeChatUsecase) DeleteChatLocalMedia(_ context.Context, request domainChat.DeleteChatLocalMediaRequest) (domainChat.LocalMediaDeleteResponse, error) {
	f.lastDeleteRequest = request
	return domainChat.LocalMediaDeleteResponse{
		ChatJID:    request.ChatJID,
		DryRun:     request.DryRun,
		MediaTypes: []string{"image", "video", "video_note"},
	}, nil
}

func (f *fakeChatUsecase) CleanupExpiredLocalMedia(context.Context) (domainChat.CleanupExpiredLocalMediaResponse, error) {
	return domainChat.CleanupExpiredLocalMediaResponse{}, nil
}

func TestChatMediaPolicyRoutes(t *testing.T) {
	app := fiber.New()
	service := &fakeChatUsecase{}
	InitRestChat(app, service)

	for _, tc := range []struct {
		method string
		path   string
	}{
		{method: "GET", path: "/chat/120363424157959439@g.us/media-policy"},
		{method: "PUT", path: "/chat/120363424157959439@g.us/media-policy"},
		{method: "DELETE", path: "/chat/120363424157959439@g.us/media-policy"},
	} {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test(%s %s) unexpected error: %v", tc.method, tc.path, err)
		}
		if resp.StatusCode != 200 {
			t.Fatalf("expected %s %s to return 200, got %d", tc.method, tc.path, resp.StatusCode)
		}
	}
}

func TestDeleteChatLocalMediaDefaultsToDryRun(t *testing.T) {
	app := fiber.New()
	service := &fakeChatUsecase{}
	InitRestChat(app, service)

	req := httptest.NewRequest("POST", "/chat/120363424157959439@g.us/local-media/delete", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test() unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
	if !service.lastDeleteRequest.DryRun {
		t.Fatal("expected dry_run to default to true")
	}

	var body struct {
		Results domainChat.LocalMediaDeleteResponse `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("Decode() unexpected error: %v", err)
	}
	if !body.Results.DryRun {
		t.Fatalf("expected response dry_run=true, got %+v", body.Results)
	}
}
