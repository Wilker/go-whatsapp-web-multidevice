package mcp

import (
	"strings"
	"testing"

	domainChat "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chat"
	domainUser "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/user"
	"go.mau.fi/whatsmeow/types"
)

func TestBuildChatsResultPayloadIncludesContactName(t *testing.T) {
	resp := domainChat.ListChatsResponse{
		Data: []domainChat.ChatInfo{
			{
				JID:  "5511999999999@s.whatsapp.net",
				Name: "5511999999999",
			},
		},
	}
	contactNames := map[string]string{
		"5511999999999@s.whatsapp.net": "Maria Silva",
	}

	payload := buildChatsResultPayload(resp, chatVisibleNames{contactNames: contactNames})
	items, ok := payload["items"].([]map[string]any)
	if !ok {
		t.Fatalf("expected items payload, got %T", payload["items"])
	}
	if len(items) != 1 {
		t.Fatalf("expected one item, got %d", len(items))
	}

	item := items[0]
	if item["name"] != "5511999999999" {
		t.Fatalf("expected existing name to be preserved, got %#v", item["name"])
	}
	if item["chat_name"] != "5511999999999" {
		t.Fatalf("expected chat_name to expose stored chat name, got %#v", item["chat_name"])
	}
	if item["contact_name"] != "Maria Silva" {
		t.Fatalf("expected contact_name to be populated, got %#v", item["contact_name"])
	}
	if item["display_name"] != "Maria Silva" {
		t.Fatalf("expected display_name to prefer contact name, got %#v", item["display_name"])
	}
}

func TestBuildChatsResultPayloadIncludesGroupName(t *testing.T) {
	resp := domainChat.ListChatsResponse{
		Data: []domainChat.ChatInfo{
			{
				JID:  "120363424157959439@g.us",
				Name: "Group 120363424157959439",
			},
		},
	}
	groupNames := map[string]string{
		"120363424157959439@g.us": "Refeição Gesso Casa Branca Liv Primavera",
	}

	payload := buildChatsResultPayload(resp, chatVisibleNames{groupNames: groupNames})
	items, ok := payload["items"].([]map[string]any)
	if !ok {
		t.Fatalf("expected items payload, got %T", payload["items"])
	}
	if len(items) != 1 {
		t.Fatalf("expected one item, got %d", len(items))
	}

	item := items[0]
	if item["group_name"] != "Refeição Gesso Casa Branca Liv Primavera" {
		t.Fatalf("expected group_name to be populated, got %#v", item["group_name"])
	}
	if item["display_name"] != "Refeição Gesso Casa Branca Liv Primavera" {
		t.Fatalf("expected display_name to prefer group name, got %#v", item["display_name"])
	}
}

func TestBuildListChatsFallbackIncludesContactName(t *testing.T) {
	resp := domainChat.ListChatsResponse{
		Data: []domainChat.ChatInfo{
			{
				JID:  "5511999999999@s.whatsapp.net",
				Name: "5511999999999",
			},
		},
		Pagination: domainChat.PaginationResponse{Limit: 25, Total: 1},
	}
	req := domainChat.ListChatsRequest{Limit: 25}
	contactNames := map[string]string{
		"5511999999999@s.whatsapp.net": "Maria Silva",
	}

	fallback := buildListChatsFallback(resp, req, chatVisibleNames{contactNames: contactNames})
	if !strings.Contains(fallback, "contact_name: Maria Silva") {
		t.Fatalf("expected fallback to include contact_name, got:\n%s", fallback)
	}
}

func TestBuildListChatsFallbackIncludesGroupName(t *testing.T) {
	resp := domainChat.ListChatsResponse{
		Data: []domainChat.ChatInfo{
			{
				JID:  "120363424157959439@g.us",
				Name: "Group 120363424157959439",
			},
		},
		Pagination: domainChat.PaginationResponse{Limit: 25, Total: 1},
	}
	req := domainChat.ListChatsRequest{Limit: 25}
	groupNames := map[string]string{
		"120363424157959439@g.us": "Refeição Gesso Casa Branca Liv Primavera",
	}

	fallback := buildListChatsFallback(resp, req, chatVisibleNames{groupNames: groupNames})
	if !strings.Contains(fallback, "group_name: Refeição Gesso Casa Branca Liv Primavera") {
		t.Fatalf("expected fallback to include group_name, got:\n%s", fallback)
	}
}

