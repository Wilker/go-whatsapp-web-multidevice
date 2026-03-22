package usecase

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/proto/waMmsRetry"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const (
	mediaRetryTimeoutAggressive  = 45 * time.Second
	mediaRetryAttemptsAggressive = 2
	mediaRetryBatchWindow        = 90 * time.Second
)

var (
	sendMediaRetryReceiptFunc = func(ctx context.Context, client *whatsmeow.Client, message *types.MessageInfo, mediaKey []byte) error {
		return client.SendMediaRetryReceipt(ctx, message, mediaKey)
	}
	decryptMediaRetryNotificationFunc = whatsmeow.DecryptMediaRetryNotification
	downloadStoredMessageMediaFunc    = downloadStoredMessageMedia
)

type serviceMessage struct {
	chatStorageRepo domainChatStorage.IChatStorageRepository
}

type mediaDownloadResult struct {
	extractedMedia    utils.ExtractedMedia
	recoveryMethod    string
	failureReason     string
	updatedDirectPath string
}

type mediaDownloadProfile struct {
	allowMediaRetry bool
	retryAttempts   int
	retryTimeout    time.Duration
}

type mediaRetryOutcome struct {
	messageID         string
	recoveryMethod    string
	failureReason     string
	updatedDirectPath string
	err               error
}

func NewMessageService(chatStorageRepo domainChatStorage.IChatStorageRepository) domainMessage.IMessageUsecase {
	return &serviceMessage{
		chatStorageRepo: chatStorageRepo,
	}
}

func (service serviceMessage) MarkAsRead(ctx context.Context, request domainMessage.MarkAsReadRequest) (response domainMessage.GenericResponse, err error) {
	if err = validations.ValidateMarkAsRead(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return response, err
	}

	ids := []types.MessageID{request.MessageID}
	if err = client.MarkRead(ctx, ids, time.Now(), dataWaRecipient, *client.Store.ID); err != nil {
		return response, err
	}

	logrus.Info(map[string]any{
		"phone":      request.Phone,
		"message_id": request.MessageID,
		"chat":       dataWaRecipient.String(),
		"sender":     client.Store.ID.String(),
	})

	response.MessageID = request.MessageID
	response.Status = fmt.Sprintf("Mark as read success %s", request.MessageID)
	return response, nil
}

func (service serviceMessage) ReactMessage(ctx context.Context, request domainMessage.ReactionRequest) (response domainMessage.GenericResponse, err error) {
	if err = validations.ValidateReactMessage(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return response, err
	}

	// FromMe in reaction refers to whether the ORIGINAL message (being reacted to) was sent by us
	isFromMe := true
	message, err := service.chatStorageRepo.GetMessageByID(request.MessageID)
	if err != nil {
		logrus.Warnf("Failed to lookup message %s for reaction: %v, using fallback heuristic", request.MessageID, err)
		isFromMe = len(request.MessageID) <= 22
	} else if message != nil {
		isFromMe = message.IsFromMe
	} else {
		logrus.Debugf("Message %s not found in database, using ID length heuristic for FromMe", request.MessageID)
		isFromMe = len(request.MessageID) <= 22
	}

	msg := &waE2E.Message{
		ReactionMessage: &waE2E.ReactionMessage{
			Key: &waCommon.MessageKey{
				FromMe:    proto.Bool(isFromMe),
				ID:        proto.String(request.MessageID),
				RemoteJID: proto.String(dataWaRecipient.String()),
			},
			Text:              proto.String(request.Emoji),
			SenderTimestampMS: proto.Int64(time.Now().UnixMilli()),
		},
	}
	ts, err := client.SendMessage(ctx, dataWaRecipient, msg)
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Reaction sent to %s (server timestamp: %s)", request.Phone, ts.Timestamp)
	return response, nil
}

