package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	mcpHelpers "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/helpers"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type QueryHandler struct {
	chatService    domainChat.IChatUsecase
	userService    domainUser.IUserUsecase
	messageService domainMessage.IMessageUsecase
	exportJobs     *chatExportJobManager
}

func InitMcpQuery(chatService domainChat.IChatUsecase, userService domainUser.IUserUsecase, messageService domainMessage.IMessageUsecase) *QueryHandler {
	return &QueryHandler{
		chatService:    chatService,
		userService:    userService,
		messageService: messageService,
		exportJobs:     newChatExportJobManager(filepath.Join(config.PathStorages, "exports", "jobs")),
	}
}

func (h *QueryHandler) AddQueryTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(h.toolListContacts(), h.handleListContacts)
	mcpServer.AddTool(h.toolListChats(), h.handleListChats)
	mcpServer.AddTool(h.toolGetChatMessages(), h.handleGetChatMessages)
	mcpServer.AddTool(h.toolExportChat(), h.handleExportChat)
	mcpServer.AddTool(h.toolExportChatAsyncStart(), h.handleExportChatAsyncStart)
	mcpServer.AddTool(h.toolExportChatAsyncStatus(), h.handleExportChatAsyncStatus)
	mcpServer.AddTool(h.toolExportChatAsyncResult(), h.handleExportChatAsyncResult)
	mcpServer.AddTool(h.toolDownloadMedia(), h.handleDownloadMedia)
	mcpServer.AddTool(h.toolArchiveChat(), h.handleArchiveChat)
}

func (h *QueryHandler) toolListContacts() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_list_contacts",
		mcp.WithDescription("Retrieve all contacts available in the connected WhatsApp account."),
		mcp.WithTitleAnnotation("List Contacts"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)
}

func (h *QueryHandler) handleListContacts(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	resp, err := h.userService.MyListContacts(ctx)
	if err != nil {
		return nil, err
	}

	fallback := buildListContactsFallback(resp)
	resultPayload := buildContactsResultPayload(resp)
	return newStandardToolResult(
		"whatsapp_list_contacts",
		"success",
		map[string]any{},
		resultPayload,
		fallback,
	), nil
}

func (h *QueryHandler) toolListChats() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_list_chats",
		mcp.WithDescription("Retrieve recent chats with optional pagination and search filters."),
		mcp.WithTitleAnnotation("List Chats"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of chats to return (default 25, max 100)."),
			mcp.DefaultNumber(25),
		),
		mcp.WithNumber("offset",
			mcp.Description("Number of chats to skip from the start (default 0)."),
			mcp.DefaultNumber(0),
		),
		mcp.WithString("search",
			mcp.Description("Filter chats whose name contains this text."),
		),
		mcp.WithBoolean("has_media",
			mcp.Description("If true, return only chats that contain media messages."),
			mcp.DefaultBool(false),
		),
	)
}

func (h *QueryHandler) handleListChats(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	var hasMedia bool
	args := request.GetArguments()
	if args != nil {
		if value, ok := args["has_media"]; ok {
			parsed, err := toBool(value)
			if err != nil {
				return nil, err
			}
			hasMedia = parsed
		}
	}

	req := domainChat.ListChatsRequest{
		Limit:    request.GetInt("limit", 25),
		Offset:   request.GetInt("offset", 0),
		Search:   request.GetString("search", ""),
		HasMedia: hasMedia,
	}

	resp, err := h.chatService.ListChats(ctx, req)
	if err != nil {
		return nil, err
	}

	fallback := buildListChatsFallback(resp, req)
	resultPayload := buildChatsResultPayload(resp)
	return newStandardToolResult(
		"whatsapp_list_chats",
		"success",
		req,
		resultPayload,
		fallback,
	), nil
}

