package mcp

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainMessage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/message"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	exportTypeFull                = "full"
	exportTypePartial             = "partial"
	defaultPartialExportCount     = 200
	maxPartialExportCount         = 5000
	exportFetchPageSize           = 100
	exportHumanTextFilename       = "chat_export_human.txt"
	exportLLMJSONFilename         = "chat_export_llm.json"
	exportLLMMarkdownFilename     = "chat_export_llm.md"
	exportBundleArchiveFilename   = "chat_export_bundle.zip"
	exportMediaDirectoryName      = "media"
	exportStructuredFormatVersion = "chat_export_bundle_v2"
)

var exportUnsafeFilenameChars = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

type chatExportOptions struct {
	ChatJID       string
	ExportType    string
	MessageCount  int
	Offset        int
	OutputDir     string
	IncludeMedia  bool
	MediaOnly     bool
	IsFromMe      *bool
	Search        string
	StartTimeRFC  *string
	EndTimeRFC    *string
	RequestedDate string
}

type chatExportCollected struct {
	ChatInfo domainChat.ChatInfo
	Messages []domainChat.MessageInfo
}

type chatExportPreparedMessage struct {
	Seq          int
	Message      domainChat.MessageInfo
	Sender       map[string]string
	Text         string
	MediaType    string
	LocalHuman   string
	LocalRFC3339 string
	UTCRFC3339   string
	Media        *chatExportPreparedMedia
}

type chatExportPreparedMedia struct {
	Ref                 string
	ArchiveFilename     string
	OriginalFilename    string
	FilenameSource      string
	ArchivePath         string
	Record              map[string]any
	PendingBatchRetry   bool
	BatchRetryAttempted bool
	IncludedAfterBatch  bool
}

type chatExportMediaStats struct {
	Found                  int
	Included               int
	Failed                 int
	PendingRecovery        int
	DownloadedDirectURL    int
	RecoveredViaDirectPath int
	RecoveredViaRetry      int
	RecoveredAfterBatch    int
	UnavailableOnPhone     int
	RetryTimeouts          int
}

func parseChatExportOptions(request mcp.CallToolRequest) (chatExportOptions, map[string]any, error) {
	chatJID, err := request.RequireString("chat_jid")
	if err != nil {
		return chatExportOptions{}, nil, err
	}

	args := request.GetArguments()

	exportType := strings.ToLower(strings.TrimSpace(request.GetString("export_type", exportTypePartial)))
	if exportType == "" {
		exportType = exportTypePartial
	}
	if exportType != exportTypeFull && exportType != exportTypePartial {
		return chatExportOptions{}, nil, fmt.Errorf("invalid export_type %q: expected 'full' or 'partial'", exportType)
	}

	messageCount := request.GetInt("message_count", request.GetInt("limit", defaultPartialExportCount))
	if messageCount <= 0 {
		messageCount = defaultPartialExportCount
	}
	if exportType == exportTypePartial && messageCount > maxPartialExportCount {
		messageCount = maxPartialExportCount
	}

	includeMedia := false
	mediaOnly := false
	var isFromMePtr *bool

	if args != nil {
		if value, ok := args["include_media"]; ok {
			includeMedia, err = toBool(value)
			if err != nil {
				return chatExportOptions{}, nil, err
			}
		}
		if value, ok := args["media_only"]; ok {
			mediaOnly, err = toBool(value)
			if err != nil {
				return chatExportOptions{}, nil, err
			}
		}
		if value, ok := args["is_from_me"]; ok {
			parsed, parseErr := toBool(value)
			if parseErr != nil {
				return chatExportOptions{}, nil, parseErr
			}
			isFromMePtr = &parsed
		}
	}

	dateValue := strings.TrimSpace(request.GetString("date", ""))
	dateFrom := strings.TrimSpace(request.GetString("date_from", request.GetString("start_time", "")))
	dateTo := strings.TrimSpace(request.GetString("date_to", request.GetString("end_time", "")))

	startRFC, endRFC, err := resolveExportTimeFilters(dateValue, dateFrom, dateTo, time.Now().Location())
	if err != nil {
		return chatExportOptions{}, nil, err
	}

	search := strings.TrimSpace(request.GetString("search", ""))
	if exportType == exportTypeFull && search != "" {
		return chatExportOptions{}, nil, fmt.Errorf("full export with search is not supported by the current chat storage pagination; use export_type='partial' with message_count instead")
	}

	options := chatExportOptions{
		ChatJID:       strings.TrimSpace(chatJID),
		ExportType:    exportType,
		MessageCount:  messageCount,
		Offset:        request.GetInt("offset", 0),
		OutputDir:     strings.TrimSpace(request.GetString("output_dir", "")),
		IncludeMedia:  includeMedia,
		MediaOnly:     mediaOnly,
		IsFromMe:      isFromMePtr,
		Search:        search,
		StartTimeRFC:  startRFC,
		EndTimeRFC:    endRFC,
		RequestedDate: dateValue,
	}

	requestPayload := map[string]any{
		"chat_jid":      options.ChatJID,
		"export_type":   options.ExportType,
		"message_count": options.MessageCount,
		"offset":        options.Offset,
		"output_dir":    options.OutputDir,
		"include_media": options.IncludeMedia,
		"media_only":    options.MediaOnly,
		"search":        options.Search,
	}
	if options.IsFromMe != nil {
		requestPayload["is_from_me"] = *options.IsFromMe
	}
	if options.RequestedDate != "" {
		requestPayload["date"] = options.RequestedDate
	}
	if options.StartTimeRFC != nil && strings.TrimSpace(*options.StartTimeRFC) != "" {
		requestPayload["date_from_resolved"] = *options.StartTimeRFC
	}
	if options.EndTimeRFC != nil && strings.TrimSpace(*options.EndTimeRFC) != "" {
		requestPayload["date_to_resolved"] = *options.EndTimeRFC
	}

	return options, requestPayload, nil
}

