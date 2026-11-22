import { describe, expect, it } from 'vitest'
import { deriveAlerts } from './alerts'
import type { Host, Incident, Pending } from './api'

const now = Date.parse('2026-01-01T12:00:00Z')
const host = (id: string, last: string | null): Host => ({ id, name: id, mode: 'observe', last_seen: last })
const inc = (id: string, status: Incident['status'], summary = ''): Incident =>
  ({ id, host_id: 'web-1', host_name: 'web-1', alert_id: 'a' + id, status, summary, opened_at: '2026-01-01T11:50:00Z', resolved_at: null })
const pend = (id: string, dangerous: boolean): Pending =>
  ({ id, incident_id: 'i1', host_id: 'web-1', host_name: 'web-1', action_id: 'kill_process', params: {}, rationale: 'runaway', dangerous, summary: '', created_at: '2026-01-01T11:55:00Z' })

describe('deriveAlerts', () => {
  it('raises approvals, escalations, findings, unattended incidents and silent hosts', () => {
    const alerts = deriveAlerts(
      [host('web-1', '2026-01-01T11:59:00Z'), host('db-1', '2026-01-01T11:50:00Z')],
      [inc('1', 'escalated'), inc('2', 'investigating', 'nginx is stuck'), inc('3', 'open')],
      [pend('p1', true)], now)
    expect(alerts.map((a) => a.kind).sort()).toEqual(['approval', 'escalated', 'finding', 'open', 'silent'])
    expect(alerts.find((a) => a.kind === 'silent')!.title).toContain('db-1')
  })
  it('drops resolved incidents and fresh hosts', () => {
    expect(deriveAlerts([host('web-1', '2026-01-01T11:59:30Z')], [inc('1', 'resolved', 'done')], [], now)).toEqual([])
  })
  it('does not call a never-seen host silent', () => {
    expect(deriveAlerts([host('new', null)], [], [], now)).toEqual([])
  })
  it('sorts critical first and gives stable ids', () => {
    const a = deriveAlerts([], [inc('2', 'investigating', 'x')], [pend('p1', true), pend('p2', false)], now)
    expect(a.map((x) => x.tone)).toEqual(['crit', 'warn', 'info'])
    expect(deriveAlerts([], [inc('2', 'investigating', 'x')], [pend('p1', true), pend('p2', false)], now).map((x) => x.id)).toEqual(a.map((x) => x.id))
  })
  it('marks a dangerous action critical and a safe one a warning', () => {
    expect(deriveAlerts([], [], [pend('p1', true)], now)[0].tone).toBe('crit')
    expect(deriveAlerts([], [], [pend('p1', false)], now)[0].tone).toBe('warn')
  })
})
