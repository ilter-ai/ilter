import { request } from './request'
import type { CreateUserRequest, UpdateUserRequest, User } from './types'

interface GoUsersResponse {
  users: User[]
}

export async function getUsers(): Promise<User[]> {
  const res = await request<GoUsersResponse>('/users')
  return res.users || []
}

export async function getUser(id: number): Promise<User> {
  return request<User>(`/users/${id}`)
}

export async function createUser(data: CreateUserRequest): Promise<User> {
  return request<User>('/users', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateUser(id: number, data: UpdateUserRequest): Promise<User> {
  return request<User>(`/users/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteUser(id: number): Promise<void> {
  await request(`/users/${id}`, { method: 'DELETE' })
}
