package whatsapp

import (
	"context"
	"testing"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/stretchr/testify/assert"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store"
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

func TestBuildMediaFieldsUsesStoredLocalMediaPath(t *testing.T) {
	originalAutoDownload := config.WhatsappAutoDownloadMedia
	config.WhatsappAutoDownloadMedia = true
	t.Cleanup(func() { config.WhatsappAutoDownloadMedia = originalAutoDownload })

	caption := "foto"
	payload := map[string]any{}
	err := buildMediaFields(&waE2E.Message{
		ImageMessage: &waE2E.ImageMessage{
			URL:     protoString("https://mmg.whatsapp.net/image"),
			Caption: protoString(caption),
		},
	}, payload, &domainChatStorage.Message{
		MediaType:      "image",
		LocalMediaPath: "statics/media/chat/image.jpg",
	})
	if err != nil {
		t.Fatalf("buildMediaFields() unexpected error: %v", err)
	}

	imagePayload, ok := payload["image"].(map[string]any)
	if !ok {
		t.Fatalf("expected structured image payload, got %#v", payload["image"])
	}
	if imagePayload["path"] != "statics/media/chat/image.jpg" || imagePayload["caption"] != caption {
		t.Fatalf("expected stored local media payload, got %#v", imagePayload)
	}
}

func TestBuildMediaFieldsFallsBackToURLWithoutDownloading(t *testing.T) {
	originalAutoDownload := config.WhatsappAutoDownloadMedia
	config.WhatsappAutoDownloadMedia = true
	t.Cleanup(func() { config.WhatsappAutoDownloadMedia = originalAutoDownload })

	payload := map[string]any{}
	err := buildMediaFields(&waE2E.Message{
		VideoMessage: &waE2E.VideoMessage{
			URL:     protoString("https://mmg.whatsapp.net/video"),
			Caption: protoString("video"),
		},
	}, payload, nil)
	if err != nil {
		t.Fatalf("buildMediaFields() unexpected error: %v", err)
	}

	videoPayload, ok := payload["video"].(map[string]any)
	if !ok {
		t.Fatalf("expected fallback video payload, got %#v", payload["video"])
	}
	if videoPayload["url"] != "https://mmg.whatsapp.net/video" {
		t.Fatalf("expected URL fallback payload, got %#v", videoPayload)
	}
	if _, ok := videoPayload["path"]; ok {
		t.Fatalf("expected no local path in fallback payload, got %#v", videoPayload)
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

func TestBuildEventPayloadQuotedBodyUsesQuotedCaption(t *testing.T) {
	oldAutoDownload := config.WhatsappAutoDownloadMedia
	config.WhatsappAutoDownloadMedia = false
	t.Cleanup(func() {
		config.WhatsappAutoDownloadMedia = oldAutoDownload
	})

	tests := []struct {
		name           string
		quotedMessage  func(*string) *waE2E.Message
		wantQuotedBody string
	}{
		{
			name: "uses quoted image caption",
			quotedMessage: func(caption *string) *waE2E.Message {
				return &waE2E.Message{
					ImageMessage: &waE2E.ImageMessage{
						Caption: caption,
					},
				}
			},
			wantQuotedBody: "Launch checklist",
		},
		{
			name: "uses quoted document caption",
			quotedMessage: func(caption *string) *waE2E.Message {
				return &waE2E.Message{
					DocumentMessage: &waE2E.DocumentMessage{
						Caption: caption,
					},
				}
			},
			wantQuotedBody: "Project brief",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replyText := "Thanks for the update"
			quotedCaption := tt.wantQuotedBody
			evt := &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Chat:     types.NewJID("123", types.DefaultUserServer),
						Sender:   types.NewJID("456", types.DefaultUserServer),
						IsFromMe: false,
					},
					ID:        "MSG206",
					Timestamp: time.Date(2026, time.February, 8, 10, 0, 0, 0, time.UTC),
				},
				Message: &waE2E.Message{
					ExtendedTextMessage: &waE2E.ExtendedTextMessage{
						Text: &replyText,
						ContextInfo: &waE2E.ContextInfo{
							StanzaID:      protoString("QUOTE206"),
							QuotedMessage: tt.quotedMessage(&quotedCaption),
						},
					},
				},
			}

			eventType, payload, err := buildEventPayload(context.Background(), nil, evt)
			assert.NoError(t, err)
			assert.Equal(t, EventTypeMessage, eventType)
			assert.Contains(t, payload, "quoted_body")
			assert.Equal(t, tt.wantQuotedBody, payload["quoted_body"])
		})
	}
}