func buildListChatsFallback(resp domainChat.ListChatsResponse, req domainChat.ListChatsRequest) string {
	const maxPreview = 10

	totalOnPage := len(resp.Data)
	totalAvailable := resp.Pagination.Total

	if totalOnPage == 0 {
		return fmt.Sprintf(
			"No chats found (offset %d, limit %d, total %d)",
			req.Offset,
			req.Limit,
			totalAvailable,
		)
	}

	previewCount := totalOnPage
	if previewCount > maxPreview {
		previewCount = maxPreview
	}

	lines := make([]string, 0, previewCount+2)
	lines = append(lines, fmt.Sprintf(
		"Found %d chats in this page (offset %d, limit %d, total %d):",
		totalOnPage,
		req.Offset,
		req.Limit,
		totalAvailable,
	))
	if req.Search != "" {
		lines = append(lines, fmt.Sprintf("Search: %q", req.Search))
	}
	if req.HasMedia {
		lines = append(lines, "Filter: has_media=true")
	}

	for i := 0; i < previewCount; i++ {
		chat := resp.Data[i]
		name := strings.TrimSpace(chat.Name)
		if name == "" {
			name = "(no name)"
		}

		lines = append(lines, fmt.Sprintf("%d. %s | %s", i+1, name, chat.JID))
	}

	if totalOnPage > previewCount {
		lines = append(lines, fmt.Sprintf("...and %d more chats in this page.", totalOnPage-previewCount))
	}

	return strings.Join(lines, "\n")
}

func (h *QueryHandler) toolGetChatMessages() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_get_chat_messages",
		mcp.WithDescription("Fetch messages from a specific chat, with optional pagination, search, and time filters."),
		mcp.WithTitleAnnotation("Get Chat Messages"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("chat_jid",
			mcp.Description("The chat JID (e.g., 628123456789@s.whatsapp.net or group@g.us)."),
			mcp.Required(),
		),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of messages to return (default 50, max 100)."),
			mcp.DefaultNumber(50),
		),
		mcp.WithNumber("offset",
			mcp.Description("Number of messages to skip from the start (default 0)."),
			mcp.DefaultNumber(0),
		),
		mcp.WithString("start_time",
			mcp.Description("Filter messages sent after this RFC3339 timestamp."),
		),
		mcp.WithString("end_time",
			mcp.Description("Filter messages sent before this RFC3339 timestamp."),
		),
		mcp.WithBoolean("media_only",
			mcp.Description("If true, return only messages containing media."),
			mcp.DefaultBool(false),
		),
		mcp.WithBoolean("is_from_me",
			mcp.Description("If provided, filter messages sent by you (true) or others (false)."),
		),
		mcp.WithString("search",
			mcp.Description("Full-text search within the chat history (case-insensitive)."),
		),
	)
}

func (h *QueryHandler) handleGetChatMessages(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return nil, err
	}

	args := request.GetArguments()

	var startTimePtr *string
	startTime := strings.TrimSpace(request.GetString("start_time", ""))
	if startTime != "" {
		startTimePtr = &startTime
	}

	var endTimePtr *string
	endTime := strings.TrimSpace(request.GetString("end_time", ""))
	if endTime != "" {
		endTimePtr = &endTime
	}

	mediaOnly := false
	if args != nil {
		if value, ok := args["media_only"]; ok {
			parsed, err := toBool(value)
			if err != nil {
				return nil, err
			}
			mediaOnly = parsed
		}
	}

	var isFromMePtr *bool
	if args != nil {
		if value, ok := args["is_from_me"]; ok {
			parsed, err := toBool(value)
			if err != nil {
				return nil, err
			}
			isFromMePtr = &parsed
		}
	}

	req := domainChat.GetChatMessagesRequest{
		ChatJID:   chatJID,
		Limit:     request.GetInt("limit", 50),
		Offset:    request.GetInt("offset", 0),
		StartTime: startTimePtr,
		EndTime:   endTimePtr,
		MediaOnly: mediaOnly,
		IsFromMe:  isFromMePtr,
		Search:    request.GetString("search", ""),
	}

	resp, err := h.chatService.GetChatMessages(ctx, req)
	if err != nil {
		return nil, err
	}

	fallback := buildGetChatMessagesFallback(resp, req)
	resultPayload := buildChatMessagesResultPayload(resp)
	return newStandardToolResult(
		"whatsapp_get_chat_messages",
		"success",
		req,
		resultPayload,
		fallback,
	), nil
}

