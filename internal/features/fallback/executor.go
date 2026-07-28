package fallback

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/ilter-ai/ilter/internal/config"
	"github.com/ilter-ai/ilter/internal/platform/cooldown"
	"github.com/ilter-ai/ilter/internal/provider"
)

// FallbackExecutor coordinates candidate execution, error classification,
// cooldown tracking, and fallback retries.
//
//nolint:revive // FallbackExecutor is explicit and matches project conventions
type FallbackExecutor struct {
	cfg           config.FallbackConfig
	cooldownStore cooldown.Store
	reg           *provider.Registry
}

// NewFallbackExecutor creates a new FallbackExecutor instance.
func NewFallbackExecutor(cfg config.FallbackConfig, cooldownStore cooldown.Store, reg *provider.Registry) *FallbackExecutor {
	return &FallbackExecutor{
		cfg:           cfg,
		cooldownStore: cooldownStore,
		reg:           reg,
	}
}

// ExecutionResult holds the outcome of a single attempt.
type ExecutionResult struct {
	Candidate  cooldown.Candidate
	Provider   provider.Provider
	StatusCode int
	Err        error
}

// Execute iterates through ranked candidates, attempting execution with each.
// It uses Classify() to determine the Verdict for failures and manages cooldowns accordingly.
func (fe *FallbackExecutor) Execute(
	ctx context.Context,
	candidates []cooldown.Candidate,
	tryFn func(ctx context.Context, cand cooldown.Candidate, p provider.Provider) (int, http.Header, error),
) (*ExecutionResult, error) {
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no candidates available for execution")
	}

	cooldownDuration := fe.cfg.CooldownDuration
	if cooldownDuration <= 0 {
		cooldownDuration = config.DefaultCooldownDuration
	}

	excludedCandidates := make(map[string]bool)
	excludedKeys := make(map[string]bool)

	var lastErr error
	var lastStatus int

	maxAttempts := fe.cfg.MaxAttempts
	if maxAttempts <= 0 || maxAttempts > len(candidates) {
		maxAttempts = len(candidates)
	}

	attemptCount := 0
	for _, cand := range candidates {
		if attemptCount >= maxAttempts {
			break
		}

		if excludedCandidates[cand.Provider+"|"+cand.Model] {
			continue
		}
		if cand.KeyID != "" && excludedKeys[cand.Provider+"|"+cand.KeyID] {
			continue
		}
		if fe.cooldownStore != nil && fe.cooldownStore.InCooldown(ctx, cand) {
			continue
		}

		p, err := fe.reg.Get(cand.Provider)
		if err != nil {
			slog.Warn("fallback: provider not found in registry", "provider", cand.Provider, "error", err)
			continue
		}

		attemptCount++
		status, headers, tryErr := tryFn(ctx, cand, p)
		if tryErr == nil && (status == 0 || (status >= 200 && status < 300)) {
			slog.Info("fallback: candidate succeeded", "provider", cand.Provider, "model", cand.Model, "key_id", cand.KeyID, "status", status, "attempt", attemptCount)
			return &ExecutionResult{
				Candidate:  cand,
				Provider:   p,
				StatusCode: status,
				Err:        nil,
			}, nil
		}

		lastStatus = status
		lastErr = tryErr

		verdict, dynamicCooldown := ClassifyWithHeaders(status, tryErr, headers)
		if dynamicCooldown <= 0 {
			dynamicCooldown = cooldownDuration
		}

		// 12× cap prevents 367h from Retry-After header; at 5m default this means ~1h max
		maxCooldown := cooldownDuration * 12
		if maxCooldown <= 0 {
			// unreachable after above guard, kept as defense-in-depth
			maxCooldown = 60 * time.Minute
		}
		if dynamicCooldown > maxCooldown {
			slog.Warn("cooldown capped by safety limit", "original", dynamicCooldown, "capped", maxCooldown, "configured", fe.cfg.CooldownDuration)
			incCooldownCapped()
			dynamicCooldown = maxCooldown
		}
		slog.Warn(
			"fallback: candidate failed",
			"provider", cand.Provider,
			"model", cand.Model,
			"key_id", cand.KeyID,
			"status", status,
			"verdict", verdict.String(),
			"cooldown", dynamicCooldown.String(),
			"error", tryErr,
		)

		// A 401/403 with no actual API key attached (cand.APIKey == "") isn't a
		// bad-key situation — there's no key to blacklist. It means this specific
		// model requires auth we don't have configured. Providers that mix
		// keyless free-tier models with key-required paid ones (e.g. opencode_zen)
		// all share the same KeyID "default" when no key is configured, so
		// excluding the (provider, keyID) pair here would also lock out every
		// other — including keyless, working — model on that provider for the
		// full cooldown. Demote to excluding just this one candidate instead.
		if verdict == VerdictExcludeKey && cand.APIKey == "" {
			verdict = VerdictExcludeCandidate
		}

		switch verdict {
		case VerdictFatal, VerdictExcludeCandidate:
			// A generic 400 (Fatal) is classified from an opaque provider error
			// string and can't be proven request-wide — it's frequently specific
			// to this one candidate (e.g. a model-specific quirk replaying a
			// malformed tool-call turn). Exclude just this candidate and keep
			// trying the rest of the fallback chain instead of aborting outright.
			excludedCandidates[cand.Provider+"|"+cand.Model] = true
			if fe.cooldownStore != nil {
				fe.cooldownStore.SetCooldown(ctx, cand, dynamicCooldown)
			}

		case VerdictExcludeKey:
			if cand.KeyID != "" {
				excludedKeys[cand.Provider+"|"+cand.KeyID] = true
			}
			if fe.cooldownStore != nil {
				fe.cooldownStore.SetCooldown(ctx, cand, dynamicCooldown)
			}

		case VerdictRetrySame:
			// Allow next loop iteration to try next candidate or same
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("all %d candidate(s) exhausted or in cooldown", len(candidates))
	}
	if lastStatus == 0 {
		lastStatus = http.StatusServiceUnavailable
	}

	return &ExecutionResult{
		StatusCode: lastStatus,
		Err:        lastErr,
	}, lastErr
}
