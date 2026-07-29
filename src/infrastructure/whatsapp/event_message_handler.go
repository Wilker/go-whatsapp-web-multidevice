package whatsapp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

var extractIncomingMediaFunc = utils.ExtractMedia

func handleMessage(ctx context.Context, evt *events.Message, chatStorageRepo domainChatStorage.IChatStorageRepository, client *whatsmeow.Client) {
	// Log message metadata
	metaParts := buildMessageMetaParts(evt)
	log.Infof("Received message %s from %s (%s): %+v",
		evt.Info.ID,
		evt.Info.SourceString(),
		strings.Join(metaParts, ", "),
		evt.Message,
	)

	evt = materializeSecretEditMessage(ctx, evt, client)

	if isReactionMessage(evt) {
		if err := chatStorageRepo.CreateReaction(ctx, evt); err != nil {
			log.Errorf("Failed to store incoming reaction %s: %v", evt.Info.ID, err)
		}
		handleWebhookForward(ctx, evt, client, chatStorageRepo)
		return
	}

	if handleMessageRevoke(ctx, evt, chatStorageRepo, client) {
		handleWebhookForward(ctx, evt, client, chatStorageRepo)
		return
	}

	if err := chatStorageRepo.CreateMessage(ctx, evt); err != nil {
		// Log storage errors to avoid silent failures that could lead to data loss
		log.Errorf("Failed to store incoming message %s: %v", evt.Info.ID, err)
	}

	handleIncomingMediaDownload(ctx, evt, chatStorageRepo, client)

	// Auto-mark message as read if configured
	handleAutoMarkRead(ctx, evt, client)

	// Handle auto-reply if configured
	handleAutoReply(ctx, evt, chatStorageRepo, client)

	// Forward to webhook if configured
	handleWebhookForward(ctx, evt, client, chatStorageRepo)
}

func handleMessageRevoke(ctx context.Context, evt *events.Message, chatStorageRepo domainChatStorage.IChatStorageRepository, client *whatsmeow.Client) bool {
	revokedMessageID, chatJID, ok := extractRevokeTarget(ctx, evt, client)
	if !ok {
		return false
	}
	if chatStorageRepo == nil {
		log.Warnf("Cannot mark revoked message %s as deleted: chat storage is nil", revokedMessageID)
		return true
	}

	if err := chatStorageRepo.DeleteMessage(revokedMessageID, chatJID); err != nil {
		log.Errorf("Failed to mark revoked message %s as deleted in database: %v", revokedMessageID, err)
	} else {
		log.Infof("Marked revoked message %s as deleted in database", revokedMessageID)
	}
	return true
}

func extractRevokeTarget(ctx context.Context, evt *events.Message, client *whatsmeow.Client) (messageID string, chatJID string, ok bool) {
	if evt == nil {
		return "", "", false
	}

	msg := utils.UnwrapMessage(evt.Message)
	if msg == nil {
		return "", "", false
	}

	protocolMessage := msg.GetProtocolMessage()
	if protocolMessage == nil || protocolMessage.GetType().String() != "REVOKE" {
		return "", "", false
	}

	key := protocolMessage.GetKey()
	if key == nil {
		return "", "", false
	}

	messageID = strings.TrimSpace(key.GetID())
	chatJID = strings.TrimSpace(key.GetRemoteJID())
	if chatJID == "" {
		chatJID = NormalizeJIDFromLID(ctx, evt.Info.Chat.ToNonAD(), client).ToNonAD().String()
	} else if parsed, err := types.ParseJID(chatJID); err == nil {
		chatJID = NormalizeJIDFromLID(ctx, parsed.ToNonAD(), client).ToNonAD().String()
	}

	return messageID, chatJID, messageID != "" && chatJID != ""
}

func buildMessageMetaParts(evt *events.Message) []string {
	metaParts := []string{
		fmt.Sprintf("pushname: %s", evt.Info.PushName),
		fmt.Sprintf("timestamp: %s", evt.Info.Timestamp),
	}
	if evt.Info.Type != "" {
		metaParts = append(metaParts, fmt.Sprintf("type: %s", evt.Info.Type))
	}
	if evt.Info.Category != "" {
		metaParts = append(metaParts, fmt.Sprintf("category: %s", evt.Info.Category))
	}
	if evt.IsViewOnce {
		metaParts = append(metaParts, "view once")
	}
	return metaParts
}