func TestBuildEventPayloadContactIncludesPhoneNumber(t *testing.T) {
	name := "Alice"
	vcard := "BEGIN:VCARD\nVERSION:3.0\nN:;Alice;;;\nFN:Alice\nTEL;type=Mobile:+62 812 3456 7890\nEND:VCARD"
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("123", types.DefaultUserServer),
				Sender:   types.NewJID("456", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "MSG204",
			Timestamp: time.Date(2026, time.February, 8, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ContactMessage: &waE2E.ContactMessage{
				DisplayName: &name,
				Vcard:       &vcard,
			},
		},
	}

	_, payload, err := buildEventPayload(context.Background(), nil, evt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	contact, ok := payload["contact"].(webhookContactPayload)
	if !ok {
		t.Fatalf("expected contact payload to be webhookContactPayload, got %T", payload["contact"])
	}
	if contact.DisplayName != "Alice" {
		t.Fatalf("expected display name Alice, got %q", contact.DisplayName)
	}
	if contact.PhoneNumber != "+62 812 3456 7890" {
		t.Fatalf("expected phone number from vCard, got %q", contact.PhoneNumber)
	}
}

func TestBuildEventPayloadContactsArrayIncludesPhoneNumbers(t *testing.T) {
	nameOne := "Alice"
	vcardOne := "BEGIN:VCARD\nVERSION:3.0\nN:;Alice;;;\nFN:Alice\nTEL;type=Mobile:+62 812 3456 7890\nEND:VCARD"
	nameTwo := "Bob"
	vcardTwo := "BEGIN:VCARD\nVERSION:3.0\nN:;Bob;;;\nFN:Bob\nTEL;type=Mobile:+62 813 9876 5432\nEND:VCARD"
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:     types.NewJID("123", types.DefaultUserServer),
				Sender:   types.NewJID("456", types.DefaultUserServer),
				IsFromMe: false,
			},
			ID:        "MSG205",
			Timestamp: time.Date(2026, time.February, 8, 10, 0, 0, 0, time.UTC),
		},
		Message: &waE2E.Message{
			ContactsArrayMessage: &waE2E.ContactsArrayMessage{
				Contacts: []*waE2E.ContactMessage{
					{
						DisplayName: &nameOne,
						Vcard:       &vcardOne,
					},
					{
						DisplayName: &nameTwo,
						Vcard:       &vcardTwo,
					},
				},
			},
		},
	}

	_, payload, err := buildEventPayload(context.Background(), nil, evt)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	contacts, ok := payload["contacts_array"].([]webhookContactPayload)
	if !ok {
		t.Fatalf("expected contacts_array to be []webhookContactPayload, got %T", payload["contacts_array"])
	}
	if len(contacts) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(contacts))
	}
	if contacts[0].PhoneNumber != "+62 812 3456 7890" {
		t.Fatalf("expected first phone number, got %q", contacts[0].PhoneNumber)
	}
	if contacts[1].PhoneNumber != "+62 813 9876 5432" {
		t.Fatalf("expected second phone number, got %q", contacts[1].PhoneNumber)
	}
}

func TestBuildEventPayloadIncludesSenderDisplayName(t *testing.T) {
	accountJID := types.NewJID("628111111111", types.DefaultUserServer)
	senderJID := types.NewJID("628123456789", types.DefaultUserServer)
	contacts := &senderDisplayNameContactGetter{
		contacts: map[types.JID]types.ContactInfo{
			senderJID: {
				Found:    true,
				FullName: "Saved Contact",
			},
		},
	}
	client := &whatsmeow.Client{
		Store: &store.Device{
			ID:       &accountJID,
			PushName: "Active Account",
			Contacts: contacts,
		},
	}

	tests := []struct {
		name      string
		isFromMe  bool
		sender    types.JID
		message   *waE2E.Message
		wantEvent string
		wantName  string
	}{
		{
			name:      "message",
			sender:    senderJID,
			message:   &waE2E.Message{Conversation: protoString("hello")},
			wantEvent: EventTypeMessage,
			wantName:  "Saved Contact",
		},
		{
			name:   "reaction before early return",
			sender: senderJID,
			message: &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
				Key: &waCommon.MessageKey{
					RemoteJID: protoString("628100000000@s.whatsapp.net"),
					ID:        protoString("target-message"),
				},
				Text: protoString("👍"),
			}},
			wantEvent: EventTypeMessageReaction,
			wantName:  "Saved Contact",
		},
		{
			name:   "revoke before early return",
			sender: senderJID,
			message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
				Type: protoProtocolMessageType(waE2E.ProtocolMessage_REVOKE),
				Key:  &waCommon.MessageKey{ID: protoString("revoked-message")},
			}},
			wantEvent: EventTypeMessageRevoked,
			wantName:  "Saved Contact",
		},
		{
			name:   "edit before early return",
			sender: senderJID,
			message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
				Type: protoProtocolMessageType(waE2E.ProtocolMessage_MESSAGE_EDIT),
				Key:  &waCommon.MessageKey{ID: protoString("edited-message")},
				EditedMessage: &waE2E.Message{
					Conversation: protoString("updated"),
				},
			}},
			wantEvent: EventTypeMessageEdited,
			wantName:  "Saved Contact",
		},
		{
			name:      "outgoing active account",
			isFromMe:  true,
			sender:    accountJID,
			message:   &waE2E.Message{Conversation: protoString("hello")},
			wantEvent: EventTypeMessage,
			wantName:  "Active Account",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := &events.Message{
				Info: types.MessageInfo{
					MessageSource: types.MessageSource{
						Chat:     types.NewJID("628100000000", types.DefaultUserServer),
						Sender:   tt.sender,
						IsFromMe: tt.isFromMe,
					},
					ID:        "sender-display-name",
					PushName:  "Live Push",
					Timestamp: time.Date(2026, time.July, 28, 10, 0, 0, 0, time.UTC),
				},
				Message: tt.message,
			}

			eventType, payload, err := buildEventPayload(context.Background(), client, evt)

			assert.NoError(t, err)
			assert.Equal(t, tt.wantEvent, eventType)
			assert.Equal(t, tt.sender.String(), payload["from"])
			assert.Equal(t, "Live Push", payload["from_name"])
			assert.Equal(t, tt.wantName, payload["sender_display_name"])
		})
	}
}