func resolveExportTimeFilters(dateValue, dateFrom, dateTo string, loc *time.Location) (*string, *string, error) {
	if loc == nil {
		loc = time.UTC
	}

	if strings.TrimSpace(dateValue) != "" {
		dayStart, err := parseFlexibleExportTime(dateValue, loc, false)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid date %q: %w", dateValue, err)
		}
		dayEnd := time.Date(dayStart.Year(), dayStart.Month(), dayStart.Day(), 23, 59, 59, 0, dayStart.Location())
		start := dayStart.Format(time.RFC3339)
		end := dayEnd.Format(time.RFC3339)
		return &start, &end, nil
	}

	var startRFC *string
	if strings.TrimSpace(dateFrom) != "" {
		start, err := parseFlexibleExportTime(dateFrom, loc, false)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid date_from %q: %w", dateFrom, err)
		}
		formatted := start.Format(time.RFC3339)
		startRFC = &formatted
	}

	var endRFC *string
	if strings.TrimSpace(dateTo) != "" {
		end, err := parseFlexibleExportTime(dateTo, loc, true)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid date_to %q: %w", dateTo, err)
		}
		formatted := end.Format(time.RFC3339)
		endRFC = &formatted
	}

	return startRFC, endRFC, nil
}

func parseFlexibleExportTime(raw string, loc *time.Location, endOfDay bool) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, fmt.Errorf("empty date/time")
	}
	if loc == nil {
		loc = time.UTC
	}

	timezoneAwareLayouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04Z07:00",
	}
	for _, layout := range timezoneAwareLayouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed, nil
		}
	}

	localLayouts := []struct {
		layout   string
		hasClock bool
	}{
		{layout: "2006-01-02", hasClock: false},
		{layout: "02-01-2006", hasClock: false},
		{layout: "02-01-06", hasClock: false},
		{layout: "2006-01-02 15:04", hasClock: true},
		{layout: "2006-01-02 15:04:05", hasClock: true},
		{layout: "2006-01-02T15:04", hasClock: true},
		{layout: "2006-01-02T15:04:05", hasClock: true},
		{layout: "02-01-2006 15:04", hasClock: true},
		{layout: "02-01-2006 15:04:05", hasClock: true},
		{layout: "02-01-2006:15:04:05", hasClock: true},
		{layout: "02-01-06 15:04", hasClock: true},
		{layout: "02-01-06 15:04:05", hasClock: true},
		{layout: "02-01-06:15:04:05", hasClock: true},
	}
	for _, candidate := range localLayouts {
		if parsed, err := time.ParseInLocation(candidate.layout, trimmed, loc); err == nil {
			if !candidate.hasClock && endOfDay {
				return time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 23, 59, 59, 0, loc), nil
			}
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported date/time format")
}

func (h *QueryHandler) collectMessagesForExport(ctx context.Context, options chatExportOptions, report chatExportProgressReporter) (chatExportCollected, error) {
	baseRequest := domainChat.GetChatMessagesRequest{
		ChatJID:   options.ChatJID,
		Offset:    options.Offset,
		StartTime: options.StartTimeRFC,
		EndTime:   options.EndTimeRFC,
		MediaOnly: options.MediaOnly,
		IsFromMe:  options.IsFromMe,
		Search:    options.Search,
	}

	var collected chatExportCollected
	var allMessages []domainChat.MessageInfo
	offset := options.Offset
	firstPage := true

	for {
		pageSize := exportFetchPageSize
		if options.ExportType == exportTypePartial {
			remaining := options.MessageCount - len(allMessages)
			if remaining <= 0 {
				break
			}
			if remaining < pageSize {
				pageSize = remaining
			}
		}

		req := baseRequest
		req.Offset = offset
		req.Limit = pageSize

		resp, err := h.chatService.GetChatMessages(ctx, req)
		if err != nil {
			return collected, err
		}

		if firstPage {
			collected.ChatInfo = resp.ChatInfo
			firstPage = false
		}

		if len(resp.Data) == 0 {
			break
		}

		allMessages = append(allMessages, resp.Data...)
		offset += len(resp.Data)

		reportChatExportProgress(report, chatExportProgressSnapshot{
			Phase:         "collecting_messages",
			StatusMessage: fmt.Sprintf("collected %d messages", len(allMessages)),
			Chat: map[string]any{
				"jid":  strings.TrimSpace(collected.ChatInfo.JID),
				"name": strings.TrimSpace(collected.ChatInfo.Name),
			},
			Counters: map[string]any{
				"messages_collected": len(allMessages),
			},
		})

		if options.ExportType == exportTypePartial && len(allMessages) >= options.MessageCount {
			allMessages = allMessages[:options.MessageCount]
			break
		}

		if len(resp.Data) < pageSize {
			break
		}
	}

	collected.Messages = sortMessagesChronological(allMessages)
	return collected, nil
}

