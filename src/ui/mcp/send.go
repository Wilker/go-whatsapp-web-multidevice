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
	mcpServer.AddTool(s.toolSendFile(), s.handleSendFile)
	mcpServer.AddTool(s.toolSendVideo(), s.handleSendVideo)
	mcpServer.AddTool(s.toolSendAudio(), s.handleSendAudio)
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
		return newStandardToolErrorResultFromError("whatsapp_send_text", requestPayload, err), nil
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

	requestPayload := map[string]any{
		"phone":         phone,
		"is_forwarded":  isForwarded,
		"contact_name":  contactName,
		"contact_phone": contactPhone,
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
		return newStandardToolErrorResultFromError("whatsapp_send_contact", requestPayload, err), nil
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

	requestPayload := map[string]any{
		"phone":        phone,
		"is_forwarded": isForwarded,
		"link":         link,
		"caption":      caption,
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
		return newStandardToolErrorResultFromError("whatsapp_send_link", requestPayload, err), nil
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

	requestPayload := map[string]any{
		"phone":        phone,
		"is_forwarded": isForwarded,
		"latitude":     latitude,
		"longitude":    longitude,
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
		return newStandardToolErrorResultFromError("whatsapp_send_location", requestPayload, err), nil
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

	requestPayload := map[string]any{
		"phone":        phone,
		"is_forwarded": isForwarded,
		"image_url":    imageURL,
		"caption":      caption,
		"view_once":    viewOnce,
		"compress":     compress,
	}

	res, err := s.sendService.SendImage(ctx, imageRequest)
	if err != nil {
		return newStandardToolErrorResultFromError("whatsapp_send_image", requestPayload, err), nil
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
	return newStandardToolResult("whatsapp_send_image", res.Status, requestPayload, structured, fallback), nil
}

func (s *SendHandler) toolSendFile() mcp.Tool {
	sendFileTool := mcp.NewTool("whatsapp_send_file",
		mcp.WithDescription("Send a document or generic file as a real WhatsApp attachment. Choose exactly one source: prefer file_path when the file already exists on the MCP host; use file_url only when the file already lives remotely or no local copy is available. Do not upload a local file elsewhere just to use file_url."),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send file to"),
		),
		mcp.WithString("file_url",
			mcp.Description("Remote file URL. Use only when the file already lives outside the MCP host or no local copy is available. The server downloads this URL and sends the binary as an attachment; this is not just a link message."),
		),
		mcp.WithString("file_path",
			mcp.Description("Preferred when the file already exists on the machine running the MCP server. Use this instead of file_url for local files."),
		),
		mcp.WithString("caption",
			mcp.Description("Caption or description for the file"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)

	return sendFileTool
}

func (s *SendHandler) handleSendFile(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	fileURL, _ := request.GetArguments()["file_url"].(string)
	filePath, _ := request.GetArguments()["file_path"].(string)
	if fileURL == "" && filePath == "" {
		return nil, errors.New("either file_url or file_path must be provided")
	}
	if fileURL != "" && filePath != "" {
		return nil, errors.New("provide only one of file_url or file_path")
	}

	caption, ok := request.GetArguments()["caption"].(string)
	if !ok {
		caption = ""
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	fileRequest := domainSend.FileRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		Caption: caption,
	}
	if fileURL != "" {
		fileRequest.FileURL = &fileURL
	}
	if filePath != "" {
		fileRequest.FilePath = &filePath
	}

	requestPayload := map[string]any{
		"phone":        phone,
		"is_forwarded": isForwarded,
		"caption":      caption,
	}
	if fileURL != "" {
		requestPayload["file_url"] = fileURL
	}
	if filePath != "" {
		requestPayload["file_path"] = filePath
	}

	res, err := s.sendService.SendFile(ctx, fileRequest)
	if err != nil {
		return newStandardToolErrorResultFromError("whatsapp_send_file", requestPayload, err), nil
	}

	status := normalizedStatus(res.Status)
	structured := map[string]any{
		"message_id": res.MessageID,
		"recipient": map[string]any{
			"id": phone,
		},
		"is_forwarded": isForwarded,
		"file": map[string]any{
			"url":             fileURL,
			"path":            filePath,
			"source":          mediaSourceLabel(filePath),
			"caption":         caption,
			"caption_preview": truncateForMCP(caption, 140),
		},
	}
	fallback := fmt.Sprintf(
		"File sent\nto: %s\nmessage_id: %s\nstatus: %s\nsource: %s\nfile_path: %s\nfile_url: %s\ncaption_preview: %s",
		phone,
		res.MessageID,
		status,
		mediaSourceLabel(filePath),
		filePath,
		fileURL,
		truncateForMCP(caption, 140),
	)
	return newStandardToolResult("whatsapp_send_file", res.Status, requestPayload, structured, fallback), nil
}

func (s *SendHandler) toolSendVideo() mcp.Tool {
	sendVideoTool := mcp.NewTool("whatsapp_send_video",
		mcp.WithDescription("Send a video as a real WhatsApp attachment. Choose exactly one source: prefer video_path when the video already exists on the MCP host; use video_url only when the video already lives remotely or no local copy is available. Do not upload a local video elsewhere just to use video_url."),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send video to"),
		),
		mcp.WithString("video_url",
			mcp.Description("Remote video URL. Use only when the video already lives outside the MCP host or no local copy is available. The server downloads this URL and sends the binary as an attachment; this is not just a link message."),
		),
		mcp.WithString("video_path",
			mcp.Description("Preferred when the video already exists on the machine running the MCP server. Use this instead of video_url for local videos."),
		),
		mcp.WithString("caption",
			mcp.Description("Caption or description for the video"),
		),
		mcp.WithBoolean("view_once",
			mcp.Description("Whether this video should be viewed only once (default: false)"),
		),
		mcp.WithBoolean("compress",
			mcp.Description("Whether to compress the video before sending (default: true)"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)

	return sendVideoTool
}

func (s *SendHandler) handleSendVideo(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	videoURL, _ := request.GetArguments()["video_url"].(string)
	videoPath, _ := request.GetArguments()["video_path"].(string)
	if videoURL == "" && videoPath == "" {
		return nil, errors.New("either video_url or video_path must be provided")
	}
	if videoURL != "" && videoPath != "" {
		return nil, errors.New("provide only one of video_url or video_path")
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

	videoRequest := domainSend.VideoRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		Caption:  caption,
		ViewOnce: viewOnce,
		Compress: compress,
	}
	if videoURL != "" {
		videoRequest.VideoURL = &videoURL
	}
	if videoPath != "" {
		videoRequest.VideoPath = &videoPath
	}

	requestPayload := map[string]any{
		"phone":        phone,
		"is_forwarded": isForwarded,
		"caption":      caption,
		"view_once":    viewOnce,
		"compress":     compress,
	}
	if videoURL != "" {
		requestPayload["video_url"] = videoURL
	}
	if videoPath != "" {
		requestPayload["video_path"] = videoPath
	}

	res, err := s.sendService.SendVideo(ctx, videoRequest)
	if err != nil {
		return newStandardToolErrorResultFromError("whatsapp_send_video", requestPayload, err), nil
	}

	status := normalizedStatus(res.Status)
	structured := map[string]any{
		"message_id": res.MessageID,
		"recipient": map[string]any{
			"id": phone,
		},
		"is_forwarded": isForwarded,
		"video": map[string]any{
			"url":             videoURL,
			"path":            videoPath,
			"source":          mediaSourceLabel(videoPath),
			"caption":         caption,
			"caption_preview": truncateForMCP(caption, 140),
			"view_once":       viewOnce,
			"compress":        compress,
		},
	}
	fallback := fmt.Sprintf(
		"Video sent\nto: %s\nmessage_id: %s\nstatus: %s\nsource: %s\nvideo_path: %s\nvideo_url: %s\nview_once: %t\ncompress: %t\ncaption_preview: %s",
		phone,
		res.MessageID,
		status,
		mediaSourceLabel(videoPath),
		videoPath,
		videoURL,
		viewOnce,
		compress,
		truncateForMCP(caption, 140),
	)
	return newStandardToolResult("whatsapp_send_video", res.Status, requestPayload, structured, fallback), nil
}

func (s *SendHandler) toolSendAudio() mcp.Tool {
	sendAudioTool := mcp.NewTool("whatsapp_send_audio",
		mcp.WithDescription("Send an audio file as a real WhatsApp attachment. Choose exactly one source: prefer audio_path when the audio already exists on the MCP host; use audio_url only when the audio already lives remotely or no local copy is available. Do not upload a local audio file elsewhere just to use audio_url."),
		mcp.WithString("phone",
			mcp.Required(),
			mcp.Description("Phone number or group ID to send audio to"),
		),
		mcp.WithString("audio_url",
			mcp.Description("Remote audio URL. Use only when the audio already lives outside the MCP host or no local copy is available. The server downloads this URL and sends the binary as an attachment; this is not just a link message."),
		),
		mcp.WithString("audio_path",
			mcp.Description("Preferred when the audio already exists on the machine running the MCP server. Use this instead of audio_url for local audio files."),
		),
		mcp.WithBoolean("ptt",
			mcp.Description("Whether to send the audio as a push-to-talk voice note (default: false)"),
		),
		mcp.WithBoolean("is_forwarded",
			mcp.Description("Whether this message is being forwarded (default: false)"),
		),
	)

	return sendAudioTool
}

func (s *SendHandler) handleSendAudio(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	phone, ok := request.GetArguments()["phone"].(string)
	if !ok {
		return nil, errors.New("phone must be a string")
	}

	audioURL, _ := request.GetArguments()["audio_url"].(string)
	audioPath, _ := request.GetArguments()["audio_path"].(string)
	if audioURL == "" && audioPath == "" {
		return nil, errors.New("either audio_url or audio_path must be provided")
	}
	if audioURL != "" && audioPath != "" {
		return nil, errors.New("provide only one of audio_url or audio_path")
	}

	ptt, ok := request.GetArguments()["ptt"].(bool)
	if !ok {
		ptt = false
	}

	isForwarded, ok := request.GetArguments()["is_forwarded"].(bool)
	if !ok {
		isForwarded = false
	}

	audioRequest := domainSend.AudioRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       phone,
			IsForwarded: isForwarded,
		},
		PTT: ptt,
	}
	if audioURL != "" {
		audioRequest.AudioURL = &audioURL
	}
	if audioPath != "" {
		audioRequest.AudioPath = &audioPath
	}

	requestPayload := map[string]any{
		"phone":        phone,
		"is_forwarded": isForwarded,
		"ptt":          ptt,
	}
	if audioURL != "" {
		requestPayload["audio_url"] = audioURL
	}
	if audioPath != "" {
		requestPayload["audio_path"] = audioPath
	}

	res, err := s.sendService.SendAudio(ctx, audioRequest)
	if err != nil {
		return newStandardToolErrorResultFromError("whatsapp_send_audio", requestPayload, err), nil
	}

	status := normalizedStatus(res.Status)
	structured := map[string]any{
		"message_id": res.MessageID,
		"recipient": map[string]any{
			"id": phone,
		},
		"is_forwarded": isForwarded,
		"audio": map[string]any{
			"url":    audioURL,
			"path":   audioPath,
			"source": mediaSourceLabel(audioPath),
			"ptt":    ptt,
		},
	}
	fallback := fmt.Sprintf(
		"Audio sent\nto: %s\nmessage_id: %s\nstatus: %s\nsource: %s\naudio_path: %s\naudio_url: %s\nptt: %t",
		phone,
		res.MessageID,
		status,
		mediaSourceLabel(audioPath),
		audioPath,
		audioURL,
		ptt,
	)
	return newStandardToolResult("whatsapp_send_audio", res.Status, requestPayload, structured, fallback), nil
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

	requestPayload := map[string]any{
		"phone":        phone,
		"is_forwarded": isForwarded,
		"sticker_url":  stickerURL,
	}

	res, err := s.sendService.SendSticker(ctx, stickerRequest)
	if err != nil {
		return newStandardToolErrorResultFromError("whatsapp_send_sticker", requestPayload, err), nil
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
	return newStandardToolResult("whatsapp_send_sticker", res.Status, requestPayload, structured, fallback), nil
}

func normalizedStatus(status string) string {
	return normalizeEnvelopeStatus(status)
}

func mediaSourceLabel(localPath string) string {
	if strings.TrimSpace(localPath) != "" {
		return "local_path"
	}
	return "url"
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