func (service serviceMessage) RevokeMessage(ctx context.Context, request domainMessage.RevokeRequest) (response domainMessage.GenericResponse, err error) {
	if err = validations.ValidateRevokeMessage(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return response, err
	}

	ts, err := client.SendMessage(ctx, dataWaRecipient, client.BuildRevoke(dataWaRecipient, types.EmptyJID, request.MessageID))
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Revoke success %s (server timestamp: %s)", request.Phone, ts.Timestamp)
	return response, nil
}

func (service serviceMessage) DeleteMessage(ctx context.Context, request domainMessage.DeleteRequest) (err error) {
	if err = validations.ValidateDeleteMessage(ctx, request); err != nil {
		return err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pkgError.ErrWaCLI
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return err
	}

	isFromMe := "1"
	if len(request.MessageID) > 22 {
		isFromMe = "0"
	}

	patchInfo := appstate.PatchInfo{
		Timestamp: time.Now(),
		Type:      appstate.WAPatchRegularHigh,
		Mutations: []appstate.MutationInfo{{
			Index: []string{appstate.IndexDeleteMessageForMe, dataWaRecipient.String(), request.MessageID, isFromMe, client.Store.ID.String()},
			Value: &waSyncAction.SyncActionValue{
				DeleteMessageForMeAction: &waSyncAction.DeleteMessageForMeAction{
					DeleteMedia:      proto.Bool(true),
					MessageTimestamp: proto.Int64(time.Now().UnixMilli()),
				},
			},
		}},
	}

	if err = client.SendAppState(ctx, patchInfo); err != nil {
		return err
	}
	return nil
}

func (service serviceMessage) UpdateMessage(ctx context.Context, request domainMessage.UpdateMessageRequest) (response domainMessage.GenericResponse, err error) {
	if err = validations.ValidateUpdateMessage(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return response, err
	}

	msg := &waE2E.Message{Conversation: proto.String(request.Message)}
	ts, err := client.SendMessage(ctx, dataWaRecipient, client.BuildEdit(dataWaRecipient, request.MessageID, msg))
	if err != nil {
		return response, err
	}

	response.MessageID = ts.ID
	response.Status = fmt.Sprintf("Update message success %s (server timestamp: %s)", request.Phone, ts.Timestamp)
	return response, nil
}

// StarMessage implements message.IMessageService.
func (service serviceMessage) StarMessage(ctx context.Context, request domainMessage.StarRequest) (err error) {
	if err = validations.ValidateStarMessage(ctx, request); err != nil {
		return err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pkgError.ErrWaCLI
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return err
	}

	isFromMe := true
	if len(request.MessageID) > 22 {
		isFromMe = false
	}

	patchInfo := appstate.BuildStar(dataWaRecipient.ToNonAD(), *client.Store.ID, request.MessageID, isFromMe, request.IsStarred)

	if err = client.SendAppState(ctx, patchInfo); err != nil {
		return err
	}
	return nil
}

// DownloadMedia implements message.IMessageService.
func (service serviceMessage) DownloadMedia(ctx context.Context, request domainMessage.DownloadMediaRequest) (response domainMessage.DownloadMediaResponse, err error) {
	return service.downloadMediaWithProfile(ctx, request, mediaDownloadProfile{
		allowMediaRetry: true,
		retryAttempts:   mediaRetryAttemptsAggressive,
		retryTimeout:    mediaRetryTimeoutAggressive,
	})
}

func (service serviceMessage) DownloadMediaForExport(ctx context.Context, request domainMessage.DownloadMediaRequest) (response domainMessage.DownloadMediaResponse, err error) {
	return service.downloadMediaWithProfile(ctx, request, mediaDownloadProfile{})
}