func (h *QueryHandler) generateLocalChatExport(
	ctx context.Context,
	options chatExportOptions,
	collected chatExportCollected,
	report chatExportProgressReporter,
) (map[string]any, string, error) {
	loc := time.Now().Location()
	if loc == nil {
		loc = time.UTC
	}

	chatName := strings.TrimSpace(collected.ChatInfo.Name)
	if chatName == "" {
		chatName = "(no chat name)"
	}

	exportDir, err := makeChatExportDir(options.OutputDir, chatName, options.ChatJID)
	if err != nil {
		return nil, "", err
	}

	humanTextPath := filepath.Join(exportDir, exportHumanTextFilename)
	llmJSONPath := filepath.Join(exportDir, exportLLMJSONFilename)
	llmMarkdownPath := filepath.Join(exportDir, exportLLMMarkdownFilename)
	archivePath := filepath.Join(exportDir, exportBundleArchiveFilename)
	absoluteFiles := resolveExportProgressFiles(exportDir, humanTextPath, llmJSONPath, llmMarkdownPath, archivePath, options.IncludeMedia)

	senderLookup, ownDisplayName := buildExportSenderLookup(ctx)
	orderedMessages := sortMessagesChronological(collected.Messages)
	usedArchiveFilenames := map[string]struct{}{}

	reportChatExportProgress(report, chatExportProgressSnapshot{
		Phase:         "building_export_files",
		StatusMessage: fmt.Sprintf("preparing export bundle for %d messages", len(orderedMessages)),
		Chat: map[string]any{
			"jid":  options.ChatJID,
			"name": chatName,
		},
		Counters: map[string]any{
			"messages_collected": len(orderedMessages),
		},
		Files: absoluteFiles,
	})

	humanLines := make([]string, 0, len(orderedMessages))
	markdownLines := []string{
		fmt.Sprintf("# Chat Export: %s", chatName),
		"",
		fmt.Sprintf("**Gerado em (local):** %s", time.Now().In(loc).Format(time.RFC3339)),
		fmt.Sprintf("**Gerado em (UTC):** %s", time.Now().UTC().Format(time.RFC3339)),
		fmt.Sprintf("**Total de mensagens:** %d", len(orderedMessages)),
		fmt.Sprintf("**Tipo de export:** %s", options.ExportType),
		fmt.Sprintf("**Chat:** %s", options.ChatJID),
		"",
		"---",
		"",
		"# Chat Export for LLM",
		fmt.Sprintf("chat: %s | %s", chatName, options.ChatJID),
		fmt.Sprintf("messages_exported: %d", len(orderedMessages)),
	}

	mediaDir := filepath.Join(exportDir, exportMediaDirectoryName)
	if options.IncludeMedia {
		if err := os.MkdirAll(mediaDir, 0o700); err != nil {
			return nil, "", fmt.Errorf("failed to create media export directory: %w", err)
		}
	}

	preparedMessages := make([]chatExportPreparedMessage, 0, len(orderedMessages))
	firstMessageLocal := ""
	lastMessageLocal := ""
	firstPassStats := chatExportMediaStats{}

	for index, msg := range orderedMessages {
		sentAt := parseStoredMessageTime(msg.Timestamp)
		localTime := sentAt.In(loc)
		localHuman := formatExportHumanTimestamp(localTime)
		localRFC3339 := localTime.Format(time.RFC3339)
		utcRFC3339 := sentAt.UTC().Format(time.RFC3339)

		if firstMessageLocal == "" {
			firstMessageLocal = localRFC3339
		}
		lastMessageLocal = localRFC3339

		text := strings.TrimSpace(msg.Content)
		mediaType := strings.TrimSpace(msg.MediaType)
		prepared := chatExportPreparedMessage{
			Seq:          index + 1,
			Message:      msg,
			Sender:       buildExportSenderInfo(msg, collected.ChatInfo, senderLookup, ownDisplayName),
			Text:         text,
			MediaType:    mediaType,
			LocalHuman:   localHuman,
			LocalRFC3339: localRFC3339,
			UTCRFC3339:   utcRFC3339,
		}

		if mediaType != "" {
			firstPassStats.Found++
			mediaRef := fmt.Sprintf("media_%04d", firstPassStats.Found)
			archiveFilename, originalFilename, filenameSource := resolveMediaArchiveFilename(msg, usedArchiveFilenames)
			archivePath := filepath.ToSlash(filepath.Join(exportMediaDirectoryName, archiveFilename))
			prepared.Media = &chatExportPreparedMedia{
				Ref:              mediaRef,
				ArchiveFilename:  archiveFilename,
				OriginalFilename: originalFilename,
				FilenameSource:   filenameSource,
				ArchivePath:      archivePath,
				Record: map[string]any{
					"message_id":               msg.ID,
					"ref":                      mediaRef,
					"type":                     mediaType,
					"type_human":               humanMediaType(mediaType),
					"original_filename":        originalFilename,
					"archive_filename":         archiveFilename,
					"archive_path":             archivePath,
					"filename_source":          filenameSource,
					"remote_url":               msg.URL,
					"size_bytes":               msg.FileLength,
					"included_in_bundle":       false,
					"download_status":          "not_requested",
					"recovery_method":          domainMessage.MediaRecoveryMethodNone,
					"failure_reason":           domainMessage.MediaFailureReasonNone,
					"batch_recovery_attempted": false,
				},
			}

			if text != "" {
				prepared.Media.Record["caption_or_text"] = text
			}

			if options.IncludeMedia {
				downloadResp, archiveRelative, archiveAbsolute, sourcePath, includeErr := h.includeMediaInExport(ctx, msg, options.ChatJID, mediaDir, archiveFilename)
				applyPreparedMediaDownloadOutcome(prepared.Media, downloadResp, archiveRelative, archiveAbsolute, sourcePath, includeErr, false)
				if shouldQueuePreparedMediaBatchRetry(prepared.Media, includeErr) {
					prepared.Media.PendingBatchRetry = true
					firstPassStats.PendingRecovery++
				}
				if wasIncludedPreparedMedia(prepared.Media) {
					firstPassStats.Included++
					switch getPreparedMediaRecordString(prepared.Media, "recovery_method") {
					case domainMessage.MediaRecoveryMethodDirectURL:
						firstPassStats.DownloadedDirectURL++
					case domainMessage.MediaRecoveryMethodStoredDirectPath:
						firstPassStats.RecoveredViaDirectPath++
					case domainMessage.MediaRecoveryMethodMediaRetry:
						firstPassStats.RecoveredViaRetry++
					}
				} else if getPreparedMediaRecordString(prepared.Media, "download_status") == "failed" {
					firstPassStats.Failed++
					switch getPreparedMediaRecordString(prepared.Media, "failure_reason") {
					case domainMessage.MediaFailureReasonNotAvailableOnPhone:
						firstPassStats.UnavailableOnPhone++
					case domainMessage.MediaFailureReasonRetryTimeout:
						firstPassStats.RetryTimeouts++
					}
				}

				reportChatExportProgress(report, chatExportProgressSnapshot{
					Phase:         "downloading_media",
					StatusMessage: fmt.Sprintf("processed %d media items", firstPassStats.Found),
					Chat: map[string]any{
						"jid":  options.ChatJID,
						"name": chatName,
					},
					Counters: buildChatExportProgressCounters(len(orderedMessages), firstPassStats),
					Files:    absoluteFiles,
				})
			}
		}

		preparedMessages = append(preparedMessages, prepared)
	}

	if options.IncludeMedia {
		pendingMessageIDs := make([]string, 0)
		for _, prepared := range preparedMessages {
			if prepared.Media != nil && prepared.Media.PendingBatchRetry {
				pendingMessageIDs = append(pendingMessageIDs, prepared.Message.ID)
			}
		}

		if len(pendingMessageIDs) > 0 {
			reportChatExportProgress(report, chatExportProgressSnapshot{
				Phase:         "recovering_media_batch",
				StatusMessage: fmt.Sprintf("requesting refreshed media paths for %d items", len(pendingMessageIDs)),
				Chat: map[string]any{
					"jid":  options.ChatJID,
					"name": chatName,
				},
				Counters: buildChatExportProgressCounters(len(orderedMessages), summarizePreparedMedia(preparedMessages)),
				Files:    absoluteFiles,
			})

			batchResponse, batchErr := h.messageService.RecoverMediaBatch(ctx, domainMessage.RecoverMediaBatchRequest{
				Phone:      options.ChatJID,
				MessageIDs: pendingMessageIDs,
			})
			batchByID := make(map[string]domainMessage.RecoverMediaBatchItem, len(batchResponse.Items))
			for _, item := range batchResponse.Items {
				batchByID[item.MessageID] = item
			}

			recoveryPass := 0
			for index := range preparedMessages {
				prepared := &preparedMessages[index]
				if prepared.Media == nil || !prepared.Media.PendingBatchRetry {
					continue
				}

				prepared.Media.BatchRetryAttempted = true
				prepared.Media.Record["batch_recovery_attempted"] = true
				if batchErr != nil {
					prepared.Media.Record["batch_recovery_error"] = batchErr.Error()
				}

				if batchItem, ok := batchByID[prepared.Message.ID]; ok {
					if strings.TrimSpace(batchItem.RecoveryMethod) != "" {
						prepared.Media.Record["batch_recovery_method"] = batchItem.RecoveryMethod
					}
					if strings.TrimSpace(batchItem.UpdatedDirectPath) != "" {
						prepared.Media.Record["batch_recovery_status"] = "direct_path_refreshed"
					} else if strings.TrimSpace(batchItem.FailureReason) != "" {
						prepared.Media.Record["batch_recovery_status"] = batchItem.FailureReason
						prepared.Media.Record["failure_reason"] = batchItem.FailureReason
					}
				}

				if getPreparedMediaRecordString(prepared.Media, "failure_reason") == domainMessage.MediaFailureReasonNotAvailableOnPhone {
					prepared.Media.PendingBatchRetry = false
					recoveryPass++
					reportChatExportProgress(report, chatExportProgressSnapshot{
						Phase:         "recovering_media_batch",
						StatusMessage: fmt.Sprintf("processed %d/%d pending media recoveries", recoveryPass, len(pendingMessageIDs)),
						Chat: map[string]any{
							"jid":  options.ChatJID,
							"name": chatName,
						},
						Counters: buildChatExportProgressCounters(len(orderedMessages), summarizePreparedMedia(preparedMessages)),
						Files:    absoluteFiles,
					})
					continue
				}

				downloadResp, archiveRelative, archiveAbsolute, sourcePath, includeErr := h.includeMediaInExport(ctx, prepared.Message, options.ChatJID, mediaDir, prepared.Media.ArchiveFilename)
				recoveredAfterBatch := getPreparedMediaRecordString(prepared.Media, "batch_recovery_status") == "direct_path_refreshed"
				applyPreparedMediaDownloadOutcome(prepared.Media, downloadResp, archiveRelative, archiveAbsolute, sourcePath, includeErr, recoveredAfterBatch)
				prepared.Media.PendingBatchRetry = false
				recoveryPass++

				reportChatExportProgress(report, chatExportProgressSnapshot{
					Phase:         "recovering_media_batch",
					StatusMessage: fmt.Sprintf("processed %d/%d pending media recoveries", recoveryPass, len(pendingMessageIDs)),
					Chat: map[string]any{
						"jid":  options.ChatJID,
						"name": chatName,
					},
					Counters: buildChatExportProgressCounters(len(orderedMessages), summarizePreparedMedia(preparedMessages)),
					Files:    absoluteFiles,
				})
			}
		}
	}

	llmMessages := make([]map[string]any, 0, len(preparedMessages))
	mediaIndex := make([]map[string]any, 0)

	for _, prepared := range preparedMessages {
		messageRecord := map[string]any{
			"seq":        prepared.Seq,
			"message_id": prepared.Message.ID,
			"chat_jid":   prepared.Message.ChatJID,
			"sent_at": map[string]any{
				"raw":         prepared.Message.Timestamp,
				"utc":         prepared.UTCRFC3339,
				"local":       prepared.LocalRFC3339,
				"local_human": prepared.LocalHuman,
				"timezone":    loc.String(),
			},
			"sender":  prepared.Sender,
			"from_me": prepared.Message.IsFromMe,
			"text":    prepared.Text,
		}

		markdownLines = append(markdownLines, fmt.Sprintf(
			"%d. [%s] sender=%s (%s) from_me=%t id=%s",
			prepared.Seq,
			prepared.LocalHuman,
			prepared.Sender["label"],
			prepared.Sender["identity"],
			prepared.Message.IsFromMe,
			prepared.Message.ID,
		))

		if prepared.Media == nil {
			text := prepared.Text
			if text == "" {
				text = "(mensagem sem texto)"
			}
			humanLines = append(humanLines, fmt.Sprintf(
				"%s - %s (%s): %s",
				prepared.LocalHuman,
				prepared.Sender["label"],
				prepared.Sender["identity"],
				text,
			))
			markdownLines = append(markdownLines, "   text: "+truncateRunes(text, 260))
			messageRecord["content"] = map[string]any{
				"kind": "text",
				"text": text,
			}
			llmMessages = append(llmMessages, messageRecord)
			continue
		}

		mediaRecord := prepared.Media.Record
		humanLine := fmt.Sprintf(
			"%s - %s (%s): midia(%s) enviada [ref=%s]: %s",
			prepared.LocalHuman,
			prepared.Sender["label"],
			prepared.Sender["identity"],
			humanMediaType(prepared.MediaType),
			prepared.Media.Ref,
			prepared.Media.ArchiveFilename,
		)
		if prepared.Text != "" {
			humanLine += " | legenda: " + prepared.Text
		}
		if method := getPreparedMediaRecordString(prepared.Media, "recovery_method"); method != "" &&
			method != domainMessage.MediaRecoveryMethodNone &&
			method != domainMessage.MediaRecoveryMethodDirectURL {
			humanLine += " | recuperacao: " + method
		}
		if reason := getPreparedMediaRecordString(prepared.Media, "failure_reason"); reason != "" {
			humanLine += " | arquivo_nao_incluido: " + reason
		}
		humanLines = append(humanLines, humanLine)

		markdownLines = append(markdownLines, fmt.Sprintf(
			"   media: ref=%s message_id=%s type=%s archive_filename=%s",
			prepared.Media.Ref,
			prepared.Message.ID,
			prepared.MediaType,
			prepared.Media.ArchiveFilename,
		))
		if includedPath := getPreparedMediaRecordString(prepared.Media, "archive_path"); includedPath != "" {
			markdownLines = append(markdownLines, "   archive_path: "+includedPath)
		}
		if method := getPreparedMediaRecordString(prepared.Media, "recovery_method"); method != "" {
			markdownLines = append(markdownLines, "   recovery_method: "+method)
		}
		if reason := getPreparedMediaRecordString(prepared.Media, "failure_reason"); reason != "" {
			markdownLines = append(markdownLines, "   failure_reason: "+reason)
		}
		if status := getPreparedMediaRecordString(prepared.Media, "download_status"); status != "" {
			markdownLines = append(markdownLines, "   download_status: "+status)
		}
		if prepared.Text != "" {
			markdownLines = append(markdownLines, "   text: "+truncateRunes(prepared.Text, 260))
		}

		messageRecord["content"] = map[string]any{
			"kind": "media",
			"text": prepared.Text,
		}
		messageRecord["media"] = mediaRecord
		llmMessages = append(llmMessages, messageRecord)

		mediaIndex = append(mediaIndex, map[string]any{
			"ref":                        prepared.Media.Ref,
			"seq":                        prepared.Seq,
			"message_id":                 prepared.Message.ID,
			"chat_jid":                   prepared.Message.ChatJID,
			"sent_at_local":              prepared.LocalRFC3339,
			"sender":                     prepared.Sender["identity"],
			"type":                       prepared.MediaType,
			"type_human":                 humanMediaType(prepared.MediaType),
			"archive_filename":           prepared.Media.ArchiveFilename,
			"archive_path":               mediaRecord["archive_path"],
			"filename_source":            prepared.Media.FilenameSource,
			"included":                   mediaRecord["included_in_bundle"],
			"download_status":            mediaRecord["download_status"],
			"recovery_method":            mediaRecord["recovery_method"],
			"failure_reason":             mediaRecord["failure_reason"],
			"batch_recovery_attempted":   mediaRecord["batch_recovery_attempted"],
			"included_after_batch_retry": prepared.Media.IncludedAfterBatch,
		})
	}

	finalStats := summarizePreparedMedia(preparedMessages)
	humanTextContent := strings.Join(humanLines, "\n")
	markdownContent := strings.Join(markdownLines, "\n")

	jsonPayload := map[string]any{
		"format":             exportStructuredFormatVersion,
		"generated_at_utc":   time.Now().UTC().Format(time.RFC3339),
		"generated_at_local": time.Now().In(loc).Format(time.RFC3339),
		"timezone":           loc.String(),
		"chat": map[string]any{
			"jid":  options.ChatJID,
			"name": chatName,
		},
		"export": map[string]any{
			"type":          options.ExportType,
			"include_media": options.IncludeMedia,
			"message_count": options.MessageCount,
			"offset":        options.Offset,
			"output_dir":    options.OutputDir,
			"media_only":    options.MediaOnly,
			"search":        options.Search,
			"date":          options.RequestedDate,
			"date_from":     derefString(options.StartTimeRFC),
			"date_to":       derefString(options.EndTimeRFC),
		},
		"stats": map[string]any{
			"messages_exported":                       len(llmMessages),
			"media_messages_found":                    finalStats.Found,
			"media_files_included_in_archive":         finalStats.Included,
			"media_files_failed":                      finalStats.Failed,
			"media_pending_recovery":                  finalStats.PendingRecovery,
			"media_files_downloaded_via_direct_url":   finalStats.DownloadedDirectURL,
			"media_files_recovered_via_direct_path":   finalStats.RecoveredViaDirectPath,
			"media_files_recovered_via_retry":         finalStats.RecoveredViaRetry,
			"media_files_recovered_after_batch_retry": finalStats.RecoveredAfterBatch,
			"media_files_unavailable_on_phone":        finalStats.UnavailableOnPhone,
			"media_retry_timeouts":                    finalStats.RetryTimeouts,
			"first_message_at_local":                  firstMessageLocal,
			"last_message_at_local":                   lastMessageLocal,
		},
		"files": map[string]any{
			"human_txt":    humanTextPath,
			"llm_json":     llmJSONPath,
			"llm_markdown": llmMarkdownPath,
			"archive_zip":  archivePath,
		},
		"messages":          llmMessages,
		"media_index":       mediaIndex,
		"timeline_markdown": markdownContent,
		"human_txt_preview": truncateLines(humanTextContent, 20),
	}

	reportChatExportProgress(report, chatExportProgressSnapshot{
		Phase:         "writing_txt",
		StatusMessage: "writing human-readable TXT export",
		Chat: map[string]any{
			"jid":  options.ChatJID,
			"name": chatName,
		},
		Counters: buildChatExportProgressCounters(len(llmMessages), finalStats),
		Files:    absoluteFiles,
	})
	if err := writeExportFile(humanTextPath, humanTextContent); err != nil {
		return nil, "", err
	}

	reportChatExportProgress(report, chatExportProgressSnapshot{
		Phase:         "writing_markdown",
		StatusMessage: "writing Markdown timeline export",
		Chat: map[string]any{
			"jid":  options.ChatJID,
			"name": chatName,
		},
		Counters: buildChatExportProgressCounters(len(llmMessages), finalStats),
		Files:    absoluteFiles,
	})
	if err := writeExportFile(llmMarkdownPath, markdownContent); err != nil {
		return nil, "", err
	}

	reportChatExportProgress(report, chatExportProgressSnapshot{
		Phase:         "writing_json",
		StatusMessage: "writing structured JSON export",
		Chat: map[string]any{
			"jid":  options.ChatJID,
			"name": chatName,
		},
		Counters: buildChatExportProgressCounters(len(llmMessages), finalStats),
		Files:    absoluteFiles,
	})
	if err := writeJSONExportFile(llmJSONPath, jsonPayload); err != nil {
		return nil, "", err
	}

	reportChatExportProgress(report, chatExportProgressSnapshot{
		Phase:         "creating_zip",
		StatusMessage: "packaging ZIP bundle",
		Chat: map[string]any{
			"jid":  options.ChatJID,
			"name": chatName,
		},
		Counters: buildChatExportProgressCounters(len(llmMessages), finalStats),
		Files:    absoluteFiles,
	})
	if err := zipExportDirectory(exportDir, archivePath); err != nil {
		return nil, "", err
	}

	absoluteHumanTextPath, err := filepath.Abs(humanTextPath)
	if err != nil {
		return nil, "", err
	}
	absoluteLLMJSONPath, err := filepath.Abs(llmJSONPath)
	if err != nil {
		return nil, "", err
	}
	absoluteLLMMarkdownPath, err := filepath.Abs(llmMarkdownPath)
	if err != nil {
		return nil, "", err
	}
	absoluteArchivePath, err := filepath.Abs(archivePath)
	if err != nil {
		return nil, "", err
	}
	absoluteExportDir, err := filepath.Abs(exportDir)
	if err != nil {
		return nil, "", err
	}

	resultPayload := map[string]any{
		"format": exportStructuredFormatVersion,
		"chat": map[string]any{
			"jid":  options.ChatJID,
			"name": chatName,
		},
		"export": map[string]any{
			"type":          options.ExportType,
			"include_media": options.IncludeMedia,
			"message_count": options.MessageCount,
			"offset":        options.Offset,
			"output_dir":    options.OutputDir,
			"media_only":    options.MediaOnly,
			"search":        options.Search,
			"date":          options.RequestedDate,
			"date_from":     derefString(options.StartTimeRFC),
			"date_to":       derefString(options.EndTimeRFC),
		},
		"stats": map[string]any{
			"messages_exported":                       len(llmMessages),
			"media_messages_found":                    finalStats.Found,
			"media_files_included_in_archive":         finalStats.Included,
			"media_files_failed":                      finalStats.Failed,
			"media_pending_recovery":                  finalStats.PendingRecovery,
			"media_files_downloaded_via_direct_url":   finalStats.DownloadedDirectURL,
			"media_files_recovered_via_direct_path":   finalStats.RecoveredViaDirectPath,
			"media_files_recovered_via_retry":         finalStats.RecoveredViaRetry,
			"media_files_recovered_after_batch_retry": finalStats.RecoveredAfterBatch,
			"media_files_unavailable_on_phone":        finalStats.UnavailableOnPhone,
			"media_retry_timeouts":                    finalStats.RetryTimeouts,
			"first_message_at_local":                  firstMessageLocal,
			"last_message_at_local":                   lastMessageLocal,
		},
		"files": map[string]any{
			"export_dir":     absoluteExportDir,
			"human_txt":      absoluteHumanTextPath,
			"llm_json":       absoluteLLMJSONPath,
			"llm_markdown":   absoluteLLMMarkdownPath,
			"archive_zip":    absoluteArchivePath,
			"media_included": options.IncludeMedia,
		},
		"preview": map[string]any{
			"human_txt": truncateLines(humanTextContent, 20),
		},
	}

	reportChatExportProgress(report, chatExportProgressSnapshot{
		Phase:         "completed",
		StatusMessage: "chat export bundle completed",
		Chat: map[string]any{
			"jid":  options.ChatJID,
			"name": chatName,
		},
		Counters: buildChatExportProgressCounters(len(llmMessages), finalStats),
		Files: map[string]any{
			"export_dir":     absoluteExportDir,
			"human_txt":      absoluteHumanTextPath,
			"llm_json":       absoluteLLMJSONPath,
			"llm_markdown":   absoluteLLMMarkdownPath,
			"archive_zip":    absoluteArchivePath,
			"media_included": options.IncludeMedia,
		},
	})

	fallback := strings.Join([]string{
		fmt.Sprintf("Chat export generated\nchat: %s | %s", chatName, options.ChatJID),
		fmt.Sprintf("type: %s", options.ExportType),
		fmt.Sprintf("messages_exported: %d", len(llmMessages)),
		fmt.Sprintf("media_found: %d", finalStats.Found),
		fmt.Sprintf("media_included: %d", finalStats.Included),
		fmt.Sprintf("media_failed: %d", finalStats.Failed),
		fmt.Sprintf("media_pending_recovery: %d", finalStats.PendingRecovery),
		fmt.Sprintf("media_downloaded_via_direct_url: %d", finalStats.DownloadedDirectURL),
		fmt.Sprintf("media_recovered_via_direct_path: %d", finalStats.RecoveredViaDirectPath),
		fmt.Sprintf("media_recovered_via_retry: %d", finalStats.RecoveredViaRetry),
		fmt.Sprintf("media_recovered_after_batch_retry: %d", finalStats.RecoveredAfterBatch),
		fmt.Sprintf("media_unavailable_on_phone: %d", finalStats.UnavailableOnPhone),
		fmt.Sprintf("media_retry_timeouts: %d", finalStats.RetryTimeouts),
		fmt.Sprintf("human_txt: %s", absoluteHumanTextPath),
		fmt.Sprintf("llm_json: %s", absoluteLLMJSONPath),
		fmt.Sprintf("llm_markdown: %s", absoluteLLMMarkdownPath),
		fmt.Sprintf("archive_zip: %s", absoluteArchivePath),
		"",
		"preview:",
		truncateLines(humanTextContent, 12),
	}, "\n")

	return resultPayload, fallback, nil
}

