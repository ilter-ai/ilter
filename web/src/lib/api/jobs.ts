import { buildUrl } from './queryBuilder'
import { request } from './request'
import type { Job, JobRun, JobStats, Trigger } from './types'

export async function listJobs(): Promise<Job[]> {
  // Backend returns a raw JSON array of jobs (each with optional triggers).
  return request<Job[]>('/jobs')
}

export async function getJob(id: string): Promise<Job> {
  // Backend returns JobResponse directly (not wrapped in { job: ... }).
  return request<Job>(`/jobs/${encodeURIComponent(id)}`)
}

export async function createJob(data: Partial<Job>): Promise<Job> {
  return request<Job>('/jobs', {
    method: 'POST',
    body: JSON.stringify(data),
  })
}

export async function updateJob(id: string, data: Partial<Job>): Promise<Job> {
  return request<Job>(`/jobs/${encodeURIComponent(id)}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function deleteJob(id: string): Promise<void> {
  await request(`/jobs/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

export async function triggerJob(id: string): Promise<{ run_id: string }> {
  return request<{ run_id: string }>(`/jobs/${encodeURIComponent(id)}/trigger`, { method: 'POST' })
}

export async function listJobRuns(
  jobId: string,
  params?: { limit?: number; offset?: number },
): Promise<{ runs: JobRun[]; total: number }> {
  const per_page = params?.limit || 20
  const page = params?.offset ? Math.floor(params.offset / per_page) + 1 : 1
  const url = buildUrl(`/jobs/${encodeURIComponent(jobId)}/runs`, { per_page, page })
  // Backend returns { data: [...], total, page, per_page }
  const res = await request<{ data: JobRun[]; total: number; page: number; per_page: number }>(url)
  return { runs: res.data, total: res.total }
}

export async function getJobStats(): Promise<JobStats> {
  return request<JobStats>('/jobs/stats')
}

export async function listJobTriggers(jobId: string): Promise<Trigger[]> {
  return request<Trigger[]>(`/jobs/${encodeURIComponent(jobId)}/triggers`)
}

export async function deleteTrigger(id: string): Promise<void> {
  await request(`/triggers/${encodeURIComponent(id)}`, { method: 'DELETE' })
}

// revealTrigger fetches a webhook trigger's token + HMAC secret on demand.
// Unlike the rest of the trigger API, this can be called repeatedly — the
// credentials aren't only shown once at creation.
export async function revealTrigger(jobId: string, triggerId: string): Promise<{ token: string; secret: string }> {
  return request<{ token: string; secret: string }>(
    `/jobs/${encodeURIComponent(jobId)}/triggers/${encodeURIComponent(triggerId)}/reveal`,
  )
}
