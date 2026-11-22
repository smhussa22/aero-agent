import { useState } from 'react'
import { Check, X } from 'lucide-react'
import { api, current, type Incident, type Pending } from '../lib/api'
import { useLive } from '../lib/live'
import { Badge, ErrorLine, useToast, type Tone } from './ui'

const STATUS: Record<Incident['status'], { tone: Tone; label: string }> = {
  open: { tone: 'warn', label: 'Open' },
  investigating: { tone: 'info', label: 'Investigating' },
  remediating: { tone: 'info', label: 'Fixing' },
  resolved: { tone: 'good', label: 'Resolved' },
  escalated: { tone: 'crit', label: 'Escalated' },
}

export function IncidentBadge({ status }: { status: Incident['status'] }) {
  const s = STATUS[status] ?? { tone: 'neutral' as Tone, label: status }
  return <Badge tone={s.tone}>{s.label}</Badge>
}

export function ModeBadge({ mode }: { mode: string }) {
  return <Badge tone={mode === 'autonomous' ? 'warn' : 'neutral'}>{mode[0].toUpperCase() + mode.slice(1)}</Badge>
}

export const isAdmin = () => current.get()?.role === 'admin'

export function paramsText(params: Record<string, unknown>): string {
  return Object.entries(params).map(([k, v]) => `${k}=${String(v)}`).join(' ')
}

// Approve / reject buttons for one pending action. Only admins can decide; the server enforces it too.
export function DecisionButtons({ id, label, onDone }: { id: string; label: string; onDone?: () => void }) {
  const { reload } = useLive()
  const toast = useToast()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  if (!isAdmin()) return <p className="hint">Only an admin can approve or reject.</p>

  const decide = async (approved: boolean) => {
    setBusy(true); setError(null)
    try {
      await api(`/api/actions/${id}/decision`, { method: 'POST', body: { approved } })
      toast({ tone: approved ? 'good' : 'info', title: approved ? 'Approved' : 'Rejected', detail: label })
      reload(); onDone?.()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not save your decision.')
    } finally { setBusy(false) }
  }

  return (
    <div>
      <div className="row">
        <button className="btn primary sm" disabled={busy} onClick={() => decide(true)}><Check size={16} aria-hidden /> Approve</button>
        <button className="btn sm" disabled={busy} onClick={() => decide(false)}><X size={16} aria-hidden /> Reject</button>
      </div>
      <ErrorLine error={error} />
    </div>
  )
}

export function PendingItem({ p }: { p: Pending }) {
  return (
    <div style={{ flex: 1, minWidth: 0 }}>
      <div className="row" style={{ gap: 'var(--s-2)' }}>
        <strong>{p.action_id.replace(/_/g, ' ')}</strong>
        {p.dangerous && <Badge tone="crit">Dangerous</Badge>}
        <span className="muted small">on {p.host_name}</span>
      </div>
      <p className="mono muted" style={{ overflowWrap: 'anywhere' }}>{paramsText(p.params)}</p>
      {p.rationale && <p className="small" style={{ margin: 'var(--s-1) 0 var(--s-2)' }}>{p.rationale}</p>}
      <DecisionButtons id={p.id} label={`${p.action_id} on ${p.host_name}`} />
    </div>
  )
}