func reportChatExportProgress(report chatExportProgressReporter, snapshot chatExportProgressSnapshot) {
	if report == nil {
		return
	}
	report(snapshot)
}

func resolveExportProgressFiles(exportDir, humanTextPath, llmJSONPath, llmMarkdownPath, archivePath string, includeMedia bool) map[string]any {
	return map[string]any{
		"export_dir":     absoluteOrOriginalPath(exportDir),
		"human_txt":      absoluteOrOriginalPath(humanTextPath),
		"llm_json":       absoluteOrOriginalPath(llmJSONPath),
		"llm_markdown":   absoluteOrOriginalPath(llmMarkdownPath),
		"archive_zip":    absoluteOrOriginalPath(archivePath),
		"media_included": includeMedia,
	}
}

func absoluteOrOriginalPath(path string) string {
	if absPath, err := filepath.Abs(path); err == nil {
		return absPath
	}
	return path
}

func applyPreparedMediaDownloadOutcome(
	media *chatExportPreparedMedia,
	resp domainMessage.DownloadMediaResponse,
	archiveRelative string,
	archiveAbsolute string,
	sourcePath string,
	includeErr error,
	includedAfterBatch bool,
) {
	if media == nil {
		return
	}

	if strings.TrimSpace(resp.RecoveryMethod) != "" {
		media.Record["recovery_method"] = resp.RecoveryMethod
	}
	if strings.TrimSpace(resp.FailureReason) != "" {
		media.Record["failure_reason"] = resp.FailureReason
	}

	if includeErr != nil {
		status := "failed"
		if media.BatchRetryAttempted {
			status = "failed_after_batch_recovery"
		}
		media.Record["included_in_bundle"] = false
		media.Record["download_status"] = status
		media.Record["download_error"] = includeErr.Error()
		return
	}

	delete(media.Record, "download_error")
	media.Record["included_in_bundle"] = true
	if includedAfterBatch {
		media.IncludedAfterBatch = true
		media.Record["download_status"] = "included_after_batch_recovery"
		media.Record["recovery_method"] = domainMessage.MediaRecoveryMethodMediaRetry
	} else {
		media.Record["download_status"] = "included"
	}
	media.Record["failure_reason"] = domainMessage.MediaFailureReasonNone
	media.Record["archive_path"] = archiveRelative
	media.Record["local_path"] = archiveAbsolute
	media.Record["download_source_path"] = sourcePath
}

