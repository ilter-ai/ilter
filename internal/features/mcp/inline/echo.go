package inline

import (
	"context"
	"log/slog"
)

func init() {
	if err := RegisterTools("echo", echoHandler, echoTools); err != nil {
		slog.Error("failed to register inline tool", "tool", "echo", "error", err)
	}
}

var echoTools = []ToolDef{
	{
		Name:        "echo",
		Description: "Echoes back the input message. Useful for testing MCP connectivity, latency, and response formatting.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"message": map[string]any{
					"type":        "string",
					"description": "The message to echo back",
				},
			},
			"required": []any{"message"},
		},
	},
}

func echoHandler(_ context.Context, args map[string]any) (any, error) {
	message, ok := args["message"].(string)
	if !ok {
		return nil, nil
	}
	return map[string]any{
		"echo":   message,
		"length": len(message),
	}, nil
}
