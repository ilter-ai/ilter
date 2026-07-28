import { request } from './request'
import type { GroupRateLimit, RateLimitKeyItem, RateLimitSummary, UserRateLimit } from './types'

export async function getRateLimitSummary(): Promise<RateLimitSummary> {
  return request<RateLimitSummary>('/rate-limits')
}

export async function getUserRateLimit(userId: number): Promise<UserRateLimit> {
  return request<UserRateLimit>(`/rate-limits/user/${userId}`)
}

export async function getGroupRateLimit(groupId: number): Promise<GroupRateLimit> {
  return request<GroupRateLimit>(`/rate-limits/group/${groupId}`)
}

export async function getKeyRateLimit(keyId: string): Promise<RateLimitKeyItem> {
  return request<RateLimitKeyItem>(`/rate-limits/key/${keyId}`)
}

export async function setUserRateLimit(userId: number, rpmLimit: number, retryAfter?: number): Promise<void> {
  await request(`/rate-limits/user/${userId}`, {
    method: 'PUT',
    body: JSON.stringify({ rpm_limit: rpmLimit, retry_after: retryAfter ?? 0 }),
  })
}

export async function setGroupRateLimit(groupId: number, rpmLimit: number, retryAfter?: number): Promise<void> {
  await request(`/rate-limits/group/${groupId}`, {
    method: 'PUT',
    body: JSON.stringify({ rpm_limit: rpmLimit, retry_after: retryAfter ?? 0 }),
  })
}

export async function setKeyRateLimit(keyId: string, rpmLimit: number, retryAfter?: number): Promise<void> {
  await request(`/rate-limits/key/${keyId}`, {
    method: 'PUT',
    body: JSON.stringify({ rpm_limit: rpmLimit, retry_after: retryAfter ?? 0 }),
  })
}