func shouldQueuePreparedMediaBatchRetry(media *chatExportPreparedMedia, includeErr error) bool {
	if media == nil || includeErr == nil {
		return false
	}
	if wasIncludedPreparedMedia(media) {
		return false
	}

	switch getPreparedMediaRecordString(media, "failure_reason") {
	case domainMessage.MediaFailureReasonNoMediaMetadata, domainMessage.MediaFailureReasonNotAvailableOnPhone:
		return false
	default:
		return true
	}
}

func summarizePreparedMedia(messages []chatExportPreparedMessage) chatExportMediaStats {
	stats := chatExportMediaStats{}
	for _, prepared := range messages {
		if prepared.Media == nil {
			continue
		}

		stats.Found++
		if prepared.Media.PendingBatchRetry {
			stats.PendingRecovery++
		}

		if wasIncludedPreparedMedia(prepared.Media) {
			stats.Included++
			switch getPreparedMediaRecordString(prepared.Media, "recovery_method") {
			case domainMessage.MediaRecoveryMethodDirectURL:
				stats.DownloadedDirectURL++
			case domainMessage.MediaRecoveryMethodStoredDirectPath:
				stats.RecoveredViaDirectPath++
			case domainMessage.MediaRecoveryMethodMediaRetry:
				stats.RecoveredViaRetry++
			}
			if prepared.Media.IncludedAfterBatch {
				stats.RecoveredAfterBatch++
			}
			continue
		}

		status := getPreparedMediaRecordString(prepared.Media, "download_status")
		if strings.HasPrefix(status, "failed") {
			stats.Failed++
		}

		switch getPreparedMediaRecordString(prepared.Media, "failure_reason") {
		case domainMessage.MediaFailureReasonNotAvailableOnPhone:
			stats.UnavailableOnPhone++
		case domainMessage.MediaFailureReasonRetryTimeout:
			stats.RetryTimeouts++
		}
	}

	return stats
}

