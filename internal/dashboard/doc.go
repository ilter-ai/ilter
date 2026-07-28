// Package admin implements the management API (JSON/HTTP) for CRUD operations
// on ilter resources: API keys, groups, users, prompts, guardrails, MCP
// servers, MCP grants, OpenAPI specs, jobs, and runtime config.
//
// Relationship with dashboard/: admin/ is the machine-facing JSON API for
// programmatic management (used by the CLI and automation). dashboard/ is
// the human-facing server-rendered dashboard UI (Astro frontend + Go API
// handlers). Both manage the control plane. If you are adding a new
// management endpoint: admin/ for JSON API, dashboard/ for UI pages.
package dashboard