func (h *QueryHandler) toolExportChat() mcp.Tool {
	options := append([]mcp.ToolOption{
		mcp.WithDescription("Generate a local chat export bundle with a mandatory human-readable TXT file, correlated JSON/Markdown manifests, and optional media files packaged in a ZIP. When media export is enabled, the server will also try to recover expired media that is still available via WhatsApp Web before finalizing the bundle."),
		mcp.WithTitleAnnotation("Export Chat"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	}, exportChatArgumentOptions()...)

	return mcp.NewTool(
		"whatsapp_export_chat",
		options...,
	)
}

func exportChatArgumentOptions() []mcp.ToolOption {
	return []mcp.ToolOption{
		mcp.WithString("chat_jid",
			mcp.Description("The chat JID (e.g., 628123456789@s.whatsapp.net or group@g.us)."),
			mcp.Required(),
		),
		mcp.WithString("export_type",
			mcp.Description("Export scope: 'full' for the whole chat history that matches the filters, or 'partial' for a limited slice."),
		),
		mcp.WithNumber("message_count",
			mcp.Description("Number of messages to export when export_type='partial' (default 200)."),
			mcp.DefaultNumber(200),
		),
		mcp.WithNumber("offset",
			mcp.Description("Number of messages to skip before exporting (default 0)."),
			mcp.DefaultNumber(0),
		),
		mcp.WithString("date",
			mcp.Description("Export only one local date. Accepted formats include YYYY-MM-DD and DD-MM-YYYY."),
		),
		mcp.WithString("date_from",
			mcp.Description("Export messages sent on or after this date/time. Accepted formats include YYYY-MM-DD, YYYY-MM-DD HH:MM:SS, RFC3339, and DD-MM-YYYY."),
		),
		mcp.WithString("date_to",
			mcp.Description("Export messages sent on or before this date/time. Accepted formats include YYYY-MM-DD, YYYY-MM-DD HH:MM:SS, RFC3339, and DD-MM-YYYY."),
		),
		mcp.WithString("output_dir",
			mcp.Description("Optional local directory where the export bundle should be written. If omitted, the server default export directory is used."),
		),
		mcp.WithBoolean("include_media",
			mcp.Description("If true, download media files for the exported messages and include them in the generated ZIP bundle."),
			mcp.DefaultBool(false),
		),
		mcp.WithBoolean("media_only",
			mcp.Description("If true, export only messages that contain media."),
			mcp.DefaultBool(false),
		),
		mcp.WithBoolean("is_from_me",
			mcp.Description("If provided, export only messages sent by you (true) or by others (false)."),
		),
		mcp.WithString("search",
			mcp.Description("Full-text search within the chat history (case-insensitive)."),
		),
	}
}

func (h *QueryHandler) handleExportChat(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	options, requestPayload, err := parseChatExportOptions(request)
	if err != nil {
		return nil, err
	}

	collected, err := h.collectMessagesForExport(ctx, options, nil)
	if err != nil {
		return nil, err
	}

	resultPayload, fallback, err := h.generateLocalChatExport(ctx, options, collected, nil)
	if err != nil {
		return nil, err
	}

	return newStandardToolResult(
		"whatsapp_export_chat",
		"success",
		requestPayload,
		resultPayload,
		fallback,
	), nil
}

func (h *QueryHandler) toolExportChatAsyncStart() mcp.Tool {
	options := append([]mcp.ToolOption{
		mcp.WithDescription("Create a background chat export job for long-running exports with optional media downloads, then return a job ID immediately."),
		mcp.WithTitleAnnotation("Start Async Export"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
	}, exportChatArgumentOptions()...)

	return mcp.NewTool(
		"whatsapp_export_chat_async_start",
		options...,
	)
}

func (h *QueryHandler) handleExportChatAsyncStart(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if _, err := mcpHelpers.ContextWithDefaultDevice(ctx); err != nil {
		return nil, err
	}

	options, requestPayload, err := parseChatExportOptions(request)
	if err != nil {
		return nil, err
	}
	if h.exportJobs == nil {
		return newStandardToolErrorResult(
			"whatsapp_export_chat_async_start",
			"failed",
			requestPayload,
			map[string]any{
				"error": "async export job manager is not initialized",
			},
			"Async export job manager is not initialized",
		), nil
	}

	job, err := h.exportJobs.Start(requestPayload, func(_ string, report chatExportProgressReporter) (map[string]any, string, error) {
		backgroundCtx, ctxErr := mcpHelpers.ContextWithDefaultDevice(context.Background())
		if ctxErr != nil {
			return nil, "", ctxErr
		}

		collected, collectErr := h.collectMessagesForExport(backgroundCtx, options, report)
		if collectErr != nil {
			return nil, "", collectErr
		}

		return h.generateLocalChatExport(backgroundCtx, options, collected, report)
	})
	if err != nil {
		return newStandardToolErrorResult(
			"whatsapp_export_chat_async_start",
			"failed",
			requestPayload,
			map[string]any{
				"error": err.Error(),
			},
			err.Error(),
		), nil
	}

	resultPayload := map[string]any{
		"job_id":        job.JobID,
		"status":        job.Status,
		"created_at":    job.CreatedAt,
		"chat_jid":      options.ChatJID,
		"export_type":   options.ExportType,
		"include_media": options.IncludeMedia,
		"status_tool":   "whatsapp_export_chat_async_status",
		"result_tool":   "whatsapp_export_chat_async_result",
	}
	fallback := fmt.Sprintf(
		"Async chat export job created\njob_id: %s\nstatus: %s\nchat: %s\nexport_type: %s\ninclude_media: %t\nstatus_tool: whatsapp_export_chat_async_status\nresult_tool: whatsapp_export_chat_async_result",
		job.JobID,
		job.Status,
		options.ChatJID,
		options.ExportType,
		options.IncludeMedia,
	)

	return newStandardToolResult(
		"whatsapp_export_chat_async_start",
		job.Status,
		requestPayload,
		resultPayload,
		fallback,
	), nil
}

func (h *QueryHandler) toolExportChatAsyncStatus() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_export_chat_async_status",
		mcp.WithDescription("Get the current status and progress of an asynchronous chat export job."),
		mcp.WithTitleAnnotation("Async Export Status"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("job_id",
			mcp.Description("The async export job identifier returned by whatsapp_export_chat_async_start."),
			mcp.Required(),
		),
	)
}

func (h *QueryHandler) handleExportChatAsyncStatus(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	jobID, err := request.RequireString("job_id")
	if err != nil {
		return nil, err
	}

	requestPayload := map[string]any{
		"job_id": strings.TrimSpace(jobID),
	}

	job, err := h.exportJobs.Get(jobID)
	if err != nil {
		return newStandardToolErrorResult(
			"whatsapp_export_chat_async_status",
			"not_found",
			requestPayload,
			map[string]any{
				"job_id": strings.TrimSpace(jobID),
				"error":  err.Error(),
			},
			err.Error(),
		), nil
	}

	resultPayload := buildAsyncExportJobStatePayload(job)
	fallback := fmt.Sprintf(
		"Async export job status\njob_id: %s\nstatus: %s\nstatus_message: %s",
		job.JobID,
		job.Status,
		job.StatusMessage,
	)

	return newStandardToolResult(
		"whatsapp_export_chat_async_status",
		job.Status,
		requestPayload,
		resultPayload,
		fallback,
	), nil
}

func (h *QueryHandler) toolExportChatAsyncResult() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_export_chat_async_result",
		mcp.WithDescription("Fetch the final result of an asynchronous chat export job after it completes."),
		mcp.WithTitleAnnotation("Async Export Result"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("job_id",
			mcp.Description("The async export job identifier returned by whatsapp_export_chat_async_start."),
			mcp.Required(),
		),
	)
}