func (service serviceMessage) downloadMediaWithProfile(
	ctx context.Context,
	request domainMessage.DownloadMediaRequest,
	profile mediaDownloadProfile,
) (response domainMessage.DownloadMediaResponse, err error) {
	if err = validations.ValidateDownloadMedia(ctx, request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return response, err
	}

	// Query the message from chat storage with device scoping when available.
	message, err := service.getStoredMessageForRequest(ctx, request.MessageID)
	if err != nil {
		return response, fmt.Errorf("message not found: %v", err)
	}

	if message == nil {
		return response, fmt.Errorf("message with ID %s not found", request.MessageID)
	}

	response.MessageID = request.MessageID
	response.MediaType = message.MediaType
	response.Filename = message.Filename
	response.RecoveryMethod = domainMessage.MediaRecoveryMethodNone

	// Check if message has media
	if strings.TrimSpace(message.MediaType) == "" {
		response.FailureReason = domainMessage.MediaFailureReasonNoMediaMetadata
		return response, fmt.Errorf("message %s does not contain downloadable media", request.MessageID)
	}

	// Verify the message is from the specified chat
	if message.ChatJID != dataWaRecipient.String() {
		return response, fmt.Errorf("message %s does not belong to chat %s", request.MessageID, dataWaRecipient.String())
	}

	baseDir, dateDir, err := resolveMediaDownloadDir(request.OutputDir, message)
	if err != nil {
		return response, err
	}
	response.OutputDirUsed = baseDir

	err = os.MkdirAll(dateDir, 0755)
	if err != nil {
		return response, fmt.Errorf("failed to create directory: %v", err)
	}

	downloadResult, err := service.downloadMessageMediaWithRecovery(ctx, client, message, dateDir, profile)
	if err != nil {
		response.RecoveryMethod = downloadResult.recoveryMethod
		response.FailureReason = downloadResult.failureReason
		return response, err
	}

	if strings.TrimSpace(downloadResult.updatedDirectPath) != "" && downloadResult.updatedDirectPath != strings.TrimSpace(message.DirectPath) {
		message.DirectPath = downloadResult.updatedDirectPath
		if storeErr := service.chatStorageRepo.StoreMessage(message); storeErr != nil {
			logrus.WithError(storeErr).Warnf("Failed to persist recovered direct path for message %s", message.ID)
		}
	}

	// Get file size
	fileInfo, err := os.Stat(downloadResult.extractedMedia.MediaPath)
	if err != nil {
		logrus.Warnf("Could not get file size for %s: %v", downloadResult.extractedMedia.MediaPath, err)
	}

	// Build response
	response.Status = fmt.Sprintf(
		"Media downloaded successfully to %s (recovery_method=%s)",
		downloadResult.extractedMedia.MediaPath,
		downloadResult.recoveryMethod,
	)
	response.MediaType = message.MediaType
	response.Filename = filepath.Base(downloadResult.extractedMedia.MediaPath)
	response.FilePath = downloadResult.extractedMedia.MediaPath
	response.OutputDirUsed = baseDir
	response.RecoveryMethod = downloadResult.recoveryMethod
	response.FailureReason = domainMessage.MediaFailureReasonNone
	if fileInfo != nil {
		response.FileSize = fileInfo.Size()
	}

	logrus.Info(map[string]any{
		"message_id": request.MessageID,
		"phone":      request.Phone,
		"chat":       dataWaRecipient.String(),
		"media_type": response.MediaType,
		"file_path":  response.FilePath,
		"file_size":  response.FileSize,
		"recovery":   response.RecoveryMethod,
	})

	return response, nil
}

