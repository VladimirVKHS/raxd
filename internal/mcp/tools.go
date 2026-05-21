package mcp

// tools.go defines the MCP tool descriptors (Tool structs) for ping and server_info.
//
// Tool definitions per mcp-spec §5 (schemas) and plan.md §Contracts.
// SR-33/SR-34: server_info returns ONLY name/version/protocolVersion — no secrets.
// SR-37: only ping and server_info are registered; execute_command and upload_file are NOT.
// AC13: extension point is the AddTool call in server.go (add new tools there).

import (
	"context"
	"fmt"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// protocolVersion is the MCP protocol version supported by this server.
// ADR-002: fixed at "2025-11-25".
const protocolVersion = "2025-11-25"

// ---- ping tool ------------------------------------------------------------------

// PingInput is the (empty) input type for the ping tool.
// additionalProperties:false is enforced by SDK from the struct (no fields).
type PingInput struct{}

// PingOutput is the output type for the ping tool.
// ping returns only a text content block "pong", no structuredContent.
type PingOutput struct{}

// pingTool returns the mcp.Tool descriptor for ping.
// mcp-spec §5.1: description, inputSchema (empty object, additionalProperties:false).
func pingTool() *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name:        "ping",
		Description: `Проверка живости MCP-канала к raxd. Возвращает "pong". Без побочных эффектов на хосте.`,
	}
}

// pingHandler is the ToolHandlerFor[PingInput, PingOutput] for the ping tool.
// AC5/SR-31: returns content[0].text="pong", isError=false, no side effects.
// SR-34: no secrets in response.
func pingHandler(_ context.Context, _ *sdkmcp.CallToolRequest, _ PingInput) (*sdkmcp.CallToolResult, PingOutput, error) {
	result := &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{
			&sdkmcp.TextContent{Text: "pong"},
		},
	}
	return result, PingOutput{}, nil
}

// ---- server_info tool -----------------------------------------------------------

// ServerInfo is the structured output for the server_info tool.
// SR-33: only name, version, protocolVersion — no secrets, no paths, no config.
// mcp-spec §5.2: output schema matches these three fields exactly.
type ServerInfo struct {
	Name            string `json:"name"`
	Version         string `json:"version"`
	ProtocolVersion string `json:"protocolVersion"`
}

// InfoInput is the (empty) input type for the server_info tool.
type InfoInput struct{}

// serverInfoTool returns the mcp.Tool descriptor for server_info.
// mcp-spec §5.2: description, inputSchema (empty), outputSchema ({name,version,protocolVersion}).
func serverInfoTool(_ string) *sdkmcp.Tool {
	return &sdkmcp.Tool{
		Name:        "server_info",
		Description: `Версия и базовые сведения о демоне raxd без секретов: имя продукта, версия, версия протокола MCP.`,
	}
}

// serverInfoHandler builds the ToolHandlerFor[InfoInput, ServerInfo] for server_info.
// AC6/SR-33: returns ServerInfo{name:"raxd", version:ver, protocolVersion:"2025-11-25"}.
// SR-34: source of version is the passed `ver` (from internal/version), NOT secrets/config/env.
// The handler has no I/O and cannot fail; error path kept for contract uniformity.
func serverInfoHandler(ver string) func(context.Context, *sdkmcp.CallToolRequest, InfoInput) (*sdkmcp.CallToolResult, ServerInfo, error) {
	info := ServerInfo{
		Name:            "raxd",
		Version:         ver,
		ProtocolVersion: protocolVersion,
	}
	textLine := fmt.Sprintf("raxd %s (MCP %s)", ver, protocolVersion)

	return func(_ context.Context, _ *sdkmcp.CallToolRequest, _ InfoInput) (*sdkmcp.CallToolResult, ServerInfo, error) {
		result := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: textLine},
			},
		}
		return result, info, nil
	}
}
