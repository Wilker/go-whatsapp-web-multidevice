package chat

import (
	"context"
)

// IChatUsecase defines the interface for chat-related operations
type IChatUsecase interface {
	ListChats(ctx context.Context, request ListChatsRequest) (response ListChatsResponse, err error)
	GetChatMessages(ctx context.Context, request GetChatMessagesRequest) (response GetChatMessagesResponse, err error)
	PinChat(ctx context.Context, request PinChatRequest) (response PinChatResponse, err error)
	SetDisappearingTimer(ctx context.Context, request SetDisappearingTimerRequest) (response SetDisappearingTimerResponse, err error)
	ArchiveChat(ctx context.Context, request ArchiveChatRequest) (response ArchiveChatResponse, err error)
	GetChatMediaPolicy(ctx context.Context, request GetChatMediaPolicyRequest) (response ChatMediaPolicyResponse, err error)
	SetChatMediaPolicy(ctx context.Context, request SetChatMediaPolicyRequest) (response ChatMediaPolicyResponse, err error)
	ResetChatMediaPolicy(ctx context.Context, request ResetChatMediaPolicyRequest) (response ChatMediaPolicyResponse, err error)
	DeleteChatLocalMedia(ctx context.Context, request DeleteChatLocalMediaRequest) (response LocalMediaDeleteResponse, err error)
	CleanupExpiredLocalMedia(ctx context.Context) (response CleanupExpiredLocalMediaResponse, err error)
}
