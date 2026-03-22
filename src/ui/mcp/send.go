package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	mcpHelpers "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/helpers"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type SendHandler struct {
	sendService domainSend.ISendUsecase
}

func InitMcpSend(sendService domainSend.ISendUsecase) *SendHandler {
	return &SendHandler{
		sendService: sendService,
	}
}

func (s *SendHandler) AddSendTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(s.toolSendText(), s.handleSendText)
	mcpServer.AddTool(s.toolSendContact(), s.handleSendContact)
	mcpServer.AddTool(s.toolSendLink(), s.handleSendLink)
	mcpServer.AddTool(s.toolSendLocation(), s.handleSendLocation)
	mcpServer.AddTool(s.toolSendImage(), s.handleSendImage)
	mcpServer.AddTool(s.toolSendSticker(), s.handleSendSticker)
}

func (s *SendHandler) toolSendText() mcp.Tool {
	sendTextTool := mcp.NewTool("whatsapp_send_text",
		mcp.WithDescription("Send a text message to a WhatsApp contact or group. Supports ghost mentions (mention users without showing @phone in message text)."),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send message to"),
		),
		mcp.WithString("message",
			mcp.Required(),
			mcp.Description("The text message to send."),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
		mcp.WithString("reply_message_id",
			mcp.Description("Message ID to reply to (optional)"),
		),
		mcp.WithArray("mentions",
			mcp.Description("List of phone numbers or JIDs to mention (ghost mentions - users will be notified but @phone won't appear in message text). Use \"@everyone\" to mention all group participants. Example: [\"628123456789\", \"@everyone\"]"),
		),
	)

	return sendTextTool
}

func (s *SendHandler) handleSendText(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	message, ok := request.GetArguments()["message"].(string)
	if !ok {
		return nil, errors.New("message must be a string")
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	replyMessageId, ok := request.GetArguments()["reply_message_id"].(string)
	if !ok {
		replyMessageId = ""
	}

	// Parse mentions array (ghost mentions)
	var mentions []string
	if mentionsRaw, ok := request.GetArguments()["mentions"].([]interface{}); ok {
		for _, m := range mentionsRaw {
			if mentionStr, ok := m.(string); ok {
				mentions = append(mentions, mentionStr)
			}
		}
	}

	res, err := s.sendService.SendText(ctx, domainSend.MessageRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		Message:        message,
		ReplyMessageID: &replyMessageId,
		Mentions:       mentions,
	})

	if err != nil {
		return nil, err
	}

	status := normalizedStatus(res.Status)
	structured := map[string]any{
		"message_id": res.MessageID,
		"recipient": map[string]any{
			"id": phone,
		},
		"is_forwarded":   isForwarded,
		"text":           message,
		"text_preview":   truncateForMCP(message, 140),
		"mentions":       mentions,
		"mentions_count": len(mentions),
	}
	if replyMessageId != "" {
		structured["reply_to_message_id"] = replyMessageId
	}
	fallback := fmt.Sprintf(
		"Text message sent\nto: %s\nmessage_id: %s\nstatus: %s\nmentions_count: %d\nmessage_preview: %s",
		phone,
		res.MessageID,
		status,
		len(mentions),
		truncateForMCP(message, 140),
	)
	if replyMessageId != "" {
		fallback += "\nreply_message_id: " + replyMessageId
	}

	requestPayload := map[string]any{
		"phone":          phone,
		"is_forwarded":   isForwarded,
		"message":        message,
		"mentions":       mentions,
		"mentions_count": len(mentions),
	}
	if replyMessageId != "" {
		requestPayload["reply_message_id"] = replyMessageId
	}
	return newStandardToolResult("whatsapp_send_text", res.Status, requestPayload, structured, fallback), nil
}

