package message

type GenericResponse struct {
	MessageID string `json:"message_id"`
	Status    string `json:"status"`
}

type RevokeRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
}

type DeleteRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
}

type ReactionRequest struct {
	MessageID string `json:"message_id" form:"message_id"`
	Phone     string `json:"phone" form:"phone"`
	Emoji     string `json:"emoji" form:"emoji"`
}

type UpdateMessageRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Message   string `json:"message" form:"message"`
	Phone     string `json:"phone" form:"phone"`
}

type MarkAsReadRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
}

type StarRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
	IsStarred bool   `json:"is_starred"`
}

type DownloadMediaRequest struct {
	MessageID string `json:"message_id" uri:"message_id"`
	Phone     string `json:"phone" form:"phone"`
}

type RecoverMediaBatchRequest struct {
	Phone      string   `json:"phone"`
	MessageIDs []string `json:"message_ids"`
}

type RecoverMediaBatchItem struct {
	MessageID         string `json:"message_id"`
	RecoveryMethod    string `json:"recovery_method,omitempty"`
	FailureReason     string `json:"failure_reason,omitempty"`
	UpdatedDirectPath string `json:"updated_direct_path,omitempty"`
}

type RecoverMediaBatchResponse struct {
	Items []RecoverMediaBatchItem `json:"items"`
}

const (
	MediaRecoveryMethodNone             = "none"
	MediaRecoveryMethodDirectURL        = "direct_url"
	MediaRecoveryMethodStoredDirectPath = "stored_direct_path"
	MediaRecoveryMethodMediaRetry       = "media_retry"
)

const (
	MediaFailureReasonNone                = ""
	MediaFailureReasonNoMediaMetadata     = "no_media_metadata"
	MediaFailureReasonRetryTimeout        = "retry_timeout"
	MediaFailureReasonNotAvailableOnPhone = "not_available_on_phone"
	MediaFailureReasonDownloadFailed      = "download_failed"
)

type DownloadMediaResponse struct {
	MessageID      string `json:"message_id"`
	Status         string `json:"status"`
	MediaType      string `json:"media_type"`
	Filename       string `json:"filename"`
	FilePath       string `json:"file_path"`
	FileSize       int64  `json:"file_size"`
	RecoveryMethod string `json:"recovery_method,omitempty"`
	FailureReason  string `json:"failure_reason,omitempty"`
}
