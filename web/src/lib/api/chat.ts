import { buildUrl } from './queryBuilder'
import { request } from './request'
import type { ChatMessage, ChatThread } from './types'

export async function listThreads(): Promise<{ conversations: ChatThread[] }> {
  return request('/chat/threads')
}

export async function createThread(input?: { title?: string }): Promise<{ conversation: ChatThread }> {
  return request('/chat/threads', {
    method: 'POST',
    body: JSON.stringify(input ?? {}),
  })
}

export async function getThread(id: string): Promise<{ conversation: ChatThread; messages: ChatMessage[] }> {
  return request(`/chat/threads/${id}`)
}

export async function updateThread(id: string, input: { title: string }): Promise<{ conversation: ChatThread }> {
  return request(`/chat/threads/${id}`, {
    method: 'PUT',
    body: JSON.stringify(input),
  })
}

export async function deleteThread(id: string): Promise<{ success: boolean }> {
  return request(`/chat/threads/${id}`, { method: 'DELETE' })
}

export async function addMessage(
  conversationId: string,
  input: {
    role: string
    content: string
    model?: string
    token_count?: number
    cost?: number
    reasoning_content?: string
    tool_calls?: string
    usage_cost?: number
    billing_key?: string
  },
): Promise<{ message: ChatMessage }> {
  return request(`/chat/threads/${conversationId}/messages`, {
    method: 'POST',
    body: JSON.stringify(input),
  })
}

export async function listMessages(
  conversationId: string,
  params?: { limit?: number; before_id?: number },
): Promise<{ messages: ChatMessage[]; has_more: boolean }> {
  return request(buildUrl(`/chat/threads/${conversationId}/messages`, params))
}
