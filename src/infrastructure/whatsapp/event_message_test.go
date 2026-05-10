package whatsapp

import (
	"context"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

func TestBuildEventPayloadIncludesIsFromMe(t *testing.T) {
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("123", types.DefaultUserServer),
				Sender:   types.NewJID("123", types.DefaultUserServer),
				IsFromMe: true,
			},
			ID:        "MSG123",
			Timestamp: time.Date(2026, time.February, 8, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			Conversation: protoString("hello"),
		},
	}

	eventType, payload, err := buildEventPayload(context.Background(), nil, evt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if eventType != EventTypeMessage {
		t.Fatalf("expected event type %s, got %s", EventTypeMessage, eventType)
	}
	if value, ok := payload["is_from_me"]; !ok {
		t.Fatalf("expected is_from_me in payload")
	} else if isFromMe, ok := value.(bool); !ok || !isFromMe {
		t.Fatalf("expected is_from_me=true, got %v", value)
	}
}

func TestBuildEventPayloadRevokedIncludesIsFromMe(t *testing.T) {
	key := &waCommon.MessageKey{
		RemoteJID: protoString("123@s.whatsapp.net"),
		FromMe:    protoBool(true),
		ID:        protoString("REV123"),
	}
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("123", types.DefaultUserServer),
				Sender:   types.NewJID("123", types.DefaultUserServer),
				IsFromMe: true,
			},
			ID:        "MSG124",
			Timestamp: time.Date(2026, time.February, 8, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				Type: protoProtocolMessageType(waE2E.ProtocolMessage_REVOKE),
				Key:  key,
			},
		},
	}

	eventType, payload, err := buildEventPayload(context.Background(), nil, evt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if eventType != EventTypeMessageRevoked {
		t.Fatalf("expected event type %s, got %s", EventTypeMessageRevoked, eventType)
	}
	if value, ok := payload["is_from_me"]; !ok {
		t.Fatalf("expected is_from_me in payload")
	} else if isFromMe, ok := value.(bool); !ok || !isFromMe {
		t.Fatalf("expected is_from_me=true, got %v", value)
	}
}

func TestExtractRevokeTargetFallsBackToEventChat(t *testing.T) {
	key := &waCommon.MessageKey{
		FromMe: protoBool(true),
		ID:     protoString("REV123"),
	}
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:   types.NewJID("120363424157959439", types.GroupServer),
				Sender: types.NewJID("5521982572423", types.DefaultUserServer),
			},
			ID:        "MSG124",
			Timestamp: time.Date(2026, time.February, 8, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ProtocolMessage: &waE2E.ProtocolMessage{
				Type: protoProtocolMessageType(waE2E.ProtocolMessage_REVOKE),
				Key:  key,
			},
		},
	}

	messageID, chatJID, ok := extractRevokeTarget(context.Background(), evt, nil)
	if !ok {
		t.Fatal("expected revoke target to be extracted")
	}
	if messageID != "REV123" {
		t.Fatalf("expected message id REV123, got %q", messageID)
	}
	if chatJID != "120363424157959439@g.us" {
		t.Fatalf("expected event chat fallback, got %q", chatJID)
	}
}

func TestIncomingDownloadableMediaSupportsReceivedMediaTypes(t *testing.T) {
	tests := []struct {
		name      string
		msg       *waE2E.Message
		mediaType string
	}{
		{
			name: "image",
			msg: &waE2E.Message{
				ImageMessage: &waE2E.ImageMessage{URL: protoString("https://mmg.whatsapp.net/image")},
			},
			mediaType: "image",
		},
		{
			name: "video",
			msg: &waE2E.Message{
				VideoMessage: &waE2E.VideoMessage{URL: protoString("https://mmg.whatsapp.net/video")},
			},
			mediaType: "video",
		},
		{
			name: "video note",
			msg: &waE2E.Message{
				PtvMessage: &waE2E.VideoMessage{URL: protoString("https://mmg.whatsapp.net/ptv")},
			},
			mediaType: "video_note",
		},
		{
			name: "audio",
			msg: &waE2E.Message{
				AudioMessage: &waE2E.AudioMessage{URL: protoString("https://mmg.whatsapp.net/audio")},
			},
			mediaType: "audio",
		},
		{
			name: "document",
			msg: &waE2E.Message{
				DocumentMessage: &waE2E.DocumentMessage{URL: protoString("https://mmg.whatsapp.net/document")},
			},
			mediaType: "document",
		},
		{
			name: "sticker",
			msg: &waE2E.Message{
				StickerMessage: &waE2E.StickerMessage{URL: protoString("https://mmg.whatsapp.net/sticker")},
			},
			mediaType: "sticker",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			downloadable, mediaType := incomingDownloadableMedia(tt.msg)
			if downloadable == nil {
				t.Fatal("expected downloadable media")
			}
			if mediaType != tt.mediaType {
				t.Fatalf("expected media type %q, got %q", tt.mediaType, mediaType)
			}
		})
	}
}