func handleIncomingMediaDownload(ctx context.Context, evt *events.Message, chatStorageRepo domainChatStorage.IChatStorageRepository, client *whatsmeow.Client) {
	if !config.WhatsappAutoDownloadMedia {
		return
	}
	if client == nil || evt == nil || chatStorageRepo == nil {
		return
	}

	mediaFile, mediaType := incomingDownloadableMedia(utils.UnwrapMessage(evt.Message))
	if mediaFile == nil {
		return
	}

	chatJID := normalizedEventChatJID(ctx, evt, client)
	policy := mediaPolicyForIncomingChat(chatStorageRepo, client, chatJID)

	downloadDir := incomingMediaDownloadDir(ctx, evt, client)
	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		log.Errorf("Failed to create incoming media directory %s for message %s: %v", downloadDir, evt.Info.ID, err)
		return
	}

	extracted, err := extractIncomingMediaFunc(ctx, client, downloadDir, mediaFile)
	if err != nil {
		log.Errorf("Failed to auto-download %s media for message %s: %v", mediaType, evt.Info.ID, err)
		return
	}
	if strings.TrimSpace(extracted.MediaPath) == "" {
		log.Warnf("Auto-download for %s media message %s returned an empty media path", mediaType, evt.Info.ID)
		return
	}

	stored, err := chatStorageRepo.GetMessageByID(evt.Info.ID)
	if err != nil {
		log.Errorf("Failed to lookup media message %s after auto-download: %v", evt.Info.ID, err)
		return
	}
	if stored == nil {
		log.Warnf("Media message %s was auto-downloaded to %s but was not found in storage", evt.Info.ID, extracted.MediaPath)
		return
	}

	stored.LocalMediaPath = extracted.MediaPath
	if err := chatStorageRepo.StoreMessage(stored); err != nil {
		log.Errorf("Failed to persist local media path for message %s: %v", evt.Info.ID, err)
		return
	}

	if policy != nil && policy.Mode == domainChatStorage.ChatMediaPolicyModeEphemeral && isEphemeralPolicyMediaType(mediaType) {
		log.Infof("Auto-downloaded %s media for message %s to %s with %d-day ephemeral retention", mediaType, evt.Info.ID, extracted.MediaPath, policy.RetentionDays)
	} else {
		log.Infof("Auto-downloaded %s media for message %s to %s", mediaType, evt.Info.ID, extracted.MediaPath)
	}
}

func incomingDownloadableMedia(msg *waE2E.Message) (whatsmeow.DownloadableMessage, string) {
	if msg == nil {
		return nil, ""
	}
	if image := msg.GetImageMessage(); image != nil {
		return image, "image"
	}
	if video := msg.GetVideoMessage(); video != nil {
		return video, "video"
	}
	if ptv := msg.GetPtvMessage(); ptv != nil {
		return ptv, "video_note"
	}
	if audio := msg.GetAudioMessage(); audio != nil {
		return audio, "audio"
	}
	if document := msg.GetDocumentMessage(); document != nil {
		return document, "document"
	}
	if sticker := msg.GetStickerMessage(); sticker != nil {
		return sticker, "sticker"
	}
	return nil, ""
}

func incomingMediaDownloadDir(ctx context.Context, evt *events.Message, client *whatsmeow.Client) string {
	chatJID := normalizedEventChatJID(ctx, evt, client)

	chatDirName := utils.ExtractPhoneNumber(chatJID.String())
	if strings.TrimSpace(chatDirName) == "" {
		chatDirName = "unknown_chat"
	}

	timestamp := evt.Info.Timestamp
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	return filepath.Join(config.PathMedia, chatDirName, timestamp.Format("2006-01-02"))
}

func normalizedEventChatJID(ctx context.Context, evt *events.Message, client *whatsmeow.Client) types.JID {
	chatJID := evt.Info.Chat.ToNonAD()
	if client != nil {
		chatJID = NormalizeJIDFromLID(ctx, chatJID, client).ToNonAD()
	}
	return chatJID
}

