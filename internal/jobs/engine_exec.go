package jobs

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ilter-ai/ilter/internal/features/mcp"
	"github.com/ilter-ai/ilter/internal/model"
)

func (r *JobRunner) runExecution(ctx context.Context, job Job, run *JobRun, start time.Time) {
	select {
	case r.sem <- struct{}{}:
		defer func() { <-r.sem }()
	case <-ctx.Done():
		return
	}

	timeout := time.Duration(job.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = time.Duration(r.cfg.DefaultTimeoutMs) * time.Millisecond
	}

	successfullyCompleted := false
	defer func() {
		if p := recover(); p != nil {
			if run.ID != "" {
				run.Status = "llm_failed"
				run.LLMError = sqlNullString(fmt.Sprintf("panic: %v", p))
				failRun(run, start)
				if err := r.store.UpdateRun(ctx, run); err != nil {
					r.logger.Error("failed to update run after panic", "job", job.ID, "error", err)
				}
			}
			r.logger.Error(
				"recovered from panic in job execution",
				"job", job.ID,
				"run", run.ID,
				"panic", p,
			)
		}
		if !successfullyCompleted && run.ID != "" && run.Status != "pending" {
			if run.Status == "running" || run.Status == "" {
				run.Status = "llm_failed"
				if !run.LLMError.Valid || run.LLMError.String == "" {
					run.LLMError = sqlNullString("execution terminated without completion")
				}
				failRun(run, start)
				if err := r.store.UpdateRun(ctx, run); err != nil {
					r.logger.Error("failed to update run on incomplete path", "job", job.ID, "error", err)
				}
			}
		}
	}()

	lockKey := fmt.Sprintf("job:lock:%s", job.ID)
	if r.lock != nil {
		ok, err := r.lock.TryLock(ctx, lockKey, timeout)
		if err != nil {
			r.logger.Warn("lock attempt failed, proceeding anyway", "job", job.ID, "error", err)
		} else if !ok {
			run.Status = "skipped_locked"
			failRun(run, start)
			if err := r.store.UpdateRun(ctx, run); err != nil {
				r.logger.Error("failed to update run record", "job", job.ID, "error", err)
			}
			return
		}
		defer func() {
			if uErr := r.lock.Unlock(ctx, lockKey); uErr != nil {
				r.logger.Warn("failed to release job lock", "job", job.ID, "error", uErr)
			}
		}()
	}

	vars, err := ResolveVariables(job.VariablesConfig, r.cfg.MaxVarLength)
	if err != nil {
		if !run.LLMError.Valid || run.LLMError.String == "" {
			run.LLMError = sqlNullString(fmt.Sprintf("variable resolution: %v", err))
		}
		run.Status = StatusDeadLetter
		failRun(run, start)
		if err := r.store.UpdateRun(ctx, run); err != nil {
			r.logger.Error("failed to update run record", "job", job.ID, "error", err)
		}
		return
	}

	llmResult, stepsErr := r.runSteps(ctx, &job, run, vars)
	if stepsErr != nil {
		if !run.LLMError.Valid || run.LLMError.String == "" {
			run.LLMError = sqlNullString(stepsErr.Error())
		}
		if isLLMCallError(stepsErr) {
			r.retryOrFail(run, start)
		} else {
			run.Status = StatusDeadLetter
			failRun(run, start)
		}
		if err := r.store.UpdateRun(ctx, run); err != nil {
			r.logger.Error("failed to update run record", "job", job.ID, "error", err)
		}
		return
	}
	run.Status = "llm_success"

	if job.DeliveryConfig != "" && job.DeliveryConfig != "{}" {
		if delErr := Deliver(ctx, job.DeliveryConfig, run, llmResult); delErr != nil {
			run.DeliveryError = sqlNullString(delErr.Error())
			run.Status = "delivery_failed"
		} else {
			run.DeliveryResult = sqlNullString("delivered")
		}
	}

	if run.Status != "delivery_failed" {
		run.Status = "success"
	}

	now := time.Now()
	run.FinishedAt = sqlNullTime(now)
	run.DurationMs = int(time.Since(start).Milliseconds())
	if err := r.store.UpdateRun(ctx, run); err != nil {
		r.logger.Error("failed to update run record", "job", job.ID, "error", err)
	} else {
		successfullyCompleted = true
	}
}