func (s *SendHandler) toolSendContact() mcp.Tool {
	sendContactTool := mcp.NewTool("whatsapp_send_contact",
		mcp.WithDescription("Send a contact card to a WhatsApp contact or group."),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send contact to"),
		),
		mcp.WithString("contact_name",
			mcp.Required(),
			mcp.Description("Name of the contact to send"),
		),
		mcp.WithString("contact_phone",
			mcp.Required(),
			mcp.Description("Phone number of the contact to send"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)

	return sendContactTool
}

func (s *SendHandler) handleSendContact(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	contactName, ok := request.GetArguments()["contact_name"].(string)
	if !ok {
		return nil, errors.New("contact_name must be a string")
	}

	contactPhone, ok := request.GetArguments()["contact_phone"].(string)
	if !ok {
		return nil, errors.New("contact_phone must be a string")
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	res, err := s.sendService.SendContact(ctx, domainSend.ContactRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		ContactName:  contactName,
		ContactPhone: contactPhone,
	})

	if err != nil {
		return nil, err
	}

	status := normalizedStatus(res.Status)
	structured := map[string]any{
		"message_id": res.MessageID,
		"recipient": map[string]any{
			"id": phone,
		},
		"is_forwarded": isForwarded,
		"contact": map[string]any{
			"name":  contactName,
			"phone": contactPhone,
		},
	}
	fallback := fmt.Sprintf(
		"Contact sent\nto: %s\nmessage_id: %s\nstatus: %s\ncontact_name: %s\ncontact_phone: %s",
		phone,
		res.MessageID,
		status,
		contactName,
		contactPhone,
	)
	requestPayload := map[string]any{
		"phone":         phone,
		"is_forwarded":  isForwarded,
		"contact_name":  contactName,
		"contact_phone": contactPhone,
	}
	return newStandardToolResult("whatsapp_send_contact", res.Status, requestPayload, structured, fallback), nil
}

func (s *SendHandler) toolSendLink() mcp.Tool {
	sendLinkTool := mcp.NewTool("whatsapp_send_link",
		mcp.WithDescription("Send a link with caption to a WhatsApp contact or group."),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send link to"),
		),
		mcp.WithString("link",
			mcp.Required(),
			mcp.Description("URL link to send"),
		),
		mcp.WithString("caption",
			mcp.Required(),
			mcp.Description("Caption or description for the link"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)

	return sendLinkTool
}

func (s *SendHandler) handleSendLink(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	link, ok := request.GetArguments()["link"].(string)
	if !ok {
		return nil, errors.New("link must be a string")
	}

	caption, ok := request.GetArguments()["caption"].(string)
	if !ok {
		caption = ""
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	res, err := s.sendService.SendLink(ctx, domainSend.LinkRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		Link:    link,
		Caption: caption,
	})

	if err != nil {
		return nil, err
	}

	status := normalizedStatus(res.Status)
	structured := map[string]any{
		"message_id": res.MessageID,
		"recipient": map[string]any{
			"id": phone,
		},
		"is_forwarded": isForwarded,
		"link": map[string]any{
			"url":             link,
			"caption":         caption,
			"caption_preview": truncateForMCP(caption, 140),
		},
	}
	fallback := fmt.Sprintf(
		"Link sent\nto: %s\nmessage_id: %s\nstatus: %s\nlink: %s\ncaption_preview: %s",
		phone,
		res.MessageID,
		status,
		link,
		truncateForMCP(caption, 140),
	)
	requestPayload := map[string]any{
		"phone":        phone,
		"is_forwarded": isForwarded,
		"link":         link,
		"caption":      caption,
	}
	return newStandardToolResult("whatsapp_send_link", res.Status, requestPayload, structured, fallback), nil
}

func (s *SendHandler) toolSendLocation() mcp.Tool {
	sendLocationTool := mcp.NewTool("whatsapp_send_location",
		mcp.WithDescription("Send a location coordinates to a WhatsApp contact or group."),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send location to"),
		),
		mcp.WithString("latitude",
			mcp.Required(),
			mcp.Description("Latitude coordinate (as string)"),
		),
		mcp.WithString("longitude",
			mcp.Required(),
			mcp.Description("Longitude coordinate (as string)"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)

	return sendLocationTool
}

func (s *SendHandler) handleSendLocation(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	latitude, ok := request.GetArguments()["latitude"].(string)
	if !ok {
		return nil, errors.New("latitude must be a string")
	}

	longitude, ok := request.GetArguments()["longitude"].(string)
	if !ok {
		return nil, errors.New("longitude must be a string")
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	res, err := s.sendService.SendLocation(ctx, domainSend.LocationRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		Latitude:  latitude,
		Longitude: longitude,
	})

	if err != nil {
		return nil, err
	}

	status := normalizedStatus(res.Status)
	structured := map[string]any{
		"message_id": res.MessageID,
		"recipient": map[string]any{
			"id": phone,
		},
		"is_forwarded": isForwarded,
		"location": map[string]any{
			"latitude":  latitude,
			"longitude": longitude,
		},
	}
	fallback := fmt.Sprintf(
		"Location sent\nto: %s\nmessage_id: %s\nstatus: %s\nlatitude: %s\nlongitude: %s",
		phone,
		res.MessageID,
		status,
		latitude,
		longitude,
	)
	requestPayload := map[string]any{
		"phone":        phone,
		"is_forwarded": isForwarded,
		"latitude":     latitude,
		"longitude":    longitude,
	}
	return newStandardToolResult("whatsapp_send_location", res.Status, requestPayload, structured, fallback), nil
}

func (s *SendHandler) toolSendImage() mcp.Tool {
	sendImageTool := mcp.NewTool("whatsapp_send_image",
		mcp.WithDescription("Send an image to a WhatsApp contact or group."),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send image to"),
		),
		mcp.WithString("image_url",
			mcp.Description("URL of the image to send"),
		),
		mcp.WithString("caption",
			mcp.Description("Caption or description for the image"),
		),
		mcp.WithBoolean("view_once",
			mcp.Description("Whether this image should be viewed only once (default: false)"),
		),
		mcp.WithBoolean("compress",
			mcp.Description("Whether to compress the image (default: true)"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)

	return sendImageTool
}

func (s *SendHandler) handleSendImage(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	imageURL, imageURLOk := request.GetArguments()["image_url"].(string)
	if !imageURLOk {
		return nil, errors.New("image_url must be a string")
	}

	caption, ok := request.GetArguments()["caption"].(string)
	if !ok {
		caption = ""
	}

	viewOnce, ok := request.GetArguments()["view_once"].(bool)
	if !ok {
		viewOnce = false
	}

	compress, ok := request.GetArguments()["compress"].(bool)
	if !ok {
		compress = true
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	// Create image request
	imageRequest := domainSend.ImageRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		Caption:  caption,
		ViewOnce: viewOnce,
		Compress: compress,
	}

	if imageURLOk && imageURL != "" {
		imageRequest.ImageURL = &imageURL
	}
	res, err := s.sendService.SendImage(ctx, imageRequest)
	if err != nil {
		return nil, err
	}

	status := normalizedStatus(res.Status)
	structured := map[string]any{
		"message_id": res.MessageID,
		"recipient": map[string]any{
			"id": phone,
		},
		"is_forwarded": isForwarded,
		"image": map[string]any{
			"url":             imageURL,
			"caption":         caption,
			"caption_preview": truncateForMCP(caption, 140),
			"view_once":       viewOnce,
			"compress":        compress,
		},
	}
	fallback := fmt.Sprintf(
		"Image sent\nto: %s\nmessage_id: %s\nstatus: %s\nimage_url: %s\nview_once: %t\ncompress: %t\ncaption_preview: %s",
		phone,
		res.MessageID,
		status,
		imageURL,
		viewOnce,
		compress,
		truncateForMCP(caption, 140),
	)
	requestPayload := map[string]any{
		"phone":        phone,
		"is_forwarded": isForwarded,
		"image_url":    imageURL,
		"caption":      caption,
		"view_once":    viewOnce,
		"compress":     compress,
	}
	return newStandardToolResult("whatsapp_send_image", res.Status, requestPayload, structured, fallback), nil
}

func (s *SendHandler) toolSendSticker() mcp.Tool {
	sendStickerTool := mcp.NewTool("whatsapp_send_sticker",
		mcp.WithDescription("Send a sticker to a WhatsApp contact or group. Images are automatically converted to WebP sticker format."),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send sticker to"),
		),
		mcp.WithString("sticker_url",
			mcp.Description("URL of the image to convert to sticker and send"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this is a forwarded sticker"),
		),
	)

	return sendStickerTool
}

func (s *SendHandler) handleSendSticker(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	stickerURL, stickerURLOk := request.GetArguments()["sticker_url"].(string)
	if !stickerURLOk || stickerURL == "" {
		return nil, errors.New("sticker_url must be a non-empty string")
	}

	isForwarded := false
	if val, ok := request.GetArguments()["is_forwarded"].(bool); ok {
		isForwarded = val
	}

	stickerRequest := domainSend.StickerRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		StickerURL: &stickerURL,
	}

	res, err := s.sendService.SendSticker(ctx, stickerRequest)
	if err != nil {
		return nil, err
	}

	status := normalizedStatus(res.Status)
	structured := map[string]any{
		"message_id": res.MessageID,
		"recipient": map[string]any{
			"id": phone,
		},
		"is_forwarded": isForwarded,
		"sticker": map[string]any{
			"url": stickerURL,
		},
	}
	fallback := fmt.Sprintf(
		"Sticker sent\nto: %s\nmessage_id: %s\nstatus: %s\nsticker_url: %s",
		phone,
		res.MessageID,
		status,
		stickerURL,
	)
	requestPayload := map[string]any{
		"phone":        phone,
		"is_forwarded": isForwarded,
		"sticker_url":  stickerURL,
	}
	return newStandardToolResult("whatsapp_send_sticker", res.Status, requestPayload, structured, fallback), nil
}

func normalizedStatus(status string) string {
	return normalizeEnvelopeStatus(status)
}

func truncateForMCP(text string, maxRunes int) string {
	trimmed := strings.TrimSpace(text)
	if maxRunes <= 0 {
		return ""
	}

	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}
