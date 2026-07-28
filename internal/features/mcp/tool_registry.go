// Package mcp provides the MCP (Model Context Protocol) integration layer.
//
// ProviderSet aggregates multiple tool providers (MCP, OpenAPI, GraphQL, etc.)
// into a single inject/execute pair.
//
// Each registered Provider has a Prefix string. ProviderSet routes tool calls
// to the right provider by matching the prefix against the tool name.
//
// Instead of an interface with adapters per provider, we use struct+func-fields
// — MCP's inject and execute already live on two different objects, so an
// interface would require a wrapper struct per provider without adding value.
package mcp

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/ilter-ai/ilter/internal/model"
)

// Provider describes one tool namespace: how to list its tools and how to
// execute calls that belong to this namespace. Both fields satisfy the
// middleware's injectFn/executeFn signatures directly.
type Provider struct {
	Name    string                                                                                               // unique label for logging
	Prefix  string                                                                                               // tool name prefix (e.g. "openapi__")
	Match   func(string) bool                                                                                    // optional custom match function for routing tool calls
	Tools   func(keyID string, groupIDs []int) []model.Tool                                                      // injectFn-compatible
	Execute func(ctx context.Context, keyID, keyPrefix string, calls []model.ToolCall) ([]model.Message, []bool) // executeFn-compatible
}

// ProviderSet merges multiple Providers into a single inject + execute pair.
type ProviderSet struct {
	providers []Provider
}

// New creates a ProviderSet. Returns an error on duplicate or empty provider
// names.
func New(providers ...Provider) (*ProviderSet, error) {
	seen := make(map[string]int, len(providers))
	for i, p := range providers {
		if p.Name == "" {
			return nil, fmt.Errorf("mcp.ProviderSet[%d]: empty name", i)
		}
		if _, dup := seen[p.Name]; dup {
			return nil, fmt.Errorf("mcp.ProviderSet: duplicate provider name %q", p.Name)
		}
		seen[p.Name] = i
	}
	return &ProviderSet{providers: slices.Clone(providers)}, nil
}

// Inject calls every provider's Tools function and merges the result.
// Satisfies the middleware injectFn signature when assigned by value.
func (r *ProviderSet) Inject(keyID string, groupIDs []int) []model.Tool {
	maxTools := 0
	for _, p := range r.providers {
		if p.Tools != nil {
			maxTools += 32 // guess
		}
	}
	all := make([]model.Tool, 0, maxTools)

	for _, p := range r.providers {
		if p.Tools == nil {
			continue
		}
		all = append(all, p.Tools(keyID, groupIDs)...)
	}
	return all
}

// Execute routes each tool call to the provider whose Prefix matches,
// creates a single composite assistant message, and returns results in
// the original call order.
//
// Satisfies the middleware executeFn signature when assigned by value.
//
// Each provider's assistant message is discarded — ProviderSet builds one.
// Unmatched calls receive an error result so the upstream never stalls.
func (r *ProviderSet) Execute(ctx context.Context, keyID, keyPrefix string, calls []model.ToolCall) ([]model.Message, []bool) {
	if len(calls) == 0 {
		return nil, nil
	}

	// 1 — Group calls by owning provider, collect results.
	type result struct {
		msg model.Message
		err bool
	}
	results := make(map[string]result, len(calls))

	for _, p := range r.providers {
		if p.Execute == nil {
			continue
		}
		owned := pickByPrefix(calls, p)
		if len(owned) == 0 {
			continue
		}
		msgs, errs := p.Execute(ctx, keyID, keyPrefix, owned)
		// msgs[0] is the provider's assistant message — skip.
		// errs[i] maps to msgs[i+1] (tool results only).
		toolIdx := 0
		for _, m := range msgs {
			if m.Role != "tool" {
				continue
			}
			isErr := toolIdx < len(errs) && errs[toolIdx]
			results[m.ToolCallID] = result{msg: m, err: isErr}
			toolIdx++
		}
	}

	// 2 — Composite assistant message (one, with every call the LLM made).
	assistantMsg := model.Message{
		Role:      "assistant",
		ToolCalls: calls,
		Content:   "",
	}
	out := make([]model.Message, 1, 1+len(calls))
	out[0] = assistantMsg
	errFlags := make([]bool, 0, len(calls))

	// 3 — Emit results in original call order; fill gaps with errors.
	var validNames string // computed lazily, only if a call actually goes unmatched
	for _, c := range calls {
		res, ok := results[c.ID]
		if !ok {
			if validNames == "" {
				validNames = strings.Join(r.toolNames(keyID), ", ")
			}
			var msg string
			if c.Function.Name == "" {
				msg = fmt.Sprintf(
					"Error: this tool call is missing a function name, so it could not be routed. "+
						"Retry the call with an exact, non-empty \"name\" from this list: %s",
					validNames,
				)
			} else {
				msg = fmt.Sprintf(
					"Error: no tool named %q is available. "+
						"Retry the call with an exact name from this list: %s",
					c.Function.Name, validNames,
				)
			}
			out = append(out, model.Message{
				Role:       "tool",
				ToolCallID: c.ID,
				Name:       c.Function.Name,
				Content:    msg,
			})
			errFlags = append(errFlags, true)
			continue
		}
		out = append(out, res.msg)
		errFlags = append(errFlags, res.err)
	}

	return out, errFlags
}

// toolNames returns every tool name currently authorized for this key, for
// use in a self-correction hint when a tool call can't be routed. Execute
// doesn't carry groupIDs, so this is scoped by keyID only — a slightly
// narrower list than group-based grants might allow, acceptable since this
// is just a hint, not an authorization decision.
func (r *ProviderSet) toolNames(keyID string) []string {
	tools := r.Inject(keyID, nil)
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Function.Name)
	}
	return names
}

// pickByPrefix returns the subset of calls whose function name matches the
// provider's prefix or custom Match function.
//
// Must not mutate calls: Execute invokes this once per provider against the
// same backing slice, then does a final pass over the original calls to
// build ordered output. slices.DeleteFunc mutates in place and zeroes
// elements past the new length — reusing it here corrupted the shared
// backing array (a later provider's empty match zeroed out an earlier
// provider's already-matched entries), which is why an already-resolved
// tool call would show as empty by the time Execute's final loop read it.
func pickByPrefix(calls []model.ToolCall, p Provider) []model.ToolCall {
	matches := func(c model.ToolCall) bool {
		if p.Match != nil {
			return p.Match(c.Function.Name)
		}
		if p.Prefix == "" {
			return true
		}
		return len(c.Function.Name) >= len(p.Prefix) && c.Function.Name[:len(p.Prefix)] == p.Prefix
	}
	owned := make([]model.ToolCall, 0, len(calls))
	for _, c := range calls {
		if matches(c) {
			owned = append(owned, c)
		}
	}
	return owned
}
