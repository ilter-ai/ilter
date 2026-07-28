package jobs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// MCPExecutor is the interface for executing MCP tool calls from delivery.
// The concrete adapter wraps *mcp.Executor and is set via SetMCPExecutor.
type MCPExecutor interface {
	ExecuteTool(ctx context.Context, server, tool string, args map[string]any) (any, error)
}

var globalMCPExec MCPExecutor

// SetMCPExecutor sets the global MCP executor for delivery.
func SetMCPExecutor(exec MCPExecutor) {
	globalMCPExec = exec
}

// Deliver sends the LLM result to the configured delivery target for a run.
// The deliveryConfig parameter is the DeliveryConfig JSON from the associated job.
func Deliver(ctx context.Context, deliveryConfig string, run *JobRun, llmResult string) error {
	if deliveryConfig == "" || deliveryConfig == "{}" {
		return nil
	}

	var cfg DeliveryConfig
	if err := json.Unmarshal([]byte(deliveryConfig), &cfg); err != nil {
		return fmt.Errorf("parse delivery_config: %w", err)
	}

	switch cfg.Type {
	case "mcp":
		return deliverViaMCP(ctx, &cfg, llmResult)
	case "webhook":
		return deliverViaWebhook(ctx, &cfg, llmResult, run)
	default:
		return fmt.Errorf("unknown delivery type: %s", cfg.Type)
	}
}

func deliverViaMCP(ctx context.Context, cfg *DeliveryConfig, llmResult string) error {
	if cfg.MCPServer == "" || cfg.Tool == "" {
		return fmt.Errorf("MCP delivery requires server and tool")
	}

	args := make(map[string]any)
	for k, v := range cfg.Args {
		switch val := v.(type) {
		case string:
			args[k] = strings.ReplaceAll(val, "{{result}}", llmResult)
		default:
			args[k] = v
		}
	}

	if len(args) == 0 {
		args["message"] = llmResult
	}

	if globalMCPExec == nil {
		return fmt.Errorf("MCP executor not initialized")
	}

	_, err := globalMCPExec.ExecuteTool(ctx, cfg.MCPServer, cfg.Tool, args)
	return err
}

func deliverViaWebhook(ctx context.Context, cfg *DeliveryConfig, llmResult string, run *JobRun) error {
	if cfg.WebhookURL == "" {
		return fmt.Errorf("webhook URL required")
	}

	payload := map[string]any{
		"result":    llmResult,
		"run_id":    run.ID,
		"job_id":    run.JobID,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
