package utils

import (
	"errors"
	"fmt"
	"mime"
	"net/url"
	"path/filepath"
	"strings"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

// ResolveMediaDirectPath returns storedDirectPath, or derives one from legacy media URLs.
func ResolveMediaDirectPath(storedDirectPath, mediaURL string) string {
	storedDirectPath = strings.TrimSpace(storedDirectPath)
	if storedDirectPath != "" {
		return storedDirectPath
	}

	mediaURL = strings.TrimSpace(mediaURL)
	if mediaURL == "" {
		return ""
	}

	parsed, err := url.Parse(mediaURL)
	if err != nil {
		return ""
	}
	requestURI := parsed.RequestURI()
	if strings.HasPrefix(requestURI, "/") {
		return requestURI
	}
	return ""
}

const ErrUnsupportedForwardType = "unsupported message type for forward"

type ForwardBuildOptions struct {
	Duration *int
	Upload   *whatsmeow.UploadResponse
	MimeType string
}

var forwardableMediaTypes = map[string]struct{}{
	"image":      {},
	"video":      {},
	"video_note": {},
	"audio":      {},
	"ptt":        {},
	"document":   {},
	"sticker":    {},
}

func IsForwardableStorageMessage(message *domainChatStorage.Message) bool {
	if message == nil || message.MediaType == "call" {
		return false
	}
	if _, ok := forwardableMediaTypes[message.MediaType]; ok {
		return true
	}
	if message.MediaType != "" || strings.TrimSpace(message.Content) == "" {
		return false
	}
	return !isUnsupportedTextForwardContent(message.Content)
}

func isUnsupportedTextForwardContent(content string) bool {
	trimmed := strings.TrimSpace(content)
	return strings.HasPrefix(trimmed, "Contact:") ||
		strings.HasPrefix(trimmed, "Contacts:") ||
		strings.Contains(trimmed, "https://maps.google.com/?q=")
}

func IsForwardMediaMessage(message *domainChatStorage.Message) bool {
	if message == nil {
		return false
	}
	_, ok := forwardableMediaTypes[message.MediaType]
	return ok
}

func newForwardContextInfo(duration *int) *waE2E.ContextInfo {
	contextInfo := &waE2E.ContextInfo{
		IsForwarded:     proto.Bool(true),
		ForwardingScore: proto.Uint32(100),
	}
	if duration != nil && *duration > 0 {
		contextInfo.Expiration = proto.Uint32(uint32(*duration))
	}
	return contextInfo
}

func defaultForwardMimeType(message *domainChatStorage.Message) string {
	switch message.MediaType {
	case "image":
		return "image/jpeg"
	case "video", "video_note":
		return "video/mp4"
	case "ptt":
		return "audio/ogg; codecs=opus"
	case "audio":
		return "audio/mpeg"
	case "sticker":
		return "image/webp"
	case "document":
		ext := strings.ToLower(filepath.Ext(message.Filename))
		if mimeType, ok := knownDocumentMIMEByExtension[ext]; ok {
			return mimeType
		}
		if mimeType := mime.TypeByExtension(ext); mimeType != "" {
			return mimeType
		}
		return "application/octet-stream"
	default:
		return ""
	}
}

func BuildForwardMessageFromStorage(message *domainChatStorage.Message, opts ForwardBuildOptions) (*waE2E.Message, error) {
	if message == nil {
		return nil, fmt.Errorf("message is nil")
	}
	if !IsForwardableStorageMessage(message) {
		return nil, errors.New(ErrUnsupportedForwardType)
	}

	contextInfo := newForwardContextInfo(opts.Duration)
	if message.MediaType == "" {
		return &waE2E.Message{
			ExtendedTextMessage: &waE2E.ExtendedTextMessage{
				Text:        proto.String(message.Content),
				ContextInfo: contextInfo,
			},
		}, nil
	}

	mediaURL := message.URL
	directPath := ResolveMediaDirectPath(message.DirectPath, message.URL)
	mediaKey := message.MediaKey
	fileSHA256 := message.FileSHA256
	fileEncSHA256 := message.FileEncSHA256
	fileLength := message.FileLength
	if opts.Upload != nil {
		mediaURL = opts.Upload.URL
		directPath = opts.Upload.DirectPath
		mediaKey = opts.Upload.MediaKey
		fileSHA256 = opts.Upload.FileSHA256
		fileEncSHA256 = opts.Upload.FileEncSHA256
		fileLength = opts.Upload.FileLength
	} else if directPath == "" && mediaURL == "" {
		return nil, fmt.Errorf("message %s has no media references", message.ID)
	}

	mimeType := opts.MimeType
	if mimeType == "" {
		mimeType = defaultForwardMimeType(message)
	}

	switch message.MediaType {
	case "image":
		media := &waE2E.ImageMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(directPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
			Caption:       proto.String(message.Content),
			ContextInfo:   contextInfo,
		}
		if mimeType != "" {
			media.Mimetype = proto.String(mimeType)
		}
		return &waE2E.Message{ImageMessage: media}, nil
	case "video":
		media := &waE2E.VideoMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(directPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
			Caption:       proto.String(message.Content),
			ContextInfo:   contextInfo,
		}
		if mimeType != "" {
			media.Mimetype = proto.String(mimeType)
		}
		return &waE2E.Message{VideoMessage: media}, nil
	case "video_note":
		media := &waE2E.VideoMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(directPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
			Caption:       proto.String(message.Content),
			ContextInfo:   contextInfo,
		}
		if mimeType != "" {
			media.Mimetype = proto.String(mimeType)
		}
		return &waE2E.Message{PtvMessage: media}, nil
	case "audio", "ptt":
		media := &waE2E.AudioMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(directPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
			ContextInfo:   contextInfo,
		}
		if message.MediaType == "ptt" {
			media.PTT = proto.Bool(true)
		}
		if mimeType != "" {
			media.Mimetype = proto.String(mimeType)
		}
		return &waE2E.Message{AudioMessage: media}, nil
	case "document":
		media := &waE2E.DocumentMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(directPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
			FileName:      proto.String(message.Filename),
			Caption:       proto.String(message.Content),
			ContextInfo:   contextInfo,
		}
		if mimeType != "" {
			media.Mimetype = proto.String(mimeType)
		}
		return &waE2E.Message{DocumentMessage: media}, nil
	case "sticker":
		media := &waE2E.StickerMessage{
			URL:           proto.String(mediaURL),
			DirectPath:    proto.String(directPath),
			MediaKey:      mediaKey,
			FileSHA256:    fileSHA256,
			FileEncSHA256: fileEncSHA256,
			FileLength:    proto.Uint64(fileLength),
			ContextInfo:   contextInfo,
		}
		if mimeType != "" {
			media.Mimetype = proto.String(mimeType)
		}
		return &waE2E.Message{StickerMessage: media}, nil
	default:
		return nil, errors.New(ErrUnsupportedForwardType)
	}
}

func BuildDownloadableMessage(mediaType, mediaURL, directPath, filename string, mediaKey, fileSHA256, fileEncSHA256 []byte, fileLength uint64) (whatsmeow.DownloadableMessage, error) {
	resolvedDirectPath := ResolveMediaDirectPath(directPath, mediaURL)
	switch mediaType {
	case "image":
		return &waE2E.ImageMessage{
			URL: proto.String(mediaURL), DirectPath: proto.String(resolvedDirectPath),
			MediaKey: mediaKey, FileSHA256: fileSHA256, FileEncSHA256: fileEncSHA256,
			FileLength: proto.Uint64(fileLength),
		}, nil
	case "video", "video_note":
		return &waE2E.VideoMessage{
			URL: proto.String(mediaURL), DirectPath: proto.String(resolvedDirectPath),
			MediaKey: mediaKey, FileSHA256: fileSHA256, FileEncSHA256: fileEncSHA256,
			FileLength: proto.Uint64(fileLength),
		}, nil
	case "audio", "ptt":
		return &waE2E.AudioMessage{
			URL: proto.String(mediaURL), DirectPath: proto.String(resolvedDirectPath),
			MediaKey: mediaKey, FileSHA256: fileSHA256, FileEncSHA256: fileEncSHA256,
			FileLength: proto.Uint64(fileLength),
		}, nil
	case "document":
		return &waE2E.DocumentMessage{
			URL: proto.String(mediaURL), DirectPath: proto.String(resolvedDirectPath),
			MediaKey: mediaKey, FileSHA256: fileSHA256, FileEncSHA256: fileEncSHA256,
			FileLength: proto.Uint64(fileLength), FileName: proto.String(filename),
		}, nil
	case "sticker":
		return &waE2E.StickerMessage{
			URL: proto.String(mediaURL), DirectPath: proto.String(resolvedDirectPath),
			MediaKey: mediaKey, FileSHA256: fileSHA256, FileEncSHA256: fileEncSHA256,
			FileLength: proto.Uint64(fileLength),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported media type: %s", mediaType)
	}
}

func ExtractContextInfo(msg *waE2E.Message) *waE2E.ContextInfo {
	if msg == nil {
		return nil
	}
	switch {
	case msg.GetExtendedTextMessage() != nil:
		return msg.GetExtendedTextMessage().GetContextInfo()
	case msg.GetImageMessage() != nil:
		return msg.GetImageMessage().GetContextInfo()
	case msg.GetVideoMessage() != nil:
		return msg.GetVideoMessage().GetContextInfo()
	case msg.GetAudioMessage() != nil:
		return msg.GetAudioMessage().GetContextInfo()
	case msg.GetDocumentMessage() != nil:
		return msg.GetDocumentMessage().GetContextInfo()
	case msg.GetStickerMessage() != nil:
		return msg.GetStickerMessage().GetContextInfo()
	case msg.GetContactMessage() != nil:
		return msg.GetContactMessage().GetContextInfo()
	case msg.GetLocationMessage() != nil:
		return msg.GetLocationMessage().GetContextInfo()
	case msg.GetPtvMessage() != nil:
		return msg.GetPtvMessage().GetContextInfo()
	case msg.GetLiveLocationMessage() != nil:
		return msg.GetLiveLocationMessage().GetContextInfo()
	default:
		return nil
	}
}