func TestBuildContactNameMapSkipsBlankNames(t *testing.T) {
	resp := domainUser.MyListContactsResponse{
		Data: []domainUser.MyListContactsResponseData{
			{
				JID:  types.NewJID("5511999999999", types.DefaultUserServer),
				Name: " Maria Silva ",
			},
			{
				JID:  types.NewJID("5511888888888", types.DefaultUserServer),
				Name: " ",
			},
		},
	}

	contactNames := buildContactNameMap(resp)
	if contactNames["5511999999999@s.whatsapp.net"] != "Maria Silva" {
		t.Fatalf("expected trimmed contact name, got %#v", contactNames["5511999999999@s.whatsapp.net"])
	}
	if _, ok := contactNames["5511888888888@s.whatsapp.net"]; ok {
		t.Fatal("expected blank contact names to be skipped")
	}
}

func TestBuildGroupNameMapSkipsBlankNames(t *testing.T) {
	resp := domainUser.MyListGroupsResponse{
		Data: []types.GroupInfo{
			{
				JID: types.NewJID("120363424157959439", "g.us"),
				GroupName: types.GroupName{
					Name: " Refeição Gesso Casa Branca Liv Primavera ",
				},
			},
			{
				JID: types.NewJID("120363111111111111", "g.us"),
				GroupName: types.GroupName{
					Name: " ",
				},
			},
		},
	}

	groupNames := buildGroupNameMap(resp)
	if groupNames["120363424157959439@g.us"] != "Refeição Gesso Casa Branca Liv Primavera" {
		t.Fatalf("expected trimmed group name, got %#v", groupNames["120363424157959439@g.us"])
	}
	if _, ok := groupNames["120363111111111111@g.us"]; ok {
		t.Fatal("expected blank group names to be skipped")
	}
}

func TestFilterChatsByVisibleNameMatchesStoredContactAndGroupNames(t *testing.T) {
	chats := []domainChat.ChatInfo{
		{
			JID:  "5511999999999@s.whatsapp.net",
			Name: "5511999999999",
		},
		{
			JID:  "120363424157959439@g.us",
			Name: "Group 120363424157959439",
		},
		{
			JID:  "status@broadcast",
			Name: "Status",
		},
	}
	visibleNames := chatVisibleNames{
		contactNames: map[string]string{
			"5511999999999@s.whatsapp.net": "Davidson Refeição Liv Primavera",
		},
		groupNames: map[string]string{
			"120363424157959439@g.us": "Refeição Gesso Casa Branca Liv Primavera",
		},
	}

	filtered := filterChatsByVisibleName(chats, "Refeição", visibleNames)
	if len(filtered) != 2 {
		t.Fatalf("expected two visible-name matches, got %d", len(filtered))
	}
	if filtered[0].JID != "5511999999999@s.whatsapp.net" {
		t.Fatalf("expected contact match first, got %s", filtered[0].JID)
	}
	if filtered[1].JID != "120363424157959439@g.us" {
		t.Fatalf("expected group match second, got %s", filtered[1].JID)
	}
}

func TestPaginateChatsUsesFilteredTotals(t *testing.T) {
	chats := []domainChat.ChatInfo{
		{JID: "1@s.whatsapp.net"},
		{JID: "2@s.whatsapp.net"},
		{JID: "3@s.whatsapp.net"},
	}

	resp := paginateChats(chats, domainChat.ListChatsRequest{Limit: 2, Offset: 1})
	if resp.Pagination.Total != 3 {
		t.Fatalf("expected filtered total 3, got %d", resp.Pagination.Total)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("expected two paginated chats, got %d", len(resp.Data))
	}
	if resp.Data[0].JID != "2@s.whatsapp.net" || resp.Data[1].JID != "3@s.whatsapp.net" {
		t.Fatalf("unexpected paginated chats: %#v", resp.Data)
	}
}
