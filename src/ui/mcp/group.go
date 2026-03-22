package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	domainGroup "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/group"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	mcpHelpers "github.com/aldinokemal/go-whatsapp-web-multidevice/ui/mcp/helpers"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"go.mau.fi/whatsmeow"
)

type GroupHandler struct {
	groupService domainGroup.IGroupUsecase
}

func InitMcpGroup(groupService domainGroup.IGroupUsecase) *GroupHandler {
	return &GroupHandler{groupService: groupService}
}

func (h *GroupHandler) AddGroupTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(h.toolCreateGroup(), h.handleCreateGroup)
	mcpServer.AddTool(h.toolJoinGroup(), h.handleJoinGroup)
	mcpServer.AddTool(h.toolLeaveGroup(), h.handleLeaveGroup)
	mcpServer.AddTool(h.toolGetParticipants(), h.handleGetParticipants)
	mcpServer.AddTool(h.toolManageParticipants(), h.handleManageParticipants)
	mcpServer.AddTool(h.toolGetInviteLink(), h.handleGetInviteLink)
	mcpServer.AddTool(h.toolGroupInfo(), h.handleGroupInfo)
	mcpServer.AddTool(h.toolSetGroupName(), h.handleSetGroupName)
	mcpServer.AddTool(h.toolSetGroupTopic(), h.handleSetGroupTopic)
	mcpServer.AddTool(h.toolSetGroupLocked(), h.handleSetGroupLocked)
	mcpServer.AddTool(h.toolSetGroupAnnounce(), h.handleSetGroupAnnounce)
	mcpServer.AddTool(h.toolListGroupJoinRequests(), h.handleListGroupJoinRequests)
	mcpServer.AddTool(h.toolManageGroupJoinRequests(), h.handleManageGroupJoinRequests)
}

func (h *GroupHandler) toolCreateGroup() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_create",
		mcp.WithDescription("Create a new WhatsApp group with an optional participant list."),
		mcp.WithTitleAnnotation("Create Group"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("title",
			mcp.Description("Group subject/title."),
			mcp.Required(),
		),
		mcp.WithArray("participants",
			mcp.Description("Phone numbers to add during creation (without @s.whatsapp.net suffix)."),
			mcp.WithStringItems(),
		),
	)
}

func (h *GroupHandler) handleCreateGroup(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	title, err := request.RequireString("title")
	if err != nil {
		return nil, err
	}

	var participants []string
	if args := request.GetArguments(); args != nil {
		if raw, ok := args["participants"]; ok {
			participants, err = toStringSlice(raw)
			if err != nil {
				return nil, err
			}
		}
	}

	groupID, err := h.groupService.CreateGroup(ctx, domainGroup.CreateGroupRequest{
		Title:        strings.TrimSpace(title),
		Participants: participants,
	})
	if err != nil {
		return nil, err
	}

	structured := map[string]any{
		"group": map[string]any{
			"jid":  groupID,
			"name": strings.TrimSpace(title),
		},
		"participants": map[string]any{
			"requested":       participants,
			"requested_count": len(participants),
		},
	}
	fallback := fmt.Sprintf(
		"Group created\ngroup_id: %s\ntitle: %s\nparticipants_requested: %d",
		groupID,
		strings.TrimSpace(title),
		len(participants),
	)
	if len(participants) > 0 {
		previewCount := len(participants)
		if previewCount > 20 {
			previewCount = 20
		}
		fallback += "\nparticipants_preview: " + strings.Join(participants[:previewCount], ", ")
		if len(participants) > previewCount {
			fallback += fmt.Sprintf("\n...and %d more participants.", len(participants)-previewCount)
		}
	}
	requestPayload := map[string]any{
		"title":        strings.TrimSpace(title),
		"participants": participants,
	}
	return newStandardToolResult("whatsapp_group_create", "success", requestPayload, structured, fallback), nil
}