func protoString(value string) *string {
	return &value
}

func protoBool(value bool) *bool {
	return &value
}

func protoProtocolMessageType(value waE2E.ProtocolMessage_Type) *waE2E.ProtocolMessage_Type {
	return &value
}

func TestBuildEventPayloadImageWithCaption(t *testing.T) {
	config.WhatsappAutoDownloadMedia = false
	caption := "Check this out!"
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("123", types.DefaultUserServer),
				Sender:   types.NewJID("456", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "MSG200",
			Timestamp: time.Date(2026, time.February, 8, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				Caption: &caption,
			},
		},
	}

	eventType, payload, err := buildEventPayload(context.Background(), nil, evt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if eventType != EventTypeMessage {
		t.Fatalf("expected event type %s, got %s", EventTypeMessage, eventType)
	}
	body, ok := payload["body"]
	if !ok {
		t.Fatal("expected body in payload for image with caption")
	}
	if body != "Check this out!" {
		t.Fatalf("expected body='Check this out!', got %v", body)
	}
}

func TestBuildEventPayloadVideoWithCaption(t *testing.T) {
	config.WhatsappAutoDownloadMedia = false
	caption := "Watch this video"
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("123", types.DefaultUserServer),
				Sender:   types.NewJID("456", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "MSG201",
			Timestamp: time.Date(2026, time.February, 8, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			VideoMessage: &waE2E.VideoMessage{
				Caption: &caption,
			},
		},
	}

	eventType, payload, err := buildEventPayload(context.Background(), nil, evt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if eventType != EventTypeMessage {
		t.Fatalf("expected event type %s, got %s", EventTypeMessage, eventType)
	}
	body, ok := payload["body"]
	if !ok {
		t.Fatal("expected body in payload for video with caption")
	}
	if body != "Watch this video" {
		t.Fatalf("expected body='Watch this video', got %v", body)
	}
}

func TestBuildEventPayloadImageWithoutCaption(t *testing.T) {
	config.WhatsappAutoDownloadMedia = false
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("123", types.DefaultUserServer),
				Sender:   types.NewJID("456", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "MSG202",
			Timestamp: time.Date(2026, time.February, 8, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{},
		},
	}

	_, payload, err := buildEventPayload(context.Background(), nil, evt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if _, ok := payload["body"]; ok {
		t.Fatal("expected no body in payload for image without caption")
	}
}

func TestBuildEventPayloadDocumentWithCaption(t *testing.T) {
	config.WhatsappAutoDownloadMedia = false
	caption := "Important document"
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("123", types.DefaultUserServer),
				Sender:   types.NewJID("456", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "MSG203",
			Timestamp: time.Date(2026, time.February, 8, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			DocumentMessage: &waE2E.DocumentMessage{
				Caption: &caption,
			},
		},
	}

	eventType, payload, err := buildEventPayload(context.Background(), nil, evt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if eventType != EventTypeMessage {
		t.Fatalf("expected event type %s, got %s", EventTypeMessage, eventType)
	}
	body, ok := payload["body"]
	if !ok {
		t.Fatal("expected body in payload for document with caption")
	}
	if body != "Important document" {
		t.Fatalf("expected body='Important document', got %v", body)
	}
}