func buildChatExportProgressCounters(messageCount int, stats chatExportMediaStats) map[string]any {
	return map[string]any{
		"messages_collected":                messageCount,
		"media_found":                       stats.Found,
		"media_included":                    stats.Included,
		"media_failed":                      stats.Failed,
		"media_pending_recovery":            stats.PendingRecovery,
		"media_downloaded_via_direct_url":   stats.DownloadedDirectURL,
		"media_recovered_via_direct_path":   stats.RecoveredViaDirectPath,
		"media_recovered_via_retry":         stats.RecoveredViaRetry,
		"media_recovered_after_batch_retry": stats.RecoveredAfterBatch,
		"media_unavailable_on_phone":        stats.UnavailableOnPhone,
		"media_retry_timeouts":              stats.RetryTimeouts,
	}
}

func getPreparedMediaRecordString(media *chatExportPreparedMedia, key string) string {
	if media == nil || media.Record == nil {
		return ""
	}
	value, _ := media.Record[key].(string)
	return strings.TrimSpace(value)
}

func wasIncludedPreparedMedia(media *chatExportPreparedMedia) bool {
	if media == nil || media.Record == nil {
		return false
	}
	included, _ := media.Record["included_in_bundle"].(bool)
	return included
}

func buildExportSenderLookup(ctx context.Context) (map[string]string, string) {
	result := map[string]string{}
	client := whatsapp.ClientFromContext(ctx)
	if client == nil || client.Store == nil || client.Store.Contacts == nil {
		return result, ""
	}

	ownDisplayName := strings.TrimSpace(client.Store.PushName)

	contacts, err := client.Store.Contacts.GetAllContacts(ctx)
	if err != nil {
		return result, ownDisplayName
	}

	for jid, contact := range contacts {
		name := strings.TrimSpace(contact.FullName)
		if name == "" {
			continue
		}
		result[jid.String()] = name
		result[jid.ToNonAD().String()] = name
	}

	return result, ownDisplayName
}

