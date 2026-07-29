package chat

// Request and Response structures for chat operations

type ListChatsRequest struct {
	Limit    int    `json:"limit" query:"limit"`
	Offset   int    `json:"offset" query:"offset"`
	Search   string `json:"search" query:"search"`
	HasMedia bool   `json:"has_media" query:"has_media"`
	Archived *bool  `json:"archived" query:"archived"`
}

type ListChatsResponse struct {
	Data       []ChatInfo         `json:"data"`
	Pagination PaginationResponse `json:"pagination"`
}

type GetChatMessagesRequest struct {
	ChatJID   string  `json:"chat_jid" uri:"chat_jid"`
	Limit     int     `json:"limit" query:"limit"`
	Offset    int     `json:"offset" query:"offset"`
	StartTime *string `json:"start_time" query:"start_time"`
	EndTime   *string `json:"end_time" query:"end_time"`
	MediaOnly bool    `json:"media_only" query:"media_only"`
	IsFromMe  *bool   `json:"is_from_me" query:"is_from_me"`
	Search    string  `json:"search" query:"search"`
}

type GetChatMessagesResponse struct {
	Data       []MessageInfo      `json:"data"`
	Pagination PaginationResponse `json:"pagination"`
	ChatInfo   ChatInfo           `json:"chat_info"`
}

// Pin Chat operations
type PinChatRequest struct {
	ChatJID string `json:"chat_jid" uri:"chat_jid"`
	Pinned  bool   `json:"pinned"`
}

type PinChatResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	ChatJID string `json:"chat_jid"`
	Pinned  bool   `json:"pinned"`
}

type ChatInfo struct {
	JID                 string `json:"jid"`
	Name                string `json:"name"`
	LastMessageTime     string `json:"last_message_time"`
	EphemeralExpiration uint32 `json:"ephemeral_expiration"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
	Archived            bool   `json:"archived"`
}

type MessageInfo struct {
	ID                string         `json:"id"`
	ChatJID           string         `json:"chat_jid"`
	SenderJID         string         `json:"sender_jid"`
	SenderDisplayName string         `json:"sender_display_name"`
	Content           string         `json:"content"`
	Timestamp         string         `json:"timestamp"`
	IsFromMe          bool           `json:"is_from_me"`
	MediaType         string         `json:"media_type"`
	Reactions         []ReactionInfo `json:"reactions,omitempty"`
	CallMetadata      string         `json:"call_metadata,omitempty"`
	Filename          string         `json:"filename"`
	URL               string         `json:"url"`
	LocalMediaPath    string         `json:"local_media_path"`
	ReplyToMessageID  string         `json:"reply_to_message_id"`
	QuotedText        string         `json:"quoted_text"`
	QuotedSenderJID   string         `json:"quoted_sender_jid"`
	FileLength        uint64         `json:"file_length"`
	DeletedAt         string         `json:"deleted_at"`
	CreatedAt         string         `json:"created_at"`
	UpdatedAt         string         `json:"updated_at"`
}

type PaginationResponse struct {
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
	Total  int `json:"total"`
}

// Disappearing Messages operations
type SetDisappearingTimerRequest struct {
	ChatJID      string `json:"chat_jid" uri:"chat_jid"`
	TimerSeconds uint32 `json:"timer_seconds"`
}

type SetDisappearingTimerResponse struct {
	Status       string `json:"status"`
	Message      string `json:"message"`
	ChatJID      string `json:"chat_jid"`
	TimerSeconds uint32 `json:"timer_seconds"`
}

// Archive Chat operations
type ArchiveChatRequest struct {
	ChatJID  string `json:"chat_jid" uri:"chat_jid"`
	Archived bool   `json:"archived"`
}

type ArchiveChatResponse struct {
	Status   string `json:"status"`
	Message  string `json:"message"`
	ChatJID  string `json:"chat_jid"`
	Archived bool   `json:"archived"`
}

const (
	MediaPolicyModePermanent = "permanent"
	MediaPolicyModeEphemeral = "ephemeral"
	MediaPolicyRetentionDays = 5
)

type GetChatMediaPolicyRequest struct {
	ChatJID string `json:"chat_jid" uri:"chat_jid"`
}

type SetChatMediaPolicyRequest struct {
	ChatJID       string `json:"chat_jid" uri:"chat_jid"`
	Mode          string `json:"mode"`
	RetentionDays int    `json:"retention_days"`
}

type ResetChatMediaPolicyRequest struct {
	ChatJID string `json:"chat_jid" uri:"chat_jid"`
}

type ChatMediaPolicyResponse struct {
	ChatJID       string `json:"chat_jid"`
	Mode          string `json:"mode"`
	RetentionDays int    `json:"retention_days"`
	IsDefault     bool   `json:"is_default"`
	Ephemeral     bool   `json:"ephemeral"`
}

type DeleteChatLocalMediaRequest struct {
	ChatJID string `json:"chat_jid" uri:"chat_jid"`
	DryRun  bool   `json:"dry_run" query:"dry_run"`
}

type LocalMediaDeleteError struct {
	MessageID string `json:"message_id"`
	Path      string `json:"path"`
	Error     string `json:"error"`
}

type LocalMediaDeleteResponse struct {
	ChatJID         string                  `json:"chat_jid,omitempty"`
	DryRun          bool                    `json:"dry_run"`
	MediaTypes      []string                `json:"media_types"`
	MatchedMessages int                     `json:"matched_messages"`
	FilesFound      int                     `json:"files_found"`
	FilesDeleted    int                     `json:"files_deleted"`
	MissingFiles    int                     `json:"missing_files"`
	SkippedFiles    int                     `json:"skipped_files"`
	PathsCleared    int                     `json:"paths_cleared"`
	BytesFound      int64                   `json:"bytes_found"`
	BytesDeleted    int64                   `json:"bytes_deleted"`
	Errors          []LocalMediaDeleteError `json:"errors"`
}

type CleanupExpiredLocalMediaResponse struct {
	LocalMediaDeleteResponse
	Cutoff string `json:"cutoff"`
}