func (service serviceMessage) RecoverMediaBatch(
	ctx context.Context,
	request domainMessage.RecoverMediaBatchRequest,
) (response domainMessage.RecoverMediaBatchResponse, err error) {
	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	instance, ok := whatsapp.DeviceFromContext(ctx)
	if !ok || instance == nil {
		return response, fmt.Errorf("media retry batch requires a device-scoped context")
	}

	dataWaRecipient, err := utils.ValidateJidWithLogin(client, request.Phone)
	if err != nil {
		return response, err
	}

	messageIDs := uniqueOrderedMessageIDs(request.MessageIDs)
	if len(messageIDs) == 0 {
		response.Items = []domainMessage.RecoverMediaBatchItem{}
		return response, nil
	}

	type pendingBatchRetry struct {
		message *domainChatStorage.Message
		retryCh <-chan *events.MediaRetry
		cleanup func()
	}

	itemsByID := make(map[string]domainMessage.RecoverMediaBatchItem, len(messageIDs))
	pending := make(map[string]pendingBatchRetry, len(messageIDs))
	outcomes := make(chan mediaRetryOutcome, len(messageIDs))
	windowCtx, cancel := context.WithTimeout(ctx, mediaRetryBatchWindow)
	defer cancel()

	for _, messageID := range messageIDs {
		item := domainMessage.RecoverMediaBatchItem{
			MessageID:      messageID,
			RecoveryMethod: domainMessage.MediaRecoveryMethodNone,
		}

		message, getErr := service.getStoredMessageForRequest(ctx, messageID)
		if getErr != nil {
			item.FailureReason = domainMessage.MediaFailureReasonDownloadFailed
			itemsByID[messageID] = item
			continue
		}
		if message == nil {
			item.FailureReason = domainMessage.MediaFailureReasonDownloadFailed
			itemsByID[messageID] = item
			continue
		}
		if message.ChatJID != dataWaRecipient.String() {
			item.FailureReason = domainMessage.MediaFailureReasonDownloadFailed
			itemsByID[messageID] = item
			continue
		}
		if !hasDownloadableMediaMetadata(message) {
			item.FailureReason = domainMessage.MediaFailureReasonNoMediaMetadata
			itemsByID[messageID] = item
			continue
		}

		messageInfo, buildErr := buildStoredMediaRetryMessageInfo(ctx, client, message)
		if buildErr != nil {
			item.FailureReason = domainMessage.MediaFailureReasonDownloadFailed
			itemsByID[messageID] = item
			continue
		}

		retryCh, cleanup, registerErr := instance.RegisterPendingMediaRetry(message.ID)
		if registerErr != nil {
			item.FailureReason = domainMessage.MediaFailureReasonDownloadFailed
			itemsByID[messageID] = item
			continue
		}

		if sendErr := sendMediaRetryReceiptFunc(windowCtx, client, &messageInfo, message.MediaKey); sendErr != nil {
			cleanup()
			item.FailureReason = domainMessage.MediaFailureReasonDownloadFailed
			itemsByID[messageID] = item
			continue
		}

		itemsByID[messageID] = item
		pending[messageID] = pendingBatchRetry{
			message: message,
			retryCh: retryCh,
			cleanup: cleanup,
		}
	}

	for messageID, entry := range pending {
		go func(id string, pendingEntry pendingBatchRetry) {
			defer pendingEntry.cleanup()

			select {
			case evt := <-pendingEntry.retryCh:
				outcomes <- service.decodeMediaRetryOutcome(pendingEntry.message, evt, true)
			case <-windowCtx.Done():
				outcomes <- mediaRetryOutcome{
					messageID:      id,
					recoveryMethod: domainMessage.MediaRecoveryMethodMediaRetry,
					failureReason:  domainMessage.MediaFailureReasonRetryTimeout,
					err:            fmt.Errorf("timed out waiting for media retry notification for message %s", id),
				}
			}
		}(messageID, entry)
	}

	for remaining := len(pending); remaining > 0; remaining-- {
		outcome := <-outcomes
		item := itemsByID[outcome.messageID]
		item.RecoveryMethod = outcome.recoveryMethod
		item.FailureReason = outcome.failureReason
		item.UpdatedDirectPath = outcome.updatedDirectPath
		itemsByID[outcome.messageID] = item
	}

	response.Items = make([]domainMessage.RecoverMediaBatchItem, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		response.Items = append(response.Items, itemsByID[messageID])
	}

	return response, nil
}