func (h *QueryHandler) handleExportChatAsyncResult(_ context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	jobID, err := request.RequireString("job_id")
	if err != nil {
		return nil, err
	}

	requestPayload := map[string]any{
		"job_id": strings.TrimSpace(jobID),
	}

	job, err := h.exportJobs.Get(jobID)
	if err != nil {
		return newStandardToolErrorResult(
			"whatsapp_export_chat_async_result",
			"not_found",
			requestPayload,
			map[string]any{
				"job_id": strings.TrimSpace(jobID),
				"error":  err.Error(),
			},
			err.Error(),
		), nil
	}

	switch job.Status {
	case chatExportJobStatusCompleted:
		summary := strings.TrimSpace(job.Summary)
		if summary == "" {
			summary = fmt.Sprintf("Async export job %s completed", job.JobID)
		}
		return newStandardToolResult(
			"whatsapp_export_chat_async_result",
			"success",
			requestPayload,
			job.Result,
			summary,
		), nil
	case chatExportJobStatusFailed:
		resultPayload := buildAsyncExportJobStatePayload(job)
		summary := fmt.Sprintf(
			"Async export job failed\njob_id: %s\nerror: %s",
			job.JobID,
			strings.TrimSpace(job.Error),
		)
		return newStandardToolErrorResult(
			"whatsapp_export_chat_async_result",
			chatExportJobStatusFailed,
			requestPayload,
			resultPayload,
			summary,
		), nil
	default:
		resultPayload := buildAsyncExportJobStatePayload(job)
		summary := fmt.Sprintf(
			"job not completed yet\njob_id: %s\nstatus: %s\nstatus_message: %s",
			job.JobID,
			job.Status,
			job.StatusMessage,
		)
		return newStandardToolErrorResult(
			"whatsapp_export_chat_async_result",
			job.Status,
			requestPayload,
			resultPayload,
			summary,
		), nil
	}
}

