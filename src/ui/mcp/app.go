package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	domainApp "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/app"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type AppHandler struct {
	appService domainApp.IAppUsecase
}

func InitMcpApp(appService domainApp.IAppUsecase) *AppHandler {
	return &AppHandler{appService: appService}
}

func (h *AppHandler) AddAppTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(h.toolConnectionStatus(), h.handleConnectionStatus)
	mcpServer.AddTool(h.toolLoginWithQR(), h.handleLoginWithQR)
	mcpServer.AddTool(h.toolLoginWithCode(), h.handleLoginWithCode)
	mcpServer.AddTool(h.toolLogout(), h.handleLogout)
	mcpServer.AddTool(h.toolReconnect(), h.handleReconnect)
}

func (h *AppHandler) toolConnectionStatus() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_connection_status",
		mcp.WithDescription("Check whether the WhatsApp client is connected and logged in."),
		mcp.WithTitleAnnotation("Connection Status"),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)
}

func (h *AppHandler) handleConnectionStatus(_ context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deviceID, err := h.defaultDeviceID()
	if err != nil {
		return nil, err
	}

	isConnected, isLoggedIn, err := h.appService.Status(context.Background(), deviceID)
	if err != nil {
		return nil, err
	}

	resultPayload := map[string]any{
		"device": map[string]any{
			"id": deviceID,
		},
		"connection": map[string]any{
			"is_connected": isConnected,
			"is_logged_in": isLoggedIn,
		},
	}
	requestPayload := map[string]any{
		"device_id": deviceID,
	}
	fallback := fmt.Sprintf(
		"Connection status\ndevice_id: %s\nis_connected: %t\nis_logged_in: %t",
		deviceID,
		isConnected,
		isLoggedIn,
	)
	return newStandardToolResult("whatsapp_connection_status", "success", requestPayload, resultPayload, fallback), nil
}

func (h *AppHandler) toolLoginWithQR() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_login_qr",
		mcp.WithDescription("Initiate a QR code based login flow. Returns the QR image and pairing code."),
		mcp.WithTitleAnnotation("Login With QR"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
	)
}

func (h *AppHandler) handleLoginWithQR(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deviceID, err := h.defaultDeviceID()
	if err != nil {
		return nil, err
	}

	resp, err := h.appService.Login(ctx, deviceID)
	if err != nil {
		return nil, err
	}

	fallback := fmt.Sprintf(
		"QR login initiated\ndevice_id: %s\nexpires_in: %d\nqr_code: %s\nqr_image_path: %s",
		deviceID,
		int(resp.Duration.Seconds()),
		resp.Code,
		resp.ImagePath,
	)
	resultPayload := map[string]any{
		"device": map[string]any{
			"id": deviceID,
		},
		"login": map[string]any{
			"method":             "qr",
			"qr_code":            resp.Code,
			"qr_image_path":      resp.ImagePath,
			"expires_in_seconds": int(resp.Duration.Seconds()),
		},
	}
	requestPayload := map[string]any{
		"device_id": deviceID,
	}

	qrBytes, readErr := os.ReadFile(resp.ImagePath)
	if readErr != nil {
		return newStandardToolResult("whatsapp_login_qr", "generated", requestPayload, resultPayload, fallback), nil
	}

	encoded := base64.StdEncoding.EncodeToString(qrBytes)
	result := mcp.NewToolResultImage(fallback, encoded, "image/png")
	result.StructuredContent = newStandardStructuredContent("whatsapp_login_qr", "generated", requestPayload, resultPayload, fallback)
	return result, nil
}

func (h *AppHandler) toolLoginWithCode() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_login_with_code",
		mcp.WithDescription("Generate a pairing code for WhatsApp multi-device login using a phone number."),
		mcp.WithTitleAnnotation("Login With Pairing Code"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
		mcp.WithString("phone",
			mcp.Description("Phone number in international format (e.g. +628123456789)."),
			mcp.Required(),
		),
	)
}

func (h *AppHandler) handleLoginWithCode(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	phone, err := request.RequireString("phone")
	if err != nil {
		return nil, err
	}

	trimmedPhone := strings.TrimSpace(phone)
	deviceID, err := h.defaultDeviceID()
	if err != nil {
		return nil, err
	}

	pairCode, err := h.appService.LoginWithCode(ctx, deviceID, trimmedPhone)
	if err != nil {
		return nil, err
	}

	resultPayload := map[string]any{
		"device": map[string]any{
			"id": deviceID,
		},
		"login": map[string]any{
			"method":    "pair_code",
			"phone":     trimmedPhone,
			"pair_code": pairCode,
		},
	}
	requestPayload := map[string]any{
		"device_id": deviceID,
		"phone":     trimmedPhone,
	}
	fallback := fmt.Sprintf(
		"Pairing code generated\ndevice_id: %s\nphone: %s\npair_code: %s",
		deviceID,
		trimmedPhone,
		pairCode,
	)
	return newStandardToolResult("whatsapp_login_with_code", "generated", requestPayload, resultPayload, fallback), nil
}

func (h *AppHandler) toolLogout() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_logout",
		mcp.WithDescription("Sign out the current WhatsApp session and clear stored credentials."),
		mcp.WithTitleAnnotation("Logout"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(true),
		mcp.WithIdempotentHintAnnotation(false),
	)
}

func (h *AppHandler) handleLogout(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deviceID, err := h.defaultDeviceID()
	if err != nil {
		return nil, err
	}

	if err := h.appService.Logout(ctx, deviceID); err != nil {
		return nil, err
	}

	resultPayload := map[string]any{
		"device": map[string]any{
			"id": deviceID,
		},
		"operation": "logout",
	}
	requestPayload := map[string]any{
		"device_id": deviceID,
	}
	fallback := fmt.Sprintf("Logout completed\ndevice_id: %s", deviceID)
	return newStandardToolResult("whatsapp_logout", "success", requestPayload, resultPayload, fallback), nil
}

func (h *AppHandler) toolReconnect() mcp.Tool {
	return mcp.NewTool(
		"whatsapp_reconnect",
		mcp.WithDescription("Attempt to reconnect to WhatsApp using the stored session."),
		mcp.WithTitleAnnotation("Reconnect"),
		mcp.WithReadOnlyHintAnnotation(false),
		mcp.WithDestructiveHintAnnotation(false),
		mcp.WithIdempotentHintAnnotation(true),
	)
}

func (h *AppHandler) handleReconnect(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	deviceID, err := h.defaultDeviceID()
	if err != nil {
		return nil, err
	}

	if err := h.appService.Reconnect(ctx, deviceID); err != nil {
		return nil, err
	}

	resultPayload := map[string]any{
		"device": map[string]any{
			"id": deviceID,
		},
		"operation": "reconnect",
	}
	requestPayload := map[string]any{
		"device_id": deviceID,
	}
	fallback := fmt.Sprintf("Reconnect initiated\ndevice_id: %s", deviceID)
	return newStandardToolResult("whatsapp_reconnect", "initiated", requestPayload, resultPayload, fallback), nil
}

func (h *AppHandler) defaultDeviceID() (string, error) {
	devices, err := h.appService.FetchDevices(context.Background())
	if err != nil {
		return "", err
	}
	if len(devices) == 0 {
		return "", fmt.Errorf("no devices registered")
	}
	return devices[0].Device, nil
}