func (r *JobRunner) callLLM(ctx context.Context, modelName string, prompt string, billingKeyID string) (result string, actualModel string, tokensIn, tokensOut int, cost float64, err error) {
	// proxy strips any provider prefix — single responsibility in handler.go
	// stream=false is required: cost comes from response header (X-Ilter-Cost),
	// which streaming responses cannot set (headers flushed before cost is known).
	//
	// Resolve billing key: job's own APIKeyID takes precedence, then default
	// billing key from config. Both empty is a hard error — an un-attributed
	// request silently falls to the admin key, bypassing per-key budget/RPM.
	effectiveBillingKey := billingKeyID
	if effectiveBillingKey == "" {
		effectiveBillingKey = r.cfg.DefaultBillingKeyID
	}
	if effectiveBillingKey == "" {
		return "", "", 0, 0, 0, fmt.Errorf("no billing key for LLM call: job has no api_key_id and no default_billing_key_id configured")
	}

	proxyURL := r.cfg.ProxyURL
	if proxyURL == "" {
		proxyURL = "http://127.0.0.1:8181/v1/chat/completions"
	}
	body := map[string]any{
		"model":      modelName,
		"messages":   []map[string]any{{"role": "user", "content": prompt}},
		"max_tokens": 1024,
		"stream":     false,
	}
	bodyBytes, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, "POST", proxyURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", 0, 0, 0, fmt.Errorf("create proxy request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+r.cfg.APIKey)
	httpReq.Header.Set("X-Ilter-Billing-Key-ID", effectiveBillingKey)

	r.logger.Info("jobs: callLLM via proxy", "url", proxyURL, "model", modelName)

	// use shared http client instead of bare one.
	// Jobs engine already has retryOrFail, so HTTP-level retry is redundant.
	httpResp, err := r.httpCli.Do(httpReq)
	if err != nil {
		return "", "", 0, 0, 0, fmt.Errorf("proxy request: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, _ := io.ReadAll(httpResp.Body)

	switch {
	case httpResp.StatusCode == http.StatusUnauthorized:
		r.logger.Error("jobs: callLLM billing key rejected",
			"billing_key_id", effectiveBillingKey,
			"status", 401,
			"body", string(respBody))
		return "", "", 0, 0, 0, fmt.Errorf("billing key %q rejected by proxy (401): key may be deleted, rotated, or invalid — %s",
			effectiveBillingKey, strings.TrimSpace(string(respBody)))

	case httpResp.StatusCode == http.StatusTooManyRequests:
		body := strings.TrimSpace(string(respBody))
		lower := strings.ToLower(body)
		// Monthly quota errors (e.g. "Monthly usage limit reached") won't
		// resolve within the retry window — mark permanent so the run goes
		// to dead_letter instead of futile retries.
		if strings.Contains(lower, "monthly") &&
			(strings.Contains(lower, "limit") || strings.Contains(lower, "quota") || strings.Contains(lower, "exhaust")) {
			r.logger.Warn("jobs: callLLM monthly quota exceeded (permanent)",
				"status", 429, "body", body)
			return "", "", 0, 0, 0, fmt.Errorf("LLM call: monthly quota exceeded (permanent): %s", body)
		}
		r.logger.Warn("jobs: callLLM rate limited (transient)",
			"status", 429, "body", body)
		return "", "", 0, 0, 0, fmt.Errorf("LLM call: rate limited (transient 429): %s", body)

	case httpResp.StatusCode != http.StatusOK:
		r.logger.Warn("jobs: callLLM proxy non-200", "status", httpResp.StatusCode, "body", string(respBody))
		return "", "", 0, 0, 0, fmt.Errorf("proxy returned status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var chatResp struct {
		model.ChatCompletionResponse
		Error *model.ErrorDetail `json:"error,omitempty"`
	}
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return "", "", 0, 0, 0, fmt.Errorf("decode proxy response: %w", err)
	}
	if chatResp.Error != nil && chatResp.Error.Message != "" {
		return "", "", 0, 0, 0, fmt.Errorf("proxy error: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", "", 0, 0, 0, fmt.Errorf("no choices in proxy response")
	}

	result = chatResp.Choices[0].Message.Content
	actualModel = chatResp.Model
	// DeepSeek models return content in reasoning_content, not content
	if result == "" && chatResp.Choices[0].Message.ReasoningContent != "" {
		result = chatResp.Choices[0].Message.ReasoningContent
	}
	if chatResp.Usage != nil {
		tokensIn = chatResp.Usage.PromptTokens
		tokensOut = chatResp.Usage.CompletionTokens
	}

	costStr := httpResp.Header.Get("X-Ilter-Cost")
	if costStr != "" {
		parsed, err := strconv.ParseFloat(costStr, 64)
		if err != nil {
			r.logger.Warn("jobs: invalid X-Ilter-Cost header", "value", costStr, "error", err)
			cost = 0
		} else {
			cost = parsed
		}
	} else {
		r.logger.Warn("jobs: missing X-Ilter-Cost header from proxy, using 0")
		cost = 0
	}
	return result, actualModel, tokensIn, tokensOut, cost, nil
}

// isLLMCallError reports whether err originated from the LLM provider call itself.
// Provider errors (network blip, rate limit, 5xx) may be transient and worth retrying.
// Errors marked "(permanent)" are non-retryable — they go to dead_letter immediately.
// Everything else (template, config, missing data) is deterministic — retry won't fix it.
func isLLMCallError(err error) bool {
	msg := err.Error()
	// Permanent errors (monthly quota, billing rejection) should NOT be retried.
	if strings.Contains(msg, "(permanent)") {
		return false
	}
	return strings.Contains(msg, "LLM call:")
}

// retryOrFail decides whether a failed run should be retried or marked terminal.
// If run.Attempts < maxAttempts, it sets status='pending' with next_retry_at
// for exponential backoff and returns true (retry scheduled).
// If max attempts exhausted, it sets status='llm_failed' and returns false.
func (r *JobRunner) retryOrFail(run *JobRun, start time.Time) bool {
	if run.Attempts < r.cfg.MaxAttempts {
		run.Status = "retrying"
		if run.LLMError.Valid && run.LLMError.String != "" {
			run.LastError = run.LLMError
		}
		// next_retry_at = now + retryDelayBase * attempts (linear backoff)
		delay := r.cfg.RetryDelayBase * time.Duration(run.Attempts)
		run.NextRetryAt = sqlNullTime(time.Now().Add(delay))
		now := time.Now()
		// Bump started_at on retry so retention-by-age doesn't prune a pending retry.
		run.StartedAt = sqlNullTime(now)
		run.DurationMs = int(now.Sub(start).Milliseconds())
		// Reset terminal fields — the run isn't done yet.
		run.FinishedAt = sql.NullTime{}
		time.AfterFunc(delay, func() {
			r.Reconcile(context.Background(), r.cfg.MaxAttempts)
		})
		return true
	}
	// Max attempts exhausted — dead letter. Clear NextRetryAt: a prior retry
	// attempt may have left it set, and a terminal run must not still look
	// like it has a retry pending.
	run.Status = StatusDeadLetter
	run.NextRetryAt = sql.NullTime{}
	failRun(run, start)
	return false
}

func (r *JobRunner) runSteps(ctx context.Context, job *Job, run *JobRun, vars map[string]any) (string, error) {
	var steps []Step
	if err := json.Unmarshal([]byte(job.StepsJSON), &steps); err != nil {
		return "", fmt.Errorf("parse steps: %w", err)
	}
	if len(steps) == 0 {
		return "", fmt.Errorf("steps is empty")
	}

	var (
		texts                         = make([]string, 0, len(steps))
		raw                           = make([]json.RawMessage, 0, len(steps))
		totalTokensIn, totalTokensOut int
		totalCost                     float64
		progress                      = make([]StepProgress, len(steps))
	)

	// Initialize all steps as pending.
	for i, s := range steps {
		progress[i] = StepProgress{Index: i, Type: s.Type}
		switch s.Type {
		case "mcp":
			progress[i].Tool = s.Tool
		case "llm":
			progress[i].Model = s.Model
		}
	}
	r.writeStepsProgress(ctx, run, progress)

	for i, s := range steps {
		progress[i].Status = "running"
		r.writeStepsProgress(ctx, run, progress)

		t, actualModel, rRaw, tokensIn, tokensOut, cost, stepErr := r.runStep(ctx, job, s, texts, raw, vars)
		totalTokensIn += tokensIn
		totalTokensOut += tokensOut
		totalCost += cost
		if stepErr != nil {
			progress[i].Status = "failed"
			progress[i].Error = stepErr.Error()
			r.writeStepsProgress(ctx, run, progress)
			if b, mErr := json.Marshal(raw); mErr == nil {
				run.LLMResult = sqlNullString(string(b))
			}
			run.PromptTokens = totalTokensIn
			run.CompletionTokens = totalTokensOut
			run.Cost = totalCost
			return "", fmt.Errorf("step %d (%s): %w", i, s.Type, stepErr)
		}
		if actualModel != "" {
			progress[i].Model = actualModel
		}
		progress[i].Status = "done"
		progress[i].Output = truncateProgressOutput(t)
		progress[i].TokensIn = tokensIn
		progress[i].TokensOut = tokensOut
		progress[i].Cost = cost
		r.writeStepsProgress(ctx, run, progress)

		texts = append(texts, t)
		raw = append(raw, rRaw)
	}

	b, err := json.Marshal(raw)
	if err != nil {
		return "", fmt.Errorf("marshal results: %w", err)
	}
	run.LLMResult = sqlNullString(string(b))
	run.PromptTokens = totalTokensIn
	run.CompletionTokens = totalTokensOut
	run.Cost = totalCost
	return texts[len(texts)-1], nil
}

func (r *JobRunner) writeStepsProgress(ctx context.Context, run *JobRun, progress []StepProgress) {
	b, err := json.Marshal(progress)
	if err != nil {
		r.logger.Warn("failed to marshal step progress", "run", run.ID, "error", err)
		return
	}
	run.Steps = sqlNullString(string(b))
	if err := r.store.UpdateRun(ctx, run); err != nil {
		r.logger.Warn("failed to write step progress", "run", run.ID, "error", err)
	}
}

func truncateProgressOutput(s string) string {
	const maxOutput = 1000
	if len(s) <= maxOutput {
		return s
	}
	return s[:maxOutput] + "..."
}

func (r *JobRunner) runStep(ctx context.Context, job *Job, s Step, texts []string, raw []json.RawMessage, vars map[string]any) (string, string, json.RawMessage, int, int, float64, error) {
	tctx := buildTemplateCtx(texts, raw, vars)

	// Re-render variable values as templates so {{.prev}} etc. resolve against
	// the accumulated step results before the step uses them. Without this,
	// variables like {"Input":"{{.prev}}"} stay as the literal string "{{.prev}}".
	for k, v := range vars {
		if str, ok := v.(string); ok {
			if rendered, err := renderTemplate(str, tctx); err == nil && rendered != str {
				tctx[k] = rendered
			}
		}
	}

	switch s.Type {
	case "mcp":
		if r.mcpExec == nil {
			return "", "", nil, 0, 0, 0, fmt.Errorf("MCP executor not initialized (MCP disabled?)")
		}
		args, err := renderTemplate(string(s.Arguments), tctx)
		if err != nil {
			return "", "", nil, 0, 0, 0, fmt.Errorf("render arguments: %w", err)
		}
		if !json.Valid([]byte(args)) {
			return "", "", nil, 0, 0, 0, fmt.Errorf("rendered arguments not valid JSON (missing | json?): %s", args)
		}
		res := r.mcpExec.ExecuteTool(ctx, &mcp.ExecuteToolParams{
			ToolName:  s.Tool,
			Arguments: json.RawMessage(args),
			APIKeyID:  job.APIKeyID,
			KeyPrefix: "cron",
			ClientIP:  "127.0.0.1",
		})
		if res == nil {
			return "", "", nil, 0, 0, 0, fmt.Errorf("tool %s returned nil result", s.Tool)
		}
		if res.IsError {
			return "", "", nil, 0, 0, 0, fmt.Errorf("tool %s returned error: %s", s.Tool, flattenContent(res.Content))
		}
		b, err := json.Marshal(res)
		if err != nil {
			return "", "", nil, 0, 0, 0, fmt.Errorf("marshal result: %w", err)
		}
		return flattenContent(res.Content), "", b, 0, 0, 0, nil

	case "llm":
		if s.PromptID == nil {
			return "", "", nil, 0, 0, 0, fmt.Errorf("prompt_id is nil")
		}
		if s.Model == "" {
			return "", "", nil, 0, 0, 0, fmt.Errorf("model is empty")
		}
		promptContent, err := r.store.GetPrompt(*s.PromptID)
		if err != nil {
			return "", "", nil, 0, 0, 0, fmt.Errorf("get prompt: %w", err)
		}
		if promptContent == "" {
			return "", "", nil, 0, 0, 0, fmt.Errorf("prompt %d not found or empty", *s.PromptID)
		}
		model := s.Model
		rendered, err := renderTemplate(promptContent, tctx)
		if err != nil {
			return "", "", nil, 0, 0, 0, fmt.Errorf("render prompt: %w", err)
		}
		result, actualModel, tokensIn, tokensOut, cost, llmErr := r.callLLM(ctx, model, rendered, job.APIKeyID)
		if llmErr != nil {
			return "", "", nil, 0, 0, 0, fmt.Errorf("LLM call: %w", llmErr)
		}
		b, err := json.Marshal(result)
		if err != nil {
			return "", "", nil, 0, 0, 0, fmt.Errorf("marshal result: %w", err)
		}
		return result, actualModel, b, tokensIn, tokensOut, cost, nil

	default:
		return "", "", nil, 0, 0, 0, fmt.Errorf("unknown step type %q", s.Type)
	}
}