func buildAsyncExportJobStatePayload(job *chatExportAsyncJob) map[string]any {
	if job == nil {
		return map[string]any{}
	}

	return map[string]any{
		"job_id":         job.JobID,
		"status":         job.Status,
		"status_message": job.StatusMessage,
		"created_at":     job.CreatedAt,
		"started_at":     job.StartedAt,
		"updated_at":     job.UpdatedAt,
		"finished_at":    job.FinishedAt,
		"request":        cloneStringAnyMap(job.Request),
		"chat":           cloneStringAnyMap(job.Chat),
		"progress":       cloneStringAnyMap(job.Progress),
		"result":         cloneStringAnyMap(job.Result),
		"error":          strings.TrimSpace(job.Error),
	}
}

func (h *QueryHandler) toolDownloadMedia() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_download_message_media",
		mcp.WithDescription("Download media associated with a specific message and return the local file path."),
		mcp.WithTitleAnnotation("Download Message Media"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("message_id",
			mcp.Description("The WhatsApp message ID that contains the media."),
			mcp.Required(),
		),
		mcp.WithString("phone",
			mcp.Description("The target chat phone number or JID associated with the message."),
			mcp.Required(),
		),
	)
}

func (h *QueryHandler) handleDownloadMedia(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	messageID, err := request.RequireString("message_id")
	if err != nil {
		return nil, err
	}

	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	utils.SanitizePhone(&phone)

	req := domainMessage.DownloadMediaRequest{
		MessageID: messageID,
		Phone:     phone,
	}

	resp, err := h.messageService.DownloadMedia(ctx, req)
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf(
		"Downloaded media\nmessage_id: %s\nchat: %s\nmedia_type: %s\nfilename: %s\nfile_path: %s\nfile_size: %d\nrecovery_method: %s",
		resp.MessageID,
		phone,
		resp.MediaType,
		resp.Filename,
		resp.FilePath,
		resp.FileSize,
		resp.RecoveryMethod,
	)
	resultPayload := map[string]any{
		"message_id": resp.MessageID,
		"chat": map[string]any{
			"jid": phone,
		},
		"media": map[string]any{
			"type":            resp.MediaType,
			"filename":        resp.Filename,
			"path":            resp.FilePath,
			"size_bytes":      resp.FileSize,
			"recovery_method": resp.RecoveryMethod,
			"failure_reason":  resp.FailureReason,
		},
		"details": resp,
	}
	return newStandardToolResult(
		"whatsapp_download_message_media",
		"success",
		req,
		resultPayload,
		fallback,
	), nil
}

func toBool(value any) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		parsed, err := strconv.ParseBool(v)
		if err != nil {
			return false, fmt.Errorf("unable to parse boolean value %q", v)
		}
		return parsed, nil
	case float64:
		return v != 0, nil
	case int:
		return v != 0, nil
	default:
		return false, fmt.Errorf("unsupported boolean value type %T", value)
	}
}

func (h *QueryHandler) toolArchiveChat() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_archive_chat",
		mcp.WithDescription("Archive or unarchive a WhatsApp chat. Archived chats are hidden from the main chat list."),
		mcp.WithTitleAnnotation("Archive/Unarchive Chat"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("chat_jid",
			mcp.Description("The chat JID (e.g., 628123456789@s.whatsapp.net or group@g.us)."),
			mcp.Required(),
		),
		mcp.WithBoolean("archived",
			mcp.Description("Set to true to archive the chat, false to unarchive it."),
			mcp.Required(),
		),
	)
}