func mediaPolicyForIncomingChat(chatStorageRepo domainChatStorage.IChatStorageRepository, client *whatsmeow.Client, chatJID types.JID) *domainChatStorage.ChatMediaPolicy {
	if chatStorageRepo == nil || client == nil || client.Store == nil || client.Store.ID == nil || chatJID.IsEmpty() {
		return nil
	}
	deviceID := client.Store.ID.ToNonAD().String()
	policy, err := chatStorageRepo.GetChatMediaPolicyByDevice(deviceID, chatJID.String())
	if err != nil {
		logrus.WithError(err).Warnf("Failed to lookup media policy for chat %s", chatJID.String())
		return nil
	}
	return policy
}

func isEphemeralPolicyMediaType(mediaType string) bool {
	switch mediaType {
	case "image", "video", "video_note":
		return true
	default:
		return false
	}
}

func handleAutoMarkRead(ctx context.Context, evt *events.Message, client *whatsmeow.Client) {
	// Only mark read if auto-mark read is enabled and message is incoming
	if !config.WhatsappAutoMarkRead || evt.Info.IsFromMe {
		return
	}

	if client == nil {
		return
	}

	// Mark the message as read
	messageIDs := []types.MessageID{evt.Info.ID}
	timestamp := time.Now()
	chat := evt.Info.Chat
	sender := evt.Info.Sender

	if err := client.MarkRead(ctx, messageIDs, timestamp, chat, sender); err != nil {
		log.Warnf("Failed to mark message %s as read: %v", evt.Info.ID, err)
	} else {
		log.Debugf("Marked message %s as read", evt.Info.ID)
	}
}

func materializeSecretEditMessage(ctx context.Context, evt *events.Message, client *whatsmeow.Client) *events.Message {
	if evt == nil || evt.Message == nil || client == nil {
		return evt
	}
	msg := utils.UnwrapMessage(evt.Message)
	secret := msg.GetSecretEncryptedMessage()
	if secret == nil || secret.GetSecretEncType() != waE2E.SecretEncryptedMessage_MESSAGE_EDIT {
		return evt
	}
	decrypted, err := client.DecryptSecretEncryptedMessage(ctx, evt)
	if err != nil {
		targetID := ""
		if key := secret.GetTargetMessageKey(); key != nil {
			targetID = key.GetID()
		}
		log.Warnf("Failed to decrypt SecretEncryptedMessage(MESSAGE_EDIT) for %s (target=%s): %v", evt.Info.ID, targetID, err)
		return evt
	}
	if decrypted == nil {
		return evt
	}
	cloned := *evt
	cloned.Message = decrypted
	return &cloned
}

func handleWebhookForward(ctx context.Context, evt *events.Message, client *whatsmeow.Client, repositories ...domainChatStorage.IChatStorageRepository) {
	// Skip webhook for protocol messages that are internal sync messages
	if protocolMessage := evt.Message.GetProtocolMessage(); protocolMessage != nil {
		protocolType := protocolMessage.GetType().String()
		// Only allow REVOKE and MESSAGE_EDIT through - skip all other protocol messages
		// (HISTORY_SYNC_NOTIFICATION, APP_STATE_SYNC_KEY_SHARE, EPHEMERAL_SYNC_RESPONSE, etc.)
		switch protocolType {
		case "REVOKE", "MESSAGE_EDIT":
			// These are meaningful user actions, allow webhook
		default:
			log.Debugf("Skipping webhook for protocol message type: %s", protocolType)
			return
		}
	}

	if strings.Contains(evt.Info.SourceString(), "broadcast") {
		return
	}

	var chatStorageRepo domainChatStorage.IChatStorageRepository
	if len(repositories) > 0 {
		chatStorageRepo = repositories[0]
	}

	go func(e *events.Message, c *whatsmeow.Client) {
		webhookCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := forwardMessageToWebhook(webhookCtx, c, e, chatStorageRepo); err != nil {
			logrus.Error("Failed forward to webhook: ", err)
		}
	}(evt, client)
}