func (service serviceMessage) downloadMessageMediaWithRecovery(
	ctx context.Context,
	client *whatsmeow.Client,
	message *domainChatStorage.Message,
	storageLocation string,
	profile mediaDownloadProfile,
) (result mediaDownloadResult, err error) {
	if !hasDownloadableMediaMetadata(message) {
		result.recoveryMethod = domainMessage.MediaRecoveryMethodNone
		result.failureReason = domainMessage.MediaFailureReasonNoMediaMetadata
		return result, fmt.Errorf("message %s does not have enough media metadata to download", message.ID)
	}

	if hasUsableStoredMediaURL(message) {
		result.extractedMedia, err = downloadStoredMessageMediaFunc(ctx, client, storageLocation, message, strings.TrimSpace(message.URL), "")
		if err == nil {
			result.recoveryMethod = domainMessage.MediaRecoveryMethodDirectURL
			return result, nil
		}
		if !isRetryableMediaDownloadError(err) {
			result.recoveryMethod = domainMessage.MediaRecoveryMethodDirectURL
			result.failureReason = domainMessage.MediaFailureReasonDownloadFailed
			return result, fmt.Errorf("failed to download media for message %s using URL: %w", message.ID, err)
		}
	}

	if hasStoredDirectPath(message) {
		result.extractedMedia, err = downloadStoredMessageMediaFunc(ctx, client, storageLocation, message, "", strings.TrimSpace(message.DirectPath))
		if err == nil {
			result.recoveryMethod = domainMessage.MediaRecoveryMethodStoredDirectPath
			result.updatedDirectPath = strings.TrimSpace(message.DirectPath)
			return result, nil
		}
		if !isRetryableMediaDownloadError(err) {
			result.recoveryMethod = domainMessage.MediaRecoveryMethodStoredDirectPath
			result.failureReason = domainMessage.MediaFailureReasonDownloadFailed
			return result, fmt.Errorf("failed to download media for message %s using stored direct path: %w", message.ID, err)
		}
	}

	if !profile.allowMediaRetry || profile.retryAttempts <= 0 || profile.retryTimeout <= 0 {
		result.recoveryMethod = domainMessage.MediaRecoveryMethodNone
		result.failureReason = domainMessage.MediaFailureReasonDownloadFailed
		return result, fmt.Errorf("failed to download media for message %s with stored URL/direct path metadata", message.ID)
	}

	return service.downloadMessageMediaViaRetry(ctx, client, message, storageLocation, profile)
}

