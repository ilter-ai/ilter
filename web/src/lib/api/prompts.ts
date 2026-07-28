import { adaptPromptTemplate } from './adapters'
import { request } from './request'
import type { PromptTemplate } from './types'

interface GoPromptTemplate {
  id: number
  name: string
  content: string
  description: string
  version: string
  is_active: boolean
  labels: string[]
  created_at: string
  updated_at: string
}

interface GoPromptTemplatesResponse {
  prompts: GoPromptTemplate[]
}

export async function getPromptTemplates(): Promise<PromptTemplate[]> {
  const res = await request<GoPromptTemplatesResponse>('/prompts')
  return (res.prompts || []).map(adaptPromptTemplate)
}

export async function createPromptTemplate(data: {
  name: string
  content: string
  variables: string[]
}): Promise<{ id: string }> {
  return request<{ id: string }>('/prompts', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updatePromptTemplate(
  id: string,
  data: { name: string; content: string; variables: string[] },
): Promise<void> {
  await request(`/prompts/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ id, ...data }),
  })
}

export async function deletePromptTemplate(id: string): Promise<void> {
  await request(`/prompts/${id}`, { method: 'DELETE' })
}
