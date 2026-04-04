package mcptool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/anish/claude-msg-utils/internal/api"
)

// RegisterReply adds the reply tool to the MCP server. socketPath is the
// daemon's unix domain socket for sending messages.
func RegisterReply(s *server.MCPServer, socketPath string) {
	s.AddTool(
		mcp.NewTool("reply",
			mcp.WithDescription("Send a message through pigeon. Use this to reply to messages received via the pigeon channel."),
			mcp.WithString("platform", mcp.Description("Platform to send on (slack, whatsapp)"), mcp.Required()),
			mcp.WithString("account", mcp.Description("Account/workspace name"), mcp.Required()),
			mcp.WithString("contact", mcp.Description("Contact name, phone number, or channel"), mcp.Required()),
			mcp.WithString("message", mcp.Description("Message text to send"), mcp.Required()),
			mcp.WithString("thread", mcp.Description("Thread timestamp for Slack thread replies")),
		),
		func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return handleReply(ctx, req, socketPath)
		},
	)
}

func handleReply(ctx context.Context, req mcp.CallToolRequest, socketPath string) (*mcp.CallToolResult, error) {
	args := req.GetArguments()

	platform, _ := args["platform"].(string)
	account, _ := args["account"].(string)
	contact, _ := args["contact"].(string)
	message, _ := args["message"].(string)
	thread, _ := args["thread"].(string)

	if platform == "" || account == "" || contact == "" || message == "" {
		return mcp.NewToolResultError("platform, account, contact, and message are required"), nil
	}

	body, err := json.Marshal(api.SendRequest{
		Platform: platform,
		Account:  account,
		Contact:  contact,
		Message:  message,
		Thread:   thread,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", socketPath)
			},
		},
	}

	resp, err := client.Post("http://pigeon/api/send", "application/json", bytes.NewReader(body))
	if err != nil {
		return mcp.NewToolResultError("daemon not reachable: " + err.Error()), nil
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)
	var result api.SendResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return mcp.NewToolResultError("unexpected response: " + string(data)), nil
	}

	if !result.OK {
		return mcp.NewToolResultError(result.Error), nil
	}

	return mcp.NewToolResultText(fmt.Sprintf("Sent to %s at %s", contact, result.Timestamp)), nil
}