func (service serviceMessage) downloadMessageMediaViaRetry(
	ctx context.Context,
	client *whatsmeow.Client,
	message *domainChatStorage.Message,
	storageLocation string,
	profile mediaDownloadProfile,
) (result mediaDownloadResult, err error) {
	result.recoveryMethod = domainMessage.MediaRecoveryMethodMediaRetry

	instance, ok := whatsapp.DeviceFromContext(ctx)
	if !ok || instance == nil {
		result.failureReason = domainMessage.MediaFailureReasonDownloadFailed
		return result, fmt.Errorf("media retry for message %s requires a device-scoped context", message.ID)
	}

	messageInfo, err := buildStoredMediaRetryMessageInfo(ctx, client, message)
	if err != nil {
		result.failureReason = domainMessage.MediaFailureReasonDownloadFailed
		return result, err
	}

	attempts := profile.retryAttempts
	for attempt := 1; attempt <= attempts; attempt++ {
		retryCh, cleanup, registerErr := instance.RegisterPendingMediaRetry(message.ID)
		if registerErr != nil {
			result.failureReason = domainMessage.MediaFailureReasonDownloadFailed
			return result, registerErr
		}

		sendErr := sendMediaRetryReceiptFunc(ctx, client, &messageInfo, message.MediaKey)
		if sendErr != nil {
			cleanup()
			result.failureReason = domainMessage.MediaFailureReasonDownloadFailed
			return result, fmt.Errorf("failed to request media retry for message %s: %w", message.ID, sendErr)
		}

		attemptCtx, cancel := context.WithTimeout(ctx, profile.retryTimeout)
		var outcome mediaRetryOutcome
		select {
		case evt := <-retryCh:
			outcome = service.decodeMediaRetryOutcome(message, evt, false)
		case <-attemptCtx.Done():
			outcome = mediaRetryOutcome{
				messageID:      message.ID,
				recoveryMethod: domainMessage.MediaRecoveryMethodMediaRetry,
				failureReason:  domainMessage.MediaFailureReasonRetryTimeout,
				err:            fmt.Errorf("timed out waiting for media retry notification for message %s", message.ID),
			}
		}
		cancel()
		cleanup()

		if outcome.updatedDirectPath != "" {
			result.updatedDirectPath = outcome.updatedDirectPath
			message.DirectPath = outcome.updatedDirectPath
			if persistErr := service.persistRecoveredDirectPath(message, outcome.updatedDirectPath); persistErr != nil {
				result.failureReason = domainMessage.MediaFailureReasonDownloadFailed
				return result, persistErr
			}

			result.extractedMedia, err = downloadStoredMessageMediaFunc(ctx, client, storageLocation, message, "", outcome.updatedDirectPath)
			if err == nil {
				result.recoveryMethod = domainMessage.MediaRecoveryMethodMediaRetry
				result.failureReason = domainMessage.MediaFailureReasonNone
				return result, nil
			}
			if !isRetryableMediaDownloadError(err) {
				result.failureReason = domainMessage.MediaFailureReasonDownloadFailed
				return result, fmt.Errorf("failed to download media for message %s using retried direct path: %w", message.ID, err)
			}
		}

		if outcome.failureReason == domainMessage.MediaFailureReasonNotAvailableOnPhone {
			result.failureReason = outcome.failureReason
			return result, outcome.err
		}

		if attempt == attempts {
			result.failureReason = outcome.failureReason
			if result.failureReason == "" {
				result.failureReason = domainMessage.MediaFailureReasonDownloadFailed
			}
			if outcome.err != nil {
				return result, outcome.err
			}
			return result, fmt.Errorf("media retry for message %s failed without additional details", message.ID)
		}
	}

	result.failureReason = domainMessage.MediaFailureReasonDownloadFailed
	return result, fmt.Errorf("media retry for message %s exhausted all attempts", message.ID)
}

func (service serviceMessage) decodeMediaRetryOutcome(
	message *domainChatStorage.Message,
	evt *events.MediaRetry,
	persistDirectPath bool,
) mediaRetryOutcome {
	outcome := mediaRetryOutcome{
		messageID:      message.ID,
		recoveryMethod: domainMessage.MediaRecoveryMethodMediaRetry,
	}

	retryData, err := decryptMediaRetryNotificationFunc(evt, message.MediaKey)
	if err != nil {
		if errors.Is(err, whatsmeow.ErrMediaNotAvailableOnPhone) {
			outcome.failureReason = domainMessage.MediaFailureReasonNotAvailableOnPhone
		} else {
			outcome.failureReason = domainMessage.MediaFailureReasonDownloadFailed
		}
		outcome.err = fmt.Errorf("failed to decrypt media retry notification for message %s: %w", message.ID, err)
		return outcome
	}

	switch retryData.GetResult() {
	case waMmsRetry.MediaRetryNotification_SUCCESS:
	case waMmsRetry.MediaRetryNotification_NOT_FOUND:
		outcome.failureReason = domainMessage.MediaFailureReasonNotAvailableOnPhone
		outcome.err = fmt.Errorf("media for message %s is no longer available on phone", message.ID)
		return outcome
	default:
		outcome.failureReason = domainMessage.MediaFailureReasonDownloadFailed
		outcome.err = fmt.Errorf("media retry for message %s returned %s", message.ID, retryData.GetResult().String())
		return outcome
	}

	directPath := strings.TrimSpace(retryData.GetDirectPath())
	if directPath == "" {
		outcome.failureReason = domainMessage.MediaFailureReasonDownloadFailed
		outcome.err = fmt.Errorf("media retry for message %s returned an empty direct path", message.ID)
		return outcome
	}

	outcome.updatedDirectPath = directPath
	if persistDirectPath {
		if persistErr := service.persistRecoveredDirectPath(message, directPath); persistErr != nil {
			outcome.failureReason = domainMessage.MediaFailureReasonDownloadFailed
			outcome.err = persistErr
			outcome.updatedDirectPath = ""
			return outcome
		}
	}
	return outcome
}

