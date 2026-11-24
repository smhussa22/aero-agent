// Typed client for the Go API.

export type Host = { id: string; name: string; mode: 'observe' | 'recommend' | 'autonomous'; last_seen: string | null }
export type Point = { time: string; value: number }
export type Latest = { metric: string; label: string; value: number; time: string }
export type Rule = {
  id: string; host_id: string | null; name: string; metric: string; label: string
  operator: '>' | '>=' | '<' | '<='; threshold: number; duration_seconds: number
  severity: 'info' | 'warning' | 'critical'; enabled: boolean
}
export type Incident = {
  id: string; host_id: string; host_name: string; alert_id: string
  status: 'open' | 'investigating' | 'remediating' | 'resolved' | 'escalated'
  summary: string; opened_at: string; resolved_at: string | null
}
export type Action = {
  id: string; action_id: string; params: Record<string, string>; rationale: string
  dangerous: boolean; dry_run: boolean; status: string; created_at: string
}
export type Trigger = { rule: string; metric: string; label: string; operator: string; severity: string; value: number; threshold: number }
export type IncidentDetail = Incident & { trigger: Trigger; actions: Action[] }
export type Pending = {
  id: string; incident_id: string; host_id: string; host_name: string; action_id: string; params: Record<string, string>
  rationale: string; dangerous: boolean; summary: string; created_at: string
}
export type AuditEntry = { id: number; at: string; actor: string; event: string; incident_id: string; details: Record<string, unknown> }
export type User = { id: string; email: string; role: 'admin' | 'viewer'; org: { id: string; name: string } }
export type Member = { id: string; email: string; role: 'admin' | 'viewer'; created_at: string }
export type Invite = { id: string; email: string; role: 'admin' | 'viewer'; expires_at: string }
export type Team = { members: Member[]; invites: Invite[] }

export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message) }
}

// The session is an HttpOnly cookie the server sets after sign-in. Script cannot read it, so a script injected into
// the page cannot steal it. A 401 simply means "not signed in".
let onUnauthorized: () => void = () => {}
let me: User | null = null

export const current = {
  get: (): User | null => me,
  set: (u: User | null) => { me = u },
  onExpire: (fn: () => void) => { onUnauthorized = fn },
}

export async function api<T>(path: string, init: { method?: string; body?: unknown; signal?: AbortSignal } = {}): Promise<T> {
  const res = await fetch(path, {
    method: init.method ?? 'GET',
    headers: init.body !== undefined ? { 'Content-Type': 'application/json' } : {},
    body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
    signal: init.signal,
  })
  if (res.status === 401) { current.set(null); onUnauthorized() }
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, (data as { error?: string }).error ?? `request failed (${res.status})`)
  return data as T
}

export const fetchMe = () => api<User>('/api/me')

export async function logout() {
  await fetch('/auth/logout', { method: 'POST' }).catch(() => {})
  current.set(null)
}

export const q = encodeURIComponent
