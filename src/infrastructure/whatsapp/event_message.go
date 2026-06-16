package whatsapp

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/types/events"
)

// Event types for webhook payload
const (
	EventTypeMessage         = "message"
	EventTypeMessageReaction = "message.reaction"
	EventTypeMessageRevoked  = "message.revoked"
	EventTypeMessageEdited   = "message.edited"
)

// WebhookEvent is the top-level structure for webhook payloads
type WebhookEvent struct {
	Event    string         `json:"event"`
	DeviceID string         `json:"device_id"`
	Payload  map[string]any `json:"payload"`
}

// forwardMessageToWebhook is a helper function to forward message event to webhook url
func forwardMessageToWebhook(ctx context.Context, client *whatsmeow.Client, evt *events.Message, chatStorageRepo domainChatStorage.IChatStorageRepository) error {
	webhookEvent, err := createWebhookEvent(ctx, client, evt, chatStorageRepo)
	if err != nil {
		return err
	}

	payload := map[string]any{
		"event":     webhookEvent.Event,
		"device_id": webhookEvent.DeviceID,
		"payload":   webhookEvent.Payload,
	}

	return forwardPayloadToConfiguredWebhooks(ctx, payload, webhookEvent.Event)
}

func createWebhookEvent(ctx context.Context, client *whatsmeow.Client, evt *events.Message, chatStorageRepo domainChatStorage.IChatStorageRepository) (*WebhookEvent, error) {
	webhookEvent := &WebhookEvent{
		Event:   EventTypeMessage,
		Payload: make(map[string]any),
	}

	// Set device_id
	if client != nil && client.Store != nil && client.Store.ID != nil {
		deviceJID := NormalizeJIDFromLID(ctx, client.Store.ID.ToNonAD(), client)
		webhookEvent.DeviceID = deviceJID.ToNonAD().String()
	}

	// Determine event type and build payload
	eventType, payload, err := buildEventPayloadWithStorage(ctx, client, evt, chatStorageRepo)
	if err != nil {
		return nil, err
	}

	webhookEvent.Event = eventType
	webhookEvent.Payload = payload

	return webhookEvent, nil
}

func buildEventPayload(ctx context.Context, client *whatsmeow.Client, evt *events.Message) (string, map[string]any, error) {
	return buildEventPayloadWithStorage(ctx, client, evt, nil)
}

func buildEventPayloadWithStorage(ctx context.Context, client *whatsmeow.Client, evt *events.Message, chatStorageRepo domainChatStorage.IChatStorageRepository) (string, map[string]any, error) {
	payload := make(map[string]any)

	msg := utils.UnwrapMessage(evt.Message)

	// Common fields for all message types
	payload["id"] = evt.Info.ID
	payload["timestamp"] = evt.Info.Timestamp.Format(time.RFC3339)
	payload["is_from_me"] = evt.Info.IsFromMe

	// Build from/from_lid fields
	buildFromFields(ctx, client, evt, payload)

	// Set from_name (pushname)
	if pushname := evt.Info.PushName; pushname != "" {
		payload["from_name"] = pushname
	}

	// Check for protocol messages (revoke, edit)
	if protocolMessage := msg.GetProtocolMessage(); protocolMessage != nil {
		protocolType := protocolMessage.GetType().String()

		switch protocolType {
		case "REVOKE":
			if key := protocolMessage.GetKey(); key != nil {
				payload["revoked_message_id"] = key.GetID()
				payload["revoked_from_me"] = key.GetFromMe()
				if key.GetRemoteJID() != "" {
					payload["revoked_chat"] = key.GetRemoteJID()
				}
			}
			return EventTypeMessageRevoked, payload, nil

		case "MESSAGE_EDIT":
			if key := protocolMessage.GetKey(); key != nil {
				payload["original_message_id"] = key.GetID()
			}
			if editedMessage := protocolMessage.GetEditedMessage(); editedMessage != nil {
				if editedText := editedMessage.GetExtendedTextMessage(); editedText != nil {
					payload["body"] = editedText.GetText()
				} else if editedConv := editedMessage.GetConversation(); editedConv != "" {
					payload["body"] = editedConv
				}
			}
			return EventTypeMessageEdited, payload, nil
		}
	}

	// Check for reaction message
	if reactionMessage := msg.GetReactionMessage(); reactionMessage != nil {
		payload["reaction"] = reactionMessage.GetText()
		if key := reactionMessage.GetKey(); key != nil {
			payload["reacted_message_id"] = key.GetID()
		}
		return EventTypeMessageReaction, payload, nil
	}

	// Regular message - build body and media fields
	if err := buildMessageBody(ctx, client, evt, payload); err != nil {
		return "", nil, err
	}

	// Add optional fields
	if err := buildOptionalFields(ctx, client, evt, msg, payload, chatStorageRepo); err != nil {
		return "", nil, err
	}

	return EventTypeMessage, payload, nil
}

