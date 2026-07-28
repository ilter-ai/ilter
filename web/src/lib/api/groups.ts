import { request } from './request'
import { extractArray } from './responseHelpers'
import type { CreateGroupRequest, Group, GroupMember, UpdateGroupRequest } from './types'

export async function getGroups(): Promise<Group[]> {
  const res = await request<{ groups: Group[] }>('/groups')
  return extractArray(res, 'groups')
}

export async function getGroup(id: number): Promise<Group> {
  return request<Group>(`/groups/${id}`)
}

export async function createGroup(data: CreateGroupRequest): Promise<Group> {
  return request<Group>('/groups', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateGroup(id: number, data: UpdateGroupRequest): Promise<Group> {
  return request<Group>(`/groups/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteGroup(id: number): Promise<void> {
  await request(`/groups/${id}`, { method: 'DELETE' })
}

export async function getGroupMembers(groupId: number): Promise<GroupMember[]> {
  const res = await request<{ members: GroupMember[] }>(`/groups/${groupId}/members`)
  return extractArray(res, 'members')
}

export async function addGroupMember(groupId: number, userId: number): Promise<void> {
  await request(`/groups/${groupId}/members`, {
    method: 'POST',
    body: JSON.stringify({ user_id: userId }),
  })
}

export async function removeGroupMember(groupId: number, userId: number): Promise<void> {
  await request(`/groups/${groupId}/members/${userId}`, { method: 'DELETE' })
}
