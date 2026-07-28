package mcp

import (
	"sync"
	"time"
)

func (ex *Executor) getToolConfig(toolName string) *ToolConfig {
	if v, ok := ex.toolConfigCache.Load(toolName); ok {
		return v.(*ToolConfig)
	}
	if ex.db == nil {
		return nil
	}
	var tc ToolConfig
	err := ex.db.QueryRow(
		`SELECT destructive, requires_confirmation, rate_limit_rpm, coalesce(timeout_ms, 0) FROM mcp_tool_config WHERE tool_name = ?`,
		toolName,
	).Scan(&tc.Destructive, &tc.RequiresConfirmation, &tc.RateLimitRPM, &tc.TimeoutMs)
	if err != nil {
		return nil
	}
	ex.toolConfigCache.Store(toolName, &tc)
	return &tc
}

func (ex *Executor) isRateLimited(toolName string, rpm int) bool {
	if rpm <= 0 {
		return false
	}
	v, _ := ex.rateLimits.LoadOrStore(toolName, &rateLimitWindow{start: time.Now()})
	w := v.(*rateLimitWindow)
	w.mu.Lock()
	defer w.mu.Unlock()
	if time.Since(w.start) >= time.Minute {
		w.count = 0
		w.start = time.Now()
	}
	w.count++
	return w.count > rpm
}

type rateLimitWindow struct {
	mu    sync.Mutex
	count int
	start time.Time
}
