package mcp

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

func newStandardToolResult(action, status string, request, result any, summary string) *mcp.CallToolResult {
	trimmedSummary := strings.TrimSpace(summary)
	if trimmedSummary == "" {
		trimmedSummary = fmt.Sprintf("%s completed", strings.TrimSpace(action))
	}

	return mcp.NewToolResultStructured(
		newStandardStructuredContent(action, status, request, result, trimmedSummary),
		trimmedSummary,
	)
}

func newStandardToolErrorResult(action, status string, request, result any, summary string) *mcp.CallToolResult {
	resultPayload := newStandardToolResult(action, status, request, result, summary)
	resultPayload.IsError = true
	return resultPayload
}

func newStandardStructuredContent(action, status string, request, result any, summary string) map[string]any {
	return map[string]any{
		"action":  strings.TrimSpace(action),
		"status":  normalizeEnvelopeStatus(status),
		"summary": strings.TrimSpace(summary),
		"request": request,
		"result":  result,
	}
}

func normalizeEnvelopeStatus(status string) string {
	trimmed := strings.TrimSpace(status)
	if trimmed == "" {
		return "success"
	}
	return strings.ToLower(trimmed)
}