func (service serviceMessage) persistRecoveredDirectPath(message *domainChatStorage.Message, directPath string) error {
	if message == nil {
		return fmt.Errorf("message is required to persist direct path")
	}

	trimmed := strings.TrimSpace(directPath)
	if trimmed == "" {
		return nil
	}

	if trimmed == strings.TrimSpace(message.DirectPath) {
		return nil
	}

	message.DirectPath = trimmed
	if err := service.chatStorageRepo.StoreMessage(message); err != nil {
		logrus.WithError(err).Warnf("Failed to persist recovered direct path for message %s", message.ID)
		return fmt.Errorf("failed to persist recovered direct path for message %s: %w", message.ID, err)
	}
	return nil
}

func downloadStoredMessageMedia(
	ctx context.Context,
	client *whatsmeow.Client,
	storageLocation string,
	message *domainChatStorage.Message,
	url string,
	directPath string,
) (utils.ExtractedMedia, error) {
	downloadableMsg, err := buildStoredDownloadableMessage(message, url, directPath)
	if err != nil {
		return utils.ExtractedMedia{}, err
	}

	return utils.ExtractMedia(ctx, client, storageLocation, downloadableMsg)
}

func buildStoredDownloadableMessage(message *domainChatStorage.Message, url string, directPath string) (whatsmeow.DownloadableMessage, error) {
	trimmedURL := strings.TrimSpace(url)
	trimmedDirectPath := strings.TrimSpace(directPath)
	fileName := strings.TrimSpace(message.Filename)

	switch strings.ToLower(strings.TrimSpace(message.MediaType)) {
	case "image":
		return &waE2E.ImageMessage{
			URL:           proto.String(trimmedURL),
			DirectPath:    proto.String(trimmedDirectPath),
			MediaKey:      message.MediaKey,
			FileSHA256:    message.FileSHA256,
			FileEncSHA256: message.FileEncSHA256,
			FileLength:    proto.Uint64(message.FileLength),
		}, nil
	case "video", "video_note":
		return &waE2E.VideoMessage{
			URL:           proto.String(trimmedURL),
			DirectPath:    proto.String(trimmedDirectPath),
			MediaKey:      message.MediaKey,
			FileSHA256:    message.FileSHA256,
			FileEncSHA256: message.FileEncSHA256,
			FileLength:    proto.Uint64(message.FileLength),
		}, nil
	case "audio", "ptt":
		return &waE2E.AudioMessage{
			URL:           proto.String(trimmedURL),
			DirectPath:    proto.String(trimmedDirectPath),
			MediaKey:      message.MediaKey,
			FileSHA256:    message.FileSHA256,
			FileEncSHA256: message.FileEncSHA256,
			FileLength:    proto.Uint64(message.FileLength),
		}, nil
	case "document":
		return &waE2E.DocumentMessage{
			URL:           proto.String(trimmedURL),
			DirectPath:    proto.String(trimmedDirectPath),
			MediaKey:      message.MediaKey,
			FileSHA256:    message.FileSHA256,
			FileEncSHA256: message.FileEncSHA256,
			FileLength:    proto.Uint64(message.FileLength),
			FileName:      proto.String(fileName),
		}, nil
	case "sticker":
		return &waE2E.StickerMessage{
			URL:           proto.String(trimmedURL),
			DirectPath:    proto.String(trimmedDirectPath),
			MediaKey:      message.MediaKey,
			FileSHA256:    message.FileSHA256,
			FileEncSHA256: message.FileEncSHA256,
			FileLength:    proto.Uint64(message.FileLength),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported media type: %s", message.MediaType)
	}
}

func buildStoredMediaRetryMessageInfo(ctx context.Context, client *whatsmeow.Client, message *domainChatStorage.Message) (types.MessageInfo, error) {
	chatJID := utils.FormatJID(message.ChatJID)
	if chatJID.IsEmpty() {
		return types.MessageInfo{}, fmt.Errorf("invalid chat JID %q for message %s", message.ChatJID, message.ID)
	}
	chatJID = whatsapp.NormalizeJIDFromLID(ctx, chatJID, client).ToNonAD()

	senderJID := utils.FormatJID(message.Sender)
	if !senderJID.IsEmpty() {
		senderJID = whatsapp.NormalizeJIDFromLID(ctx, senderJID, client).ToNonAD()
	}
	if senderJID.IsEmpty() {
		switch {
		case message.IsFromMe && client != nil && client.Store != nil && client.Store.ID != nil:
			senderJID = client.Store.ID.ToNonAD()
		case !utils.IsGroupJID(message.ChatJID):
			senderJID = chatJID
		}
	}

	return types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:     chatJID,
			Sender:   senderJID,
			IsFromMe: message.IsFromMe,
			IsGroup:  utils.IsGroupJID(message.ChatJID),
		},
		ID:        types.MessageID(message.ID),
		Timestamp: message.Timestamp,
		MediaType: message.MediaType,
	}, nil
}

