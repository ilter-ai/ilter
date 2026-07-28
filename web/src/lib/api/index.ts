/**
 * ILTER dashboard API client — domain-split barrel.
 *
 * All API functions are organized into domain modules and re-exported
 * through the `api` aggregator object: `api.dashboard.getDashboardStats()`.
 * Standalone function imports also work: `import { getDashboardStats } from './api'`.
 */

export * from './apiKeys'
export * from './budget'
export * from './cache'
export * from './chat'
export * from './circuitBreaker'
// access is NOT re-exported via wildcard to avoid grant function conflicts
// with mcp.ts. Use api.access.* for access control grants or api.mcp.* for
// MCP server-scoped grants (listServerGrants, createServerGrant, deleteServerGrant).
export * from './costs'
export * from './dashboard'
export * from './features'
export * from './groups'
export * from './guardrails'
export * from './insights'
export * from './jobs'
export * from './loops'
export * from './mcp'
export * from './models'
export * from './openapi'
export * from './pii'
export * from './prompts'
export * from './providers'
export * from './rateLimits'
export type { ApiError } from './request'
export { request } from './request'
export * from './requests'
export * from './strategies'
export type * from './types'
export * from './users'

import * as access from './access'
import * as apiKeys from './apiKeys'
import * as budget from './budget'
import * as cache from './cache'
import * as chat from './chat'
import * as circuitBreaker from './circuitBreaker'
import * as costs from './costs'
import * as dashboard from './dashboard'
import * as fallback from './fallback'
import * as features from './features'
import * as groups from './groups'
import * as guardrails from './guardrails'
import * as insights from './insights'
import * as jobs from './jobs'
import * as loops from './loops'
import * as mcp from './mcp'
import * as models from './models'
import * as openapi from './openapi'
import * as pii from './pii'
import * as prompts from './prompts'
import * as providers from './providers'
import * as rateLimits from './rateLimits'
import * as requests from './requests'
import * as strategies from './strategies'
import * as users from './users'

export const api = {
  dashboard,
  requests,
  apiKeys,
  access,
  chat,
  costs,
  models,
  providers,
  prompts,
  mcp,
  openapi,
  pii,
  loops,
  guardrails,
  rateLimits,
  budget,
  circuitBreaker,
  fallback,
  features,
  cache,

  users,
  groups,
  insights,
  jobs,
  strategies,
}