func (h *GroupHandler) toolJoinGroup() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_join_via_link",
		mcp.WithDescription("Join a group using an invite link."),
		mcp.WithTitleAnnotation("Join Group"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("invite_link",
			mcp.Description("WhatsApp group invite link."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleJoinGroup(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	link, err := request.RequireString("invite_link")
	if err != nil {
		return nil, err
	}

	groupID, err := h.groupService.JoinGroupWithLink(ctx, domainGroup.JoinGroupWithLinkRequest{Link: strings.TrimSpace(link)})
	if err != nil {
		return nil, err
	}

	structured := map[string]any{
		"group": map[string]any{
			"jid": groupID,
		},
		"invite_link": strings.TrimSpace(link),
	}
	fallback := fmt.Sprintf(
		"Joined group via invite link\ngroup_id: %s\ninvite_link: %s",
		groupID,
		strings.TrimSpace(link),
	)
	requestPayload := map[string]any{
		"invite_link": strings.TrimSpace(link),
	}
	return newStandardToolResult("whatsapp_group_join_via_link", "success", requestPayload, structured, fallback), nil
}

func (h *GroupHandler) toolLeaveGroup() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_leave",
		mcp.WithDescription("Leave a WhatsApp group by its ID."),
		mcp.WithTitleAnnotation("Leave Group"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleLeaveGroup(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	if err := h.groupService.LeaveGroup(ctx, domainGroup.LeaveGroupRequest{GroupID: trimmed}); err != nil {
		return nil, err
	}

	structured := map[string]any{
		"group": map[string]any{
			"jid": trimmed,
		},
		"state": "left",
	}
	fallback := fmt.Sprintf("Left group\ngroup_id: %s", trimmed)
	requestPayload := map[string]any{
		"group_id": trimmed,
	}
	return newStandardToolResult("whatsapp_group_leave", "success", requestPayload, structured, fallback), nil
}

func (h *GroupHandler) toolGetParticipants() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_participants",
		mcp.WithDescription("Retrieve the participant list for a group."),
		mcp.WithTitleAnnotation("List Participants"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleGetParticipants(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	resp, err := h.groupService.GetGroupParticipants(ctx, domainGroup.GetGroupParticipantsRequest{GroupID: trimmed})
	if err != nil {
		return nil, err
	}

	fallback := buildGroupParticipantsFallback(resp)
	resultPayload := buildGroupParticipantsResultPayload(resp)
	requestPayload := map[string]any{
		"group_id": trimmed,
	}
	return newStandardToolResult("whatsapp_group_participants", "success", requestPayload, resultPayload, fallback), nil
}

func (h *GroupHandler) toolManageParticipants() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_manage_participants",
		mcp.WithDescription("Add, remove, promote, or demote group participants."),
		mcp.WithTitleAnnotation("Manage Participants"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithArray("participants",
			mcp.Description("Phone numbers of participants to modify (without suffix)."),
			mcp.Required(),
			mcp.WithStringItems(),
		),
		mcp.WithString("action",
			mcp.Description("Participant action: add, remove, promote, or demote."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleManageParticipants(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	var participants []string

	args := request.GetArguments()
	if args == nil {
		return nil, fmt.Errorf("participants are required")
	}

	rawParticipants, exists := args["participants"]
	if !exists {
		return nil, fmt.Errorf("participants are required")
	}

	participants, err = toStringSlice(rawParticipants)
	if err != nil {
		return nil, err
	}
	if len(participants) == 0 {
		return nil, fmt.Errorf("participants cannot be empty")
	}

	actionStr, err := request.RequireString("action")
	if err != nil {
		return nil, err
	}

	change, err := parseParticipantChange(actionStr)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	result, err := h.groupService.ManageParticipant(ctx, domainGroup.ParticipantRequest{
		GroupID:      trimmed,
		Participants: participants,
		Action:       change,
	})
	if err != nil {
		return nil, err
	}

	successCount, errorCount := summarizeParticipantStatuses(result)
	statusItems := buildParticipantStatusItems(result)
	structured := map[string]any{
		"group": map[string]any{
			"jid": trimmed,
		},
		"participant_action":     strings.ToLower(strings.TrimSpace(actionStr)),
		"requested_participants": participants,
		"requested_count":        len(participants),
		"items":                  statusItems,
		"success_count":          successCount,
		"error_count":            errorCount,
	}
	fallback := buildParticipantStatusFallback(trimmed, strings.ToLower(strings.TrimSpace(actionStr)), participants, result)
	requestPayload := map[string]any{
		"group_id":     trimmed,
		"participants": participants,
		"action":       strings.ToLower(strings.TrimSpace(actionStr)),
	}
	return newStandardToolResult("whatsapp_group_manage_participants", "success", requestPayload, structured, fallback), nil
}

func (h *GroupHandler) toolGetInviteLink() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_invite_link",
		mcp.WithDescription("Fetch the invite link for a group, optionally resetting it."),
		mcp.WithTitleAnnotation("Get Invite Link"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithBoolean("reset",
			mcp.Description("If true, reset the invite link."),
			mcp.DefaultBool(false),
		),
	)
}

func (h *GroupHandler) handleGetInviteLink(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	reset := false
	if rawArgs := request.GetArguments(); rawArgs != nil {
		if val, ok := rawArgs["reset"]; ok {
			parsed, err := toBool(val)
			if err != nil {
				return nil, err
			}
			reset = parsed
		}
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	resp, err := h.groupService.GetGroupInviteLink(ctx, domainGroup.GetGroupInviteLinkRequest{
		GroupID: trimmed,
		Reset:   reset,
	})
	if err != nil {
		return nil, err
	}

	structured := map[string]any{
		"group": map[string]any{
			"jid": trimmed,
		},
		"invite_link": resp.InviteLink,
		"reset":       reset,
	}
	fallback := fmt.Sprintf(
		"Group invite link\ngroup_id: %s\nreset: %t\ninvite_link: %s",
		trimmed,
		reset,
		resp.InviteLink,
	)
	requestPayload := map[string]any{
		"group_id": trimmed,
		"reset":    reset,
	}
	return newStandardToolResult("whatsapp_group_invite_link", "success", requestPayload, structured, fallback), nil
}

func (h *GroupHandler) toolGroupInfo() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_info",
		mcp.WithDescription("Retrieve detailed WhatsApp group information."),
		mcp.WithTitleAnnotation("Group Info"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleGroupInfo(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	resp, err := h.groupService.GroupInfo(ctx, domainGroup.GroupInfoRequest{GroupID: trimmed})
	if err != nil {
		return nil, err
	}

	structured := buildGroupInfoResultPayload(trimmed, resp)
	fallback := buildGroupInfoFallback(trimmed, resp)
	requestPayload := map[string]any{
		"group_id": trimmed,
	}
	return newStandardToolResult("whatsapp_group_info", "success", requestPayload, structured, fallback), nil
}

func (h *GroupHandler) toolSetGroupName() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_set_name",
		mcp.WithDescription("Update the group's display name."),
		mcp.WithTitleAnnotation("Set Group Name"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithString("name",
			mcp.Description("New group name."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleSetGroupName(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	name, err := request.RequireString("name")
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	if err := h.groupService.SetGroupName(ctx, domainGroup.SetGroupNameRequest{GroupID: trimmed, Name: strings.TrimSpace(name)}); err != nil {
		return nil, err
	}

	structured := map[string]any{
		"group": map[string]any{
			"jid":  trimmed,
			"name": strings.TrimSpace(name),
		},
	}
	fallback := fmt.Sprintf(
		"Group name updated\ngroup_id: %s\ngroup_name: %s",
		trimmed,
		strings.TrimSpace(name),
	)
	requestPayload := map[string]any{
		"group_id": trimmed,
		"name":     strings.TrimSpace(name),
	}
	return newStandardToolResult("whatsapp_group_set_name", "success", requestPayload, structured, fallback), nil
}

func (h *GroupHandler) toolSetGroupTopic() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_set_topic",
		mcp.WithDescription("Update the group's topic or description."),
		mcp.WithTitleAnnotation("Set Group Topic"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithString("topic",
			mcp.Description("New group topic/description."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleSetGroupTopic(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	topic, err := request.RequireString("topic")
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	if err := h.groupService.SetGroupTopic(ctx, domainGroup.SetGroupTopicRequest{GroupID: trimmed, Topic: strings.TrimSpace(topic)}); err != nil {
		return nil, err
	}

	structured := map[string]any{
		"group": map[string]any{
			"jid":   trimmed,
			"topic": strings.TrimSpace(topic),
		},
	}
	fallback := fmt.Sprintf(
		"Group topic updated\ngroup_id: %s\ngroup_topic: %s",
		trimmed,
		strings.TrimSpace(topic),
	)
	requestPayload := map[string]any{
		"group_id": trimmed,
		"topic":    strings.TrimSpace(topic),
	}
	return newStandardToolResult("whatsapp_group_set_topic", "success", requestPayload, structured, fallback), nil
}

func (h *GroupHandler) toolSetGroupLocked() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_set_locked",
		mcp.WithDescription("Toggle whether only admins can edit group info."),
		mcp.WithTitleAnnotation("Set Group Locked"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithBoolean("locked",
			mcp.Description("Set to true to lock the group."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleSetGroupLocked(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	args := request.GetArguments()
	if args == nil {
		return nil, fmt.Errorf("locked flag is required")
	}

	val, ok := args["locked"]
	if !ok {
		return nil, fmt.Errorf("locked flag is required")
	}

	locked, err := toBool(val)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	if err := h.groupService.SetGroupLocked(ctx, domainGroup.SetGroupLockedRequest{GroupID: trimmed, Locked: locked}); err != nil {
		return nil, err
	}

	state := "unlocked"
	if locked {
		state = "locked"
	}

	structured := map[string]any{
		"group": map[string]any{
			"jid": trimmed,
		},
		"locked": locked,
		"state":  state,
	}
	fallback := fmt.Sprintf(
		"Group lock setting updated\ngroup_id: %s\nlocked: %t\nstate: %s",
		trimmed,
		locked,
		state,
	)
	requestPayload := map[string]any{
		"group_id": trimmed,
		"locked":   locked,
	}
	return newStandardToolResult("whatsapp_group_set_locked", "success", requestPayload, structured, fallback), nil
}

func (h *GroupHandler) toolSetGroupAnnounce() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_set_announce",
		mcp.WithDescription("Toggle announcement-only mode."),
		mcp.WithTitleAnnotation("Set Group Announce"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithBoolean("announce",
			mcp.Description("Set to true to allow only admins to send messages."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleSetGroupAnnounce(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	args := request.GetArguments()
	if args == nil {
		return nil, fmt.Errorf("announce flag is required")
	}

	val, ok := args["announce"]
	if !ok {
		return nil, fmt.Errorf("announce flag is required")
	}

	announce, err := toBool(val)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	if err := h.groupService.SetGroupAnnounce(ctx, domainGroup.SetGroupAnnounceRequest{GroupID: trimmed, Announce: announce}); err != nil {
		return nil, err
	}

	state := "regular chat"
	if announce {
		state = "announcement-only"
	}

	structured := map[string]any{
		"group": map[string]any{
			"jid": trimmed,
		},
		"announce_only": announce,
		"state":         state,
	}
	fallback := fmt.Sprintf(
		"Group announce mode updated\ngroup_id: %s\nannounce: %t\nstate: %s",
		trimmed,
		announce,
		state,
	)
	requestPayload := map[string]any{
		"group_id": trimmed,
		"announce": announce,
	}
	return newStandardToolResult("whatsapp_group_set_announce", "success", requestPayload, structured, fallback), nil
}

func (h *GroupHandler) toolListGroupJoinRequests() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_join_requests",
		mcp.WithDescription("List pending requests to join a group."),
		mcp.WithTitleAnnotation("List Join Requests"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleListGroupJoinRequests(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	resp, err := h.groupService.GetGroupRequestParticipants(ctx, domainGroup.GetGroupRequestParticipantsRequest{GroupID: trimmed})
	if err != nil {
		return nil, err
	}

	fallback := buildGroupJoinRequestsFallback(trimmed, resp)
	resultPayload := buildGroupJoinRequestsResultPayload(trimmed, resp)
	requestPayload := map[string]any{
		"group_id": trimmed,
	}
	return newStandardToolResult("whatsapp_group_join_requests", "success", requestPayload, resultPayload, fallback), nil
}

func (h *GroupHandler) toolManageGroupJoinRequests() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_group_manage_join_requests",
		mcp.WithDescription("Approve or reject pending group join requests."),
		mcp.WithTitleAnnotation("Manage Join Requests"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("group_id",
			mcp.Description("Group JID or numeric ID."),
			mcp.Required(),
		),
		mcp.WithArray("participants",
			mcp.Description("Phone numbers of requesters (without suffix)."),
			mcp.Required(),
			mcp.WithStringItems(),
		),
		mcp.WithString("action",
			mcp.Description("Action to apply: approve or reject."),
			mcp.Required(),
		),
	)
}

func (h *GroupHandler) handleManageGroupJoinRequests(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	ctx, err := mcpHelpers.ContextWithDefaultDevice(ctx)
	if err != nil {
		return nil, err
	}

	groupID, err := request.RequireString("group_id")
	if err != nil {
		return nil, err
	}

	args := request.GetArguments()
	if args == nil {
		return nil, fmt.Errorf("participants are required")
	}

	participantsRaw, ok := args["participants"]
	if !ok {
		return nil, fmt.Errorf("participants are required")
	}

	participants, err := toStringSlice(participantsRaw)
	if err != nil {
		return nil, err
	}
	if len(participants) == 0 {
		return nil, fmt.Errorf("participants cannot be empty")
	}

	actionStr, err := request.RequireString("action")
	if err != nil {
		return nil, err
	}

	change, err := parseParticipantRequestChange(actionStr)
	if err != nil {
		return nil, err
	}

	trimmed := strings.TrimSpace(groupID)
	utils.SanitizePhone(&trimmed)

	result, err := h.groupService.ManageGroupRequestParticipants(ctx, domainGroup.GroupRequestParticipantsRequest{
		GroupID:      trimmed,
		Participants: participants,
		Action:       change,
	})
	if err != nil {
		return nil, err
	}

	actionLower := strings.ToLower(actionStr)
	actionVerb := actionLower
	switch actionLower {
	case "approve":
		actionVerb = "approved"
	case "reject":
		actionVerb = "rejected"
	}

	actionReadable := actionVerb
	if len(actionVerb) > 0 {
		actionReadable = strings.ToUpper(actionVerb[:1]) + actionVerb[1:]
	}

	successCount, errorCount := summarizeParticipantStatuses(result)
	statusItems := buildParticipantStatusItems(result)
	structured := map[string]any{
		"group": map[string]any{
			"jid": trimmed,
		},
		"request_action":         strings.ToLower(strings.TrimSpace(actionStr)),
		"requested_participants": participants,
		"requested_count":        len(participants),
		"items":                  statusItems,
		"success_count":          successCount,
		"error_count":            errorCount,
	}
	fallback := buildParticipantStatusFallback(trimmed, actionReadable, participants, result)
	requestPayload := map[string]any{
		"group_id":     trimmed,
		"participants": participants,
		"action":       strings.ToLower(strings.TrimSpace(actionStr)),
	}
	return newStandardToolResult("whatsapp_group_manage_join_requests", "success", requestPayload, structured, fallback), nil
}

func parseParticipantChange(action string) (whatsmeow.ParticipantChange, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "add":
		return whatsmeow.ParticipantChangeAdd, nil
	case "remove":
		return whatsmeow.ParticipantChangeRemove, nil
	case "promote":
		return whatsmeow.ParticipantChangePromote, nil
	case "demote":
		return whatsmeow.ParticipantChangeDemote, nil
	default:
		return whatsmeow.ParticipantChange(""), fmt.Errorf("invalid participant action: %s", action)
	}
}

func parseParticipantRequestChange(action string) (whatsmeow.ParticipantRequestChange, error) {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "approve":
		return whatsmeow.ParticipantChangeApprove, nil
	case "reject":
		return whatsmeow.ParticipantChangeReject, nil
	default:
		return whatsmeow.ParticipantRequestChange(""), fmt.Errorf("invalid join request action: %s", action)
	}
}

func toStringSlice(raw any) ([]string, error) {
	if raw == nil {
		return nil, nil
	}

	switch v := raw.(type) {
	case []string:
		result := make([]string, len(v))
		for i, item := range v {
			result[i] = strings.TrimSpace(item)
		}
		return result, nil
	case []any:
		result := make([]string, len(v))
		for i, item := range v {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("participants[%d] is not a string", i)
			}
			result[i] = strings.TrimSpace(str)
		}
		return result, nil
	default:
		return nil, fmt.Errorf("participants must be an array of strings")
	}
}

func buildGroupParticipantsResultPayload(resp domainGroup.GetGroupParticipantsResponse) map[string]any {
	items := make([]map[string]any, 0, len(resp.Participants))
	for _, participant := range resp.Participants {
		role := "member"
		if participant.IsSuperAdmin {
			role = "super_admin"
		} else if participant.IsAdmin {
			role = "admin"
		}

		name := strings.TrimSpace(participant.DisplayName)
		if name == "" {
			name = "(no name)"
		}

		items = append(items, map[string]any{
			"jid":   participant.JID,
			"name":  name,
			"phone": participant.PhoneNumber,
			"role":  role,
		})
	}

	groupName := strings.TrimSpace(resp.Name)
	if groupName == "" {
		groupName = "(no group name)"
	}

	return map[string]any{
		"group": map[string]any{
			"jid":  resp.GroupID,
			"name": groupName,
		},
		"items": items,
		"count": len(items),
	}
}

func buildGroupJoinRequestsResultPayload(groupID string, requests []domainGroup.GetGroupRequestParticipantsResponse) map[string]any {
	items := make([]map[string]any, 0, len(requests))
	for _, req := range requests {
		name := strings.TrimSpace(req.DisplayName)
		if name == "" {
			name = "(no name)"
		}

		items = append(items, map[string]any{
			"jid":          req.JID,
			"name":         name,
			"phone":        req.PhoneNumber,
			"requested_at": req.RequestedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}

	return map[string]any{
		"group": map[string]any{
			"jid": groupID,
		},
		"items": items,
		"count": len(items),
	}
}

func buildParticipantStatusItems(results []domainGroup.ParticipantStatus) []map[string]any {
	items := make([]map[string]any, 0, len(results))
	for _, status := range results {
		items = append(items, map[string]any{
			"jid":     status.Participant,
			"status":  strings.ToLower(strings.TrimSpace(status.Status)),
			"message": status.Message,
		})
	}
	return items
}

func buildGroupParticipantsFallback(resp domainGroup.GetGroupParticipantsResponse) string {
	const maxPreview = 20

	total := len(resp.Participants)
	if total == 0 {
		return fmt.Sprintf("Group %s (%s) has no participants.", resp.GroupID, strings.TrimSpace(resp.Name))
	}

	previewCount := total
	if previewCount > maxPreview {
		previewCount = maxPreview
	}

	groupName := strings.TrimSpace(resp.Name)
	if groupName == "" {
		groupName = "(no group name)"
	}

	lines := make([]string, 0, previewCount+2)
	lines = append(lines, fmt.Sprintf("Group %s | %s\nParticipants: %d", groupName, resp.GroupID, total))
	for i := 0; i < previewCount; i++ {
		p := resp.Participants[i]
		name := strings.TrimSpace(p.DisplayName)
		if name == "" {
			name = "(no name)"
		}

		role := "member"
		if p.IsSuperAdmin {
			role = "super_admin"
		} else if p.IsAdmin {
			role = "admin"
		}

		lines = append(lines, fmt.Sprintf("%d. %s | %s | role=%s", i+1, name, p.JID, role))
	}

	if total > previewCount {
		lines = append(lines, fmt.Sprintf("...and %d more participants.", total-previewCount))
	}

	return strings.Join(lines, "\n")
}

func buildGroupJoinRequestsFallback(groupID string, requests []domainGroup.GetGroupRequestParticipantsResponse) string {
	const maxPreview = 20

	total := len(requests)
	if total == 0 {
		return fmt.Sprintf("Group %s has no pending join requests.", groupID)
	}

	previewCount := total
	if previewCount > maxPreview {
		previewCount = maxPreview
	}

	lines := make([]string, 0, previewCount+2)
	lines = append(lines, fmt.Sprintf("Group %s has %d pending join requests:", groupID, total))
	for i := 0; i < previewCount; i++ {
		req := requests[i]
		name := strings.TrimSpace(req.DisplayName)
		if name == "" {
			name = "(no name)"
		}
		lines = append(lines, fmt.Sprintf("%d. %s | %s | requested_at=%s", i+1, name, req.JID, req.RequestedAt.Format("2006-01-02T15:04:05Z07:00")))
	}

	if total > previewCount {
		lines = append(lines, fmt.Sprintf("...and %d more requests.", total-previewCount))
	}

	return strings.Join(lines, "\n")
}

func buildParticipantStatusFallback(groupID, action string, participants []string, results []domainGroup.ParticipantStatus) string {
	const maxPreview = 20

	successCount, errorCount := summarizeParticipantStatuses(results)
	lines := make([]string, 0, maxPreview+3)
	lines = append(lines, fmt.Sprintf(
		"Action %q applied to group %s\nrequested=%d success=%d error=%d",
		action,
		groupID,
		len(participants),
		successCount,
		errorCount,
	))

	if len(results) == 0 {
		lines = append(lines, "No participant status details returned.")
		return strings.Join(lines, "\n")
	}

	previewCount := len(results)
	if previewCount > maxPreview {
		previewCount = maxPreview
	}

	for i := 0; i < previewCount; i++ {
		status := results[i]
		lines = append(lines, fmt.Sprintf("%d. %s | status=%s | %s", i+1, status.Participant, status.Status, status.Message))
	}

	if len(results) > previewCount {
		lines = append(lines, fmt.Sprintf("...and %d more results.", len(results)-previewCount))
	}

	return strings.Join(lines, "\n")
}

func summarizeParticipantStatuses(results []domainGroup.ParticipantStatus) (successCount, errorCount int) {
	for _, status := range results {
		switch strings.ToLower(strings.TrimSpace(status.Status)) {
		case "success":
			successCount++
		case "error", "failed", "failure":
			errorCount++
		default:
			if strings.Contains(strings.ToLower(status.Message), "fail") || strings.Contains(strings.ToLower(status.Message), "error") {
				errorCount++
			} else {
				successCount++
			}
		}
	}
	return successCount, errorCount
}

func buildGroupInfoResultPayload(groupID string, resp domainGroup.GroupInfoResponse) map[string]any {
	result := map[string]any{
		"group": map[string]any{
			"jid": groupID,
		},
		"raw": resp.Data,
	}

	if resp.Data == nil {
		return result
	}

	raw, err := json.Marshal(resp.Data)
	if err != nil {
		return result
	}

	var details map[string]any
	if err := json.Unmarshal(raw, &details); err != nil {
		return result
	}

	group := result["group"].(map[string]any)
	if name := pickString(details, "Name", "name", "Subject", "subject"); name != "" {
		group["name"] = name
	}
	if jid := pickString(details, "JID", "jid", "ID", "id"); jid != "" {
		group["jid"] = jid
	}
	if topic := pickString(details, "Topic", "topic", "Description", "description"); topic != "" {
		group["topic"] = topic
	}
	if participantsCount := pickArrayLen(details, "Participants", "participants"); participantsCount >= 0 {
		group["participants_count"] = participantsCount
	}

	return result
}

func buildGroupInfoFallback(groupID string, resp domainGroup.GroupInfoResponse) string {
	lines := []string{fmt.Sprintf("Group info\ngroup_id: %s", groupID)}

	if resp.Data == nil {
		lines = append(lines, "details: (empty)")
		return strings.Join(lines, "\n")
	}

	raw, err := json.Marshal(resp.Data)
	if err != nil {
		lines = append(lines, fmt.Sprintf("details: %v", resp.Data))
		return strings.Join(lines, "\n")
	}

	var details map[string]any
	if err := json.Unmarshal(raw, &details); err == nil {
		if name := pickString(details, "Name", "name", "Subject", "subject"); name != "" {
			lines = append(lines, "name: "+name)
		}
		if jid := pickString(details, "JID", "jid", "ID", "id"); jid != "" {
			lines = append(lines, "jid: "+jid)
		}
		if topic := pickString(details, "Topic", "topic", "Description", "description"); topic != "" {
			lines = append(lines, "topic: "+topic)
		}

		if participantsCount := pickArrayLen(details, "Participants", "participants"); participantsCount >= 0 {
			lines = append(lines, fmt.Sprintf("participants_count: %d", participantsCount))
		}
	}

	lines = append(lines, "details_json: "+truncateGroupFallback(string(raw), 700))
	return strings.Join(lines, "\n")
}

func pickString(data map[string]any, keys ...string) string {
	for _, key := range keys {
		value, ok := data[key]
		if !ok {
			continue
		}

		switch typed := value.(type) {
		case string:
			trimmed := strings.TrimSpace(typed)
			if trimmed != "" {
				return trimmed
			}
		default:
			rendered := strings.TrimSpace(fmt.Sprintf("%v", typed))
			if rendered != "" && rendered != "<nil>" {
				return rendered
			}
		}
	}
	return ""
}

func pickArrayLen(data map[string]any, keys ...string) int {
	for _, key := range keys {
		value, ok := data[key]
		if !ok {
			continue
		}

		switch typed := value.(type) {
		case []any:
			return len(typed)
		case []string:
			return len(typed)
		}
	}
	return -1
}

func truncateGroupFallback(text string, max int) string {
	trimmed := strings.TrimSpace(text)
	if max <= 0 {
		return ""
	}

	runes := []rune(trimmed)
	if len(runes) <= max {
		return trimmed
	}
	if max <= 3 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}