func hasDownloadableMediaMetadata(message *domainChatStorage.Message) bool {
	if message == nil {
		return false
	}
	if strings.TrimSpace(message.MediaType) == "" {
		return false
	}
	return len(message.MediaKey) > 0 && len(message.FileSHA256) > 0 && len(message.FileEncSHA256) > 0
}

func hasUsableStoredMediaURL(message *domainChatStorage.Message) bool {
	if message == nil {
		return false
	}
	url := strings.TrimSpace(message.URL)
	return url != "" && !strings.HasPrefix(url, "https://web.whatsapp.net")
}

func hasStoredDirectPath(message *domainChatStorage.Message) bool {
	if message == nil {
		return false
	}
	return strings.TrimSpace(message.DirectPath) != ""
}

func resolveMediaDownloadDir(outputDir string, message *domainChatStorage.Message) (baseDir string, dateDir string, err error) {
	baseDir, err = utils.ResolveBaseOutputDir(outputDir, config.PathMedia)
	if err != nil {
		return "", "", err
	}

	chatDir := filepath.Join(baseDir, utils.ExtractPhoneNumber(message.ChatJID))
	dateDir = filepath.Join(chatDir, message.Timestamp.Format("2006-01-02"))
	return baseDir, dateDir, nil
}

func (service serviceMessage) getStoredMessageForRequest(ctx context.Context, messageID string) (*domainChatStorage.Message, error) {
	if instance, ok := whatsapp.DeviceFromContext(ctx); ok && instance != nil {
		deviceID := strings.TrimSpace(instance.JID())
		if deviceID == "" {
			client := instance.GetClient()
			if client != nil && client.Store != nil && client.Store.ID != nil {
				deviceID = client.Store.ID.ToNonAD().String()
			}
		}
		if deviceID != "" {
			return service.chatStorageRepo.GetMessageByIDByDevice(deviceID, messageID)
		}
	}

	return service.chatStorageRepo.GetMessageByID(messageID)
}

func isRetryableMediaDownloadError(err error) bool {
	return errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith404) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith403) ||
		errors.Is(err, whatsmeow.ErrMediaDownloadFailedWith410) ||
		errors.Is(err, whatsmeow.ErrNoURLPresent)
}

func uniqueOrderedMessageIDs(messageIDs []string) []string {
	if len(messageIDs) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(messageIDs))
	result := make([]string, 0, len(messageIDs))
	for _, messageID := range messageIDs {
		trimmed := strings.TrimSpace(messageID)
		if trimmed == "" {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