func buildFromFields(ctx context.Context, client *whatsmeow.Client, evt *events.Message, payload map[string]any) {
	chatJID := evt.Info.Chat.ToNonAD()
	if chatJID.Server == "lid" {
		payload["chat_lid"] = chatJID.String()
		chatJID = NormalizeJIDFromLID(ctx, chatJID, client).ToNonAD()
	}
	payload["chat_id"] = chatJID.String()

	senderJID := evt.Info.Sender
	if senderJID.Server == "lid" {
		payload["from_lid"] = senderJID.ToNonAD().String()
	}

	normalizedSenderJID := NormalizeJIDFromLID(ctx, senderJID, client)
	payload["from"] = normalizedSenderJID.ToNonAD().String()
}

func buildMessageBody(ctx context.Context, client *whatsmeow.Client, evt *events.Message, payload map[string]any) error {
	message := utils.BuildEventMessage(evt)

	// Replace LID mentions with phone numbers in text
	if message.Text != "" && client != nil && client.Store != nil && client.Store.LIDs != nil {
		tags := regexp.MustCompile(`\B@\w+`).FindAllString(message.Text, -1)
		tagsMap := make(map[string]bool)
		for _, tag := range tags {
			tagsMap[tag] = true
		}
		for tag := range tagsMap {
			lid, err := types.ParseJID(tag[1:] + "@lid")
			if err != nil {
				logrus.Errorf("Error when parse jid: %v", err)
			} else {
				pn, err := client.Store.LIDs.GetPNForLID(ctx, lid)
				if err != nil {
					logrus.Errorf("Error when get pn for lid %s: %v", lid.ToNonAD().String(), err)
				}
				if !pn.IsEmpty() {
					message.Text = strings.Replace(message.Text, tag, fmt.Sprintf("@%s", pn.User), -1)
				}
			}
		}
		payload["body"] = message.Text
	} else if message.Text != "" {
		payload["body"] = message.Text
	}

	// Fallback: extract caption from media messages if no text body was set
	if _, hasBody := payload["body"]; !hasBody {
		msg := utils.UnwrapMessage(evt.Message)
		if caption := utils.ExtractMediaCaption(msg); caption != "" {
			payload["body"] = caption
		}
	}

	// Add reply context if present
	if message.RepliedId != "" {
		payload["replied_to_id"] = message.RepliedId
	}
	if message.QuotedMessage != "" {
		payload["quoted_body"] = message.QuotedMessage
	}
	if message.QuotedParticipant != "" {
		payload["quoted_sender"] = message.QuotedParticipant
	}

	return nil
}

func buildOptionalFields(ctx context.Context, client *whatsmeow.Client, evt *events.Message, msg *waE2E.Message, payload map[string]any, chatStorageRepo domainChatStorage.IChatStorageRepository) error {
	if evt.IsViewOnce {
		payload["view_once"] = true
	}

	if utils.BuildForwarded(evt) {
		payload["forwarded"] = true
	}

	stored := storedMessageForWebhook(evt, chatStorageRepo)
	if err := buildMediaFields(msg, payload, stored); err != nil {
		return err
	}

	buildOtherMessageTypes(msg, payload)

	return nil
}

func storedMessageForWebhook(evt *events.Message, chatStorageRepo domainChatStorage.IChatStorageRepository) *domainChatStorage.Message {
	if evt == nil || chatStorageRepo == nil {
		return nil
	}
	stored, err := chatStorageRepo.GetMessageByID(evt.Info.ID)
	if err != nil {
		logrus.WithError(err).Debugf("Failed to lookup stored message %s for webhook media payload", evt.Info.ID)
		return nil
	}
	return stored
}