func buildExportSenderInfo(
	msg domainChat.MessageInfo,
	chatInfo domainChat.ChatInfo,
	senderLookup map[string]string,
	ownDisplayName string,
) map[string]string {
	senderJID := strings.TrimSpace(msg.SenderJID)
	phone := strings.TrimSpace(utils.ExtractPhoneFromJID(senderJID))
	if phone == "" {
		phone = strings.TrimSpace(utils.ExtractPhoneNumber(senderJID))
	}

	displayName := strings.TrimSpace(senderLookup[senderJID])
	if displayName == "" && !utils.IsGroupJID(chatInfo.JID) && senderJID == strings.TrimSpace(chatInfo.JID) {
		displayName = strings.TrimSpace(chatInfo.Name)
	}

	label := displayName
	if msg.IsFromMe {
		if strings.TrimSpace(ownDisplayName) != "" {
			displayName = strings.TrimSpace(ownDisplayName)
		}
		label = "Eu"
	}

	if label == "" {
		if phone != "" {
			label = phone
		} else if senderJID != "" {
			label = senderJID
		} else {
			label = "desconhecido"
		}
	}

	identity := senderJID
	if strings.TrimSpace(displayName) != "" && phone != "" {
		identity = fmt.Sprintf("%s@%s", displayName, phone)
	} else if phone != "" {
		identity = phone
	}

	return map[string]string{
		"jid":          senderJID,
		"phone":        phone,
		"display_name": displayName,
		"label":        label,
		"identity":     identity,
	}
}

