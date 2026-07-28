package mcp

import (
	"fmt"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

var (
	mcpMeter = otel.Meter("ilter-mcp")

	ActiveConnections  metric.Int64Gauge
	MCPServersHealthy  metric.Int64Gauge
	MCPRequestsTotal   metric.Int64Counter
	MCPRequestDuration metric.Float64Histogram
	MCPToolsInjected   metric.Int64Counter
	MCPToolCallsTotal  metric.Int64Counter
)

var initMCPOnce sync.Once

func InitMCPMetrics() error {
	var initErr error
	initMCPOnce.Do(func() {
		var err error

		ActiveConnections, err = mcpMeter.Int64Gauge(
			"ilter_mcp_connections_active",
		)
		if err != nil {
			initErr = fmt.Errorf("create active connections gauge: %w", err)
			return
		}

		MCPServersHealthy, err = mcpMeter.Int64Gauge(
			"ilter_mcp_servers_healthy",
		)
		if err != nil {
			initErr = fmt.Errorf("create servers healthy gauge: %w", err)
			return
		}

		MCPRequestsTotal, err = mcpMeter.Int64Counter(
			"ilter_mcp_requests_total",
		)
		if err != nil {
			initErr = fmt.Errorf("create request counter: %w", err)
			return
		}

		MCPRequestDuration, err = mcpMeter.Float64Histogram(
			"ilter_mcp_request_duration_ms",
		)
		if err != nil {
			initErr = fmt.Errorf("create request duration histogram: %w", err)
			return
		}

		MCPToolsInjected, err = mcpMeter.Int64Counter(
			"ilter_mcp_tools_injected_total",
		)
		if err != nil {
			initErr = fmt.Errorf("create tools injected counter: %w", err)
			return
		}

		MCPToolCallsTotal, err = mcpMeter.Int64Counter(
			"ilter_mcp_tool_calls_total",
		)
		if err != nil {
			initErr = fmt.Errorf("create tool calls counter: %w", err)
			return
		}
	})
	return initErr
}
