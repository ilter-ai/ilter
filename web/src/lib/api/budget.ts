import { request } from './request'
import type { BudgetKeyItem, BudgetSummary, GroupBudgetItem, UserBudgetItem } from './types'

export async function getBudgetSummary(): Promise<BudgetSummary> {
  return request<BudgetSummary>('/budget')
}

export async function getUserBudget(userId: number): Promise<UserBudgetItem> {
  return request<UserBudgetItem>(`/budget/user/${userId}`)
}

export async function getGroupBudget(groupId: number): Promise<GroupBudgetItem> {
  return request<GroupBudgetItem>(`/budget/group/${groupId}`)
}

export async function setUserBudget(userId: number, monthlyBudget?: number): Promise<UserBudgetItem> {
  return request<UserBudgetItem>(`/budget/user/${userId}`, {
    method: 'POST',
    body: JSON.stringify({
      ...(monthlyBudget !== undefined ? { monthly_budget: monthlyBudget } : {}),
    }),
  })
}

export async function setGroupBudget(groupId: number, monthlyBudget?: number): Promise<GroupBudgetItem> {
  return request<GroupBudgetItem>(`/budget/group/${groupId}`, {
    method: 'POST',
    body: JSON.stringify({
      ...(monthlyBudget !== undefined ? { monthly_budget: monthlyBudget } : {}),
    }),
  })
}

export async function getKeyBudget(keyId: string): Promise<BudgetKeyItem> {
  return request<BudgetKeyItem>(`/budget/key/${keyId}`)
}

export async function setKeyBudget(
  keyId: string,
  monthlyBudgetUsd: number,
  monthlyBudgetTokens: number,
): Promise<BudgetKeyItem> {
  return request<BudgetKeyItem>(`/budget/key/${keyId}`, {
    method: 'POST',
    body: JSON.stringify({
      monthly_budget_usd: monthlyBudgetUsd,
      monthly_budget_tokens: monthlyBudgetTokens,
    }),
  })
}