func (h *QueryHandler) handleArchiveChat(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return nil, err
	}

	args := request.GetArguments()
	if args == nil {
		return nil, fmt.Errorf("missing required argument: archived")
	}

	archivedValue, ok := args["archived"]
	if !ok {
		return nil, fmt.Errorf("missing required argument: archived")
	}

	archived, err := toBool(archivedValue)
	if err != nil {
		return nil, err
	}

	req := domainChat.ArchiveChatRequest{
		ChatJID:  chatJID,
		Archived: archived,
	}

	resp, err := h.chatService.ArchiveChat(ctx, req)
	if err != nil {
		return nil, err
	}

	state := "unarchived"
	if archived {
		state = "archived"
	}

	resultPayload := map[string]any{
		"chat": map[string]any{
			"jid": chatJID,
		},
		"archived": archived,
		"state":    state,
		"details":  resp,
	}

	fallback := fmt.Sprintf(
		"Chat updated\nchat_jid: %s\nstate: %s\nstatus: %v",
		chatJID,
		state,
		normalizeEnvelopeStatus(resp.Status),
	)
	return newStandardToolResult(
		"whatsapp_archive_chat",
		resp.Status,
		req,
		resultPayload,
		fallback,
	), nil
}

func buildContactsResultPayload(resp domainUser.MyListContactsResponse) map[string]any {
	items := make([]map[string]any, 0, len(resp.Data))
	for _, contact := range resp.Data {
		name := strings.TrimSpace(contact.Name)
		if name == "" {
			name = "(no name)"
		}

		items = append(items, map[string]any{
			"jid":  contact.JID.String(),
			"name": name,
		})
	}

	return map[string]any{
		"items": items,
		"count": len(items),
	}
}

func buildChatsResultPayload(resp domainChat.ListChatsResponse) map[string]any {
	items := make([]map[string]any, 0, len(resp.Data))
	for _, chat := range resp.Data {
		name := strings.TrimSpace(chat.Name)
		if name == "" {
			name = "(no name)"
		}

		items = append(items, map[string]any{
			"jid":                  chat.JID,
			"name":                 name,
			"archived":             chat.Archived,
			"last_message_time":    chat.LastMessageTime,
			"ephemeral_expiration": chat.EphemeralExpiration,
			"created_at":           chat.CreatedAt,
			"updated_at":           chat.UpdatedAt,
		})
	}

	return map[string]any{
		"items":      items,
		"count":      len(items),
		"pagination": resp.Pagination,
	}
}

func buildChatMessagesResultPayload(resp domainChat.GetChatMessagesResponse) map[string]any {
	messages := make([]map[string]any, 0, len(resp.Data))
	for _, msg := range resp.Data {
		messages = append(messages, map[string]any{
			"message_id":  msg.ID,
			"chat_jid":    msg.ChatJID,
			"sender_jid":  msg.SenderJID,
			"from_me":     msg.IsFromMe,
			"timestamp":   msg.Timestamp,
			"content":     msg.Content,
			"media_type":  msg.MediaType,
			"filename":    msg.Filename,
			"media_url":   msg.URL,
			"file_length": msg.FileLength,
			"created_at":  msg.CreatedAt,
			"updated_at":  msg.UpdatedAt,
		})
	}

	chatName := strings.TrimSpace(resp.ChatInfo.Name)
	if chatName == "" {
		chatName = "(no chat name)"
	}

	return map[string]any{
		"chat": map[string]any{
			"jid":  resp.ChatInfo.JID,
			"name": chatName,
		},
		"items":      messages,
		"count":      len(messages),
		"pagination": resp.Pagination,
	}
}

func sortMessagesChronological(messages []domainChat.MessageInfo) []domainChat.MessageInfo {
	ordered := make([]domainChat.MessageInfo, len(messages))
	copy(ordered, messages)

	sort.SliceStable(ordered, func(i, j int) bool {
		leftTime, leftErr := time.Parse(time.RFC3339, ordered[i].Timestamp)
		rightTime, rightErr := time.Parse(time.RFC3339, ordered[j].Timestamp)

		if leftErr == nil && rightErr == nil {
			return leftTime.Before(rightTime)
		}
		if leftErr == nil && rightErr != nil {
			return true
		}
		if leftErr != nil && rightErr == nil {
			return false
		}
		return ordered[i].Timestamp < ordered[j].Timestamp
	})

	return ordered
}

