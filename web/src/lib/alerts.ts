import type { Host, Incident, Pending } from './api'

export type AlertItem = {
  id: string
  kind: 'approval' | 'escalated' | 'finding' | 'open' | 'silent'
  tone: 'crit' | 'warn' | 'info'
  title: string
  detail: string
  at: string
  href: string
}

export const SILENT_AFTER_MS = 3 * 60_000

// Turns live data into the things worth interrupting a person for. Anything resolved drops out;
// the ids are stable, so the same situation is never announced twice.
export function deriveAlerts(hosts: Host[], incidents: Incident[], pending: Pending[], now = Date.now()): AlertItem[] {
  const out: AlertItem[] = []

  for (const p of pending) {
    out.push({
      id: `approval:${p.id}`, kind: 'approval', tone: p.dangerous ? 'crit' : 'warn',
      title: `Approval needed: ${p.action_id.replace(/_/g, ' ')} on ${p.host_name}`,
      detail: p.rationale || p.summary || 'The AI agent proposed a fix and is waiting for your decision.',
      at: p.created_at, href: `#/incidents/${p.incident_id}`,
    })
  }

  for (const i of incidents) {
    if (i.status === 'resolved') continue
    if (i.status === 'escalated') {
      out.push({
        id: `escalated:${i.id}`, kind: 'escalated', tone: 'crit',
        title: `The problem is still there on ${i.host_name}`,
        detail: 'The agent ran its fix, but the metric did not recover. A person needs to look.',
        at: i.opened_at, href: `#/incidents/${i.id}`,
      })
    } else if (i.summary) {
      out.push({
        id: `finding:${i.id}`, kind: 'finding', tone: 'info',
        title: `AI found a likely cause on ${i.host_name}`, detail: i.summary,
        at: i.opened_at, href: `#/incidents/${i.id}`,
      })
    } else {
      out.push({
        id: `open:${i.id}`, kind: 'open', tone: 'warn',
        title: `Incident opened on ${i.host_name}`,
        detail: 'No remediation agent has reported on this yet.',
        at: i.opened_at, href: `#/incidents/${i.id}`,
      })
    }
  }

  for (const h of hosts) {
    if (h.last_seen && now - new Date(h.last_seen).getTime() > SILENT_AFTER_MS) {
      out.push({
        id: `silent:${h.id}`, kind: 'silent', tone: 'warn',
        title: `No data from ${h.name}`, detail: 'The monitoring agent has stopped reporting. The server may be down or the agent stopped.',
        at: h.last_seen, href: '#/hosts',
      })
    }
  }

  const rank = { crit: 0, warn: 1, info: 2 }
  return out.sort((a, b) => rank[a.tone] - rank[b.tone] || b.at.localeCompare(a.at))
}