func buildMediaFields(msg *waE2E.Message, payload map[string]any, stored *domainChatStorage.Message) error {
	if audioMedia := msg.GetAudioMessage(); audioMedia != nil {
		if mediaPayload, ok := localMediaPayload(stored, "audio", "", false); ok && config.WhatsappAutoDownloadMedia {
			payload["audio"] = mediaPayload
		} else {
			payload["audio"] = map[string]any{
				"url": audioMedia.GetURL(),
			}
		}
	}

	if documentMedia := msg.GetDocumentMessage(); documentMedia != nil {
		if mediaPayload, ok := localMediaPayload(stored, "document", documentMedia.GetCaption(), true); ok && config.WhatsappAutoDownloadMedia {
			payload["document"] = mediaPayload
		} else {
			payload["document"] = map[string]any{
				"url":      documentMedia.GetURL(),
				"filename": documentMedia.GetFileName(),
			}
		}
	}

	if imageMedia := msg.GetImageMessage(); imageMedia != nil {
		if mediaPayload, ok := localMediaPayload(stored, "image", imageMedia.GetCaption(), true); ok && config.WhatsappAutoDownloadMedia {
			payload["image"] = mediaPayload
		} else {
			payload["image"] = map[string]any{
				"url":     imageMedia.GetURL(),
				"caption": imageMedia.GetCaption(),
			}
		}
	}

	if stickerMedia := msg.GetStickerMessage(); stickerMedia != nil {
		if mediaPayload, ok := localMediaPayload(stored, "sticker", "", false); ok && config.WhatsappAutoDownloadMedia {
			payload["sticker"] = mediaPayload
		} else {
			payload["sticker"] = map[string]any{
				"url": stickerMedia.GetURL(),
			}
		}
	}

	if videoMedia := msg.GetVideoMessage(); videoMedia != nil {
		if mediaPayload, ok := localMediaPayload(stored, "video", videoMedia.GetCaption(), true); ok && config.WhatsappAutoDownloadMedia {
			payload["video"] = mediaPayload
		} else {
			payload["video"] = map[string]any{
				"url":     videoMedia.GetURL(),
				"caption": videoMedia.GetCaption(),
			}
		}
	}

	if ptvMedia := msg.GetPtvMessage(); ptvMedia != nil {
		if mediaPayload, ok := localMediaPayload(stored, "video_note", ptvMedia.GetCaption(), true); ok && config.WhatsappAutoDownloadMedia {
			payload["video_note"] = mediaPayload
		} else {
			payload["video_note"] = map[string]any{
				"url":     ptvMedia.GetURL(),
				"caption": ptvMedia.GetCaption(),
			}
		}
	}

	return nil
}

func localMediaPayload(stored *domainChatStorage.Message, mediaType string, caption string, structured bool) (any, bool) {
	if stored == nil || stored.MediaType != mediaType {
		return nil, false
	}
	path := strings.TrimSpace(stored.LocalMediaPath)
	if path == "" {
		return nil, false
	}
	if !structured {
		return path, true
	}
	return buildAutoDownloadPayload(utils.ExtractedMedia{
		MediaPath: path,
		Caption:   caption,
	}), true
}

// buildAutoDownloadPayload builds the media payload for auto-downloaded media.
// Returns just the path string if no caption (backward compatible), or a map with path+caption.
func buildAutoDownloadPayload(extracted utils.ExtractedMedia) any {
	if extracted.Caption != "" {
		return map[string]any{
			"path":    extracted.MediaPath,
			"caption": extracted.Caption,
		}
	}
	return extracted.MediaPath
}

func buildOtherMessageTypes(msg *waE2E.Message, payload map[string]any) {
	if contactMessage := msg.GetContactMessage(); contactMessage != nil {
		payload["contact"] = contactMessage
	}

	if contactsArrayMessage := msg.GetContactsArrayMessage(); contactsArrayMessage != nil {
		payload["contacts_array"] = contactsArrayMessage.GetContacts()
	}

	if listMessage := msg.GetListMessage(); listMessage != nil {
		payload["list"] = listMessage
	}

	if liveLocationMessage := msg.GetLiveLocationMessage(); liveLocationMessage != nil {
		payload["live_location"] = liveLocationMessage
	}

	if locationMessage := msg.GetLocationMessage(); locationMessage != nil {
		payload["location"] = locationMessage
	}

	if orderMessage := msg.GetOrderMessage(); orderMessage != nil {
		payload["order"] = orderMessage
	}
}