func truncateLines(content string, maxLines int) string {
	if maxLines <= 0 {
		return ""
	}

	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}

	preview := append([]string{}, lines[:maxLines]...)
	preview = append(preview, fmt.Sprintf("... (%d lines omitted; full text in result.timeline_markdown)", len(lines)-maxLines))
	return strings.Join(preview, "\n")
}

func buildListContactsFallback(resp domainUser.MyListContactsResponse) string {
	const maxPreview = 20

	total := len(resp.Data)
	if total == 0 {
		return "No contacts found."
	}

	previewCount := total
	if previewCount > maxPreview {
		previewCount = maxPreview
	}

	lines := make([]string, 0, previewCount+2)
	lines = append(lines, fmt.Sprintf("Found %d contacts:", total))

	for i := 0; i < previewCount; i++ {
		contact := resp.Data[i]
		name := strings.TrimSpace(contact.Name)
		if name == "" {
			name = "(no name)"
		}
		lines = append(lines, fmt.Sprintf("%d. %s | %s", i+1, name, contact.JID.String()))
	}

	if total > previewCount {
		lines = append(lines, fmt.Sprintf("...and %d more contacts.", total-previewCount))
	}

	return strings.Join(lines, "\n")
}

func buildGetChatMessagesFallback(resp domainChat.GetChatMessagesResponse, req domainChat.GetChatMessagesRequest) string {
	const maxPreview = 10

	totalOnPage := len(resp.Data)
	totalAvailable := resp.Pagination.Total
	chatName := strings.TrimSpace(resp.ChatInfo.Name)
	if chatName == "" {
		chatName = "(no chat name)"
	}

	lines := make([]string, 0, maxPreview+8)
	lines = append(lines, fmt.Sprintf(
		"Chat: %s | %s\nMessages in page: %d (offset %d, limit %d, total %d)",
		chatName,
		req.ChatJID,
		totalOnPage,
		req.Offset,
		req.Limit,
		totalAvailable,
	))

	if req.Search != "" {
		lines = append(lines, fmt.Sprintf("Search: %q", req.Search))
	}
	if req.MediaOnly {
		lines = append(lines, "Filter: media_only=true")
	}
	if req.IsFromMe != nil {
		lines = append(lines, fmt.Sprintf("Filter: is_from_me=%t", *req.IsFromMe))
	}
	if req.StartTime != nil && *req.StartTime != "" {
		lines = append(lines, fmt.Sprintf("Filter: start_time=%s", *req.StartTime))
	}
	if req.EndTime != nil && *req.EndTime != "" {
		lines = append(lines, fmt.Sprintf("Filter: end_time=%s", *req.EndTime))
	}

	if totalOnPage == 0 {
		lines = append(lines, "No messages found for the provided filters.")
		return strings.Join(lines, "\n")
	}

	previewCount := totalOnPage
	if previewCount > maxPreview {
		previewCount = maxPreview
	}

	lines = append(lines, "Preview:")
	for i := 0; i < previewCount; i++ {
		msg := resp.Data[i]
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			if strings.TrimSpace(msg.MediaType) != "" {
				content = fmt.Sprintf("[media:%s]", msg.MediaType)
			} else {
				content = "(empty)"
			}
		}
		content = truncateRunes(content, 90)
		lines = append(lines, fmt.Sprintf(
			"%d. %s | %s | from_me=%t | id=%s | %s",
			i+1,
			msg.Timestamp,
			msg.SenderJID,
			msg.IsFromMe,
			msg.ID,
			content,
		))
	}

	if totalOnPage > previewCount {
		lines = append(lines, fmt.Sprintf("...and %d more messages in this page.", totalOnPage-previewCount))
	}

	return strings.Join(lines, "\n")
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}

	r := []rune(s)
	if len(r) <= max {
		return s
	}

	if max <= 3 {
		return string(r[:max])
	}

	return string(r[:max-3]) + "..."
}
