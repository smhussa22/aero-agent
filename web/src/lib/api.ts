// Typed client for the Go API. The session token lives in sessionStorage, so closing the tab signs out.

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
export type IncidentDetail = Incident & { actions: Action[] }
export type Pending = {
  id: string; incident_id: string; host_id: string; host_name: string; action_id: string; params: Record<string, string>
  rationale: string; dangerous: boolean; summary: string; created_at: string
}
export type AuditEntry = { id: number; at: string; actor: string; event: string; incident_id: string; details: Record<string, unknown> }
export type User = { id: string; email: string; role: 'admin' | 'viewer' }

export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message) }
}

const KEY = 'aero.session'
let onUnauthorized: () => void = () => {}

export const session = {
  get: (): { token: string; user: User } | null => {
    try { return JSON.parse(sessionStorage.getItem(KEY) ?? 'null') } catch { return null }
  },
  set: (v: { token: string; user: User }) => sessionStorage.setItem(KEY, JSON.stringify(v)),
  clear: () => sessionStorage.removeItem(KEY),
  onExpire: (fn: () => void) => { onUnauthorized = fn },
}

export async function api<T>(path: string, init: { method?: string; body?: unknown; signal?: AbortSignal } = {}): Promise<T> {
  const s = session.get()
  const res = await fetch(path, {
    method: init.method ?? 'GET',
    headers: { ...(init.body !== undefined ? { 'Content-Type': 'application/json' } : {}), ...(s ? { Authorization: `Bearer ${s.token}` } : {}) },
    body: init.body !== undefined ? JSON.stringify(init.body) : undefined,
    signal: init.signal,
  })
  if (res.status === 401 && path !== '/api/login') { session.clear(); onUnauthorized() }
  if (res.status === 204) return undefined as T
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new ApiError(res.status, (data as { error?: string }).error ?? `request failed (${res.status})`)
  return data as T
}

export async function login(email: string, password: string): Promise<User> {
  const r = await api<{ token: string; user: User }>('/api/login', { method: 'POST', body: { email, password } })
  session.set({ token: r.token, user: r.user })
  return r.user
}

export async function logout() {
  try { await api('/api/logout', { method: 'POST' }) } finally { session.clear() }
}

export const q = encodeURIComponent