func (h *QueryHandler) includeMediaInExport(
	ctx context.Context,
	msg domainChat.MessageInfo,
	chatJID string,
	mediaDir string,
	archiveFilename string,
) (domainMessage.DownloadMediaResponse, string, string, string, error) {
	resp := domainMessage.DownloadMediaResponse{
		MessageID:      msg.ID,
		MediaType:      msg.MediaType,
		Filename:       msg.Filename,
		RecoveryMethod: domainMessage.MediaRecoveryMethodNone,
	}

	if strings.TrimSpace(msg.MediaType) == "" {
		resp.FailureReason = domainMessage.MediaFailureReasonNoMediaMetadata
		return resp, "", "", "", fmt.Errorf("message %s does not have downloadable media metadata", msg.ID)
	}

	resp, err := h.messageService.DownloadMediaForExport(ctx, domainMessage.DownloadMediaRequest{
		MessageID: msg.ID,
		Phone:     chatJID,
	})
	if err != nil {
		return resp, "", "", "", err
	}

	targetPath := filepath.Join(mediaDir, archiveFilename)
	if err := copyExportFile(resp.FilePath, targetPath); err != nil {
		return resp, "", "", "", fmt.Errorf("failed to copy media into export bundle: %w", err)
	}

	absoluteTargetPath, err := filepath.Abs(targetPath)
	if err != nil {
		return resp, "", "", "", err
	}

	return resp, filepath.ToSlash(filepath.Join(exportMediaDirectoryName, archiveFilename)), absoluteTargetPath, resp.FilePath, nil
}

func makeChatExportDir(outputDir, chatName, chatJID string) (string, error) {
	stamp := time.Now().Format("20060102_150405")
	slug := sanitizeExportSlug(chatName)
	if slug == "" {
		slug = sanitizeExportSlug(chatJID)
	}
	if slug == "" {
		slug = "chat"
	}

	baseDir, err := resolveChatExportBaseDir(outputDir)
	if err != nil {
		return "", err
	}

	rootDir := filepath.Join(baseDir, slug+"_"+sanitizeExportSlug(chatJID), stamp)
	if err := os.MkdirAll(rootDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create export directory: %w", err)
	}
	return rootDir, nil
}

func resolveChatExportBaseDir(outputDir string) (string, error) {
	return utils.ResolveBaseOutputDir(outputDir, filepath.Join(config.PathStorages, "exports"))
}

func sanitizeExportSlug(value string) string {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if trimmed == "" {
		return ""
	}
	trimmed = strings.ReplaceAll(trimmed, " ", "_")
	trimmed = exportUnsafeFilenameChars.ReplaceAllString(trimmed, "_")
	trimmed = strings.Trim(trimmed, "._-")
	if trimmed == "" {
		return ""
	}
	return trimmed
}

func resolveMediaArchiveFilename(msg domainChat.MessageInfo, used map[string]struct{}) (archiveFilename string, originalFilename string, filenameSource string) {
	originalFilename = strings.TrimSpace(msg.Filename)
	candidate := sanitizeArchiveFilenameCandidate(originalFilename)
	if candidate != "" {
		if _, exists := used[candidate]; !exists {
			used[candidate] = struct{}{}
			return candidate, originalFilename, "stored_filename"
		}

		candidate = ensureUniqueArchiveFilename(candidate, shortMessageIDSuffix(msg.ID), used)
		return candidate, originalFilename, "stored_filename_with_collision_suffix"
	}

	extension := defaultMediaExtension(strings.TrimSpace(msg.MediaType))
	candidate = "msg_" + shortMessageIDSuffix(msg.ID) + extension
	if _, exists := used[candidate]; exists {
		candidate = ensureUniqueArchiveFilename(candidate, shortMessageIDSuffix(msg.ID), used)
	} else {
		used[candidate] = struct{}{}
	}
	return candidate, "", "message_id_fallback"
}

func sanitizeArchiveFilenameCandidate(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}

	base := strings.TrimSpace(filepath.Base(trimmed))
	base = strings.ReplaceAll(base, "\x00", "")
	base = strings.Trim(base, " ./\\")
	if base == "" {
		return ""
	}
	return base
}

func ensureUniqueArchiveFilename(candidate string, suffix string, used map[string]struct{}) string {
	if suffix == "" {
		suffix = "dup"
	}

	base := candidate
	ext := filepath.Ext(candidate)
	if ext != "" {
		base = strings.TrimSuffix(candidate, ext)
	}

	withSuffix := fmt.Sprintf("%s__%s%s", base, suffix, ext)
	if _, exists := used[withSuffix]; !exists {
		used[withSuffix] = struct{}{}
		return withSuffix
	}

	for index := 2; ; index++ {
		nextCandidate := fmt.Sprintf("%s__%s_%d%s", base, suffix, index, ext)
		if _, exists := used[nextCandidate]; !exists {
			used[nextCandidate] = struct{}{}
			return nextCandidate
		}
	}
}

func shortMessageIDSuffix(messageID string) string {
	trimmed := strings.TrimSpace(strings.ToLower(messageID))
	if trimmed == "" {
		return "unknown"
	}

	var builder strings.Builder
	for _, char := range trimmed {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
		}
	}

	value := builder.String()
	if value == "" {
		return "unknown"
	}
	if len(value) > 8 {
		return value[:8]
	}
	return value
}

func defaultMediaExtension(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image":
		return ".jpg"
	case "video", "video_note":
		return ".mp4"
	case "audio", "ptt":
		return ".ogg"
	case "document":
		return ".bin"
	case "sticker":
		return ".webp"
	default:
		return ".bin"
	}
}

func humanMediaType(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(mediaType)) {
	case "image":
		return "foto"
	case "audio", "ptt":
		return "audio"
	case "document":
		return "documento"
	case "video", "video_note":
		return "video"
	case "sticker":
		return "figurinha"
	case "location", "live_location":
		return "localizacao"
	default:
		return strings.TrimSpace(mediaType)
	}
}

func parseStoredMessageTime(raw string) time.Time {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Now()
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, trimmed); err == nil {
			return parsed
		}
		if parsed, err := time.ParseInLocation(layout, trimmed, time.Now().Location()); err == nil {
			return parsed
		}
	}

	return time.Now()
}

func formatExportHumanTimestamp(ts time.Time) string {
	return ts.Format("02-01-06:15:04:05")
}

func writeExportFile(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func writeJSONExportFile(path string, payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal export JSON: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

func copyExportFile(sourcePath, destinationPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	destination, err := os.Create(destinationPath)
	if err != nil {
		return err
	}
	defer destination.Close()

	if _, err := io.Copy(destination, source); err != nil {
		return err
	}

	return destination.Chmod(0o600)
}

func zipExportDirectory(sourceDir, archivePath string) error {
	zipFile, err := os.Create(archivePath)
	if err != nil {
		return fmt.Errorf("failed to create archive: %w", err)
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Clean(path) == filepath.Clean(archivePath) {
			return nil
		}

		relativePath, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(relativePath)
		header.Method = zip.Deflate

		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			return err
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(writer, file)
		return err
	})
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
