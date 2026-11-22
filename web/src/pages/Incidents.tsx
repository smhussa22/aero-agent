import { useState } from 'react'
import { ArrowLeft, Sparkles } from 'lucide-react'
import { api, q, type IncidentDetail } from '../lib/api'
import { timeAgo } from '../lib/format'
import { chat } from '../lib/chatStore'
import { useLive } from '../lib/live'
import { usePoll } from '../lib/poll'
import { Badge, Empty, ErrorLine, type Tone } from '../components/ui'
import { DecisionButtons, IncidentBadge, paramsText } from '../components/status'

const ACTION_TONE: Record<string, { tone: Tone; label: string }> = {
  proposed: { tone: 'neutral', label: 'Proposed' }, awaiting_approval: { tone: 'warn', label: 'Needs approval' },
  approved: { tone: 'info', label: 'Approved' }, rejected: { tone: 'neutral', label: 'Rejected' },
  running: { tone: 'info', label: 'Running' }, succeeded: { tone: 'good', label: 'Ran' },
  failed: { tone: 'crit', label: 'Blocked or failed' }, verified: { tone: 'good', label: 'Fixed and verified' },
  escalated: { tone: 'crit', label: 'Escalated' },
}

export function IncidentsPage({ id }: { id?: string }) {
  return id ? <Detail id={id} /> : <List />
}

function List() {
  const { incidents, loading, error } = useLive()
  const [status, setStatus] = useState('')
  const shown = incidents.filter((i) => !status || i.status === status)
  return (
    <div className="page">
      <div className="page-head">
        <div className="grow"><h1>Incidents</h1><p>Each alert becomes an incident. The AI agent investigates and explains it here.</p></div>
        <label className="field" style={{ minWidth: 180 }}>
          <span>Show</span>
          <select className="input" value={status} onChange={(e) => setStatus(e.target.value)}>
            <option value="">All</option>
            {['open', 'investigating', 'remediating', 'escalated', 'resolved'].map((s) => <option key={s} value={s}>{s[0].toUpperCase() + s.slice(1)}</option>)}
          </select>
        </label>
      </div>
      <ErrorLine error={error} />
      {shown.length === 0
        ? <div className="card"><Empty title="No incidents">{loading ? 'Loading…' : 'Nothing matches. That is usually good news.'}</Empty></div>
        : <div className="stack">{shown.map((i) => (
          <a key={i.id} className="card" href={`#/incidents/${i.id}`}>
            <div className="row" style={{ gap: 'var(--s-2)', marginBottom: 'var(--s-2)' }}>
              <IncidentBadge status={i.status} /><strong>{i.host_id}</strong><span className="muted small">{timeAgo(i.opened_at)}</span>
            </div>
            <p style={{ color: 'var(--ink-2)', overflowWrap: 'anywhere' }}>{i.summary || 'Waiting for the AI agent to investigate.'}</p>
          </a>
        ))}</div>}
    </div>
  )
}

function Detail({ id }: { id: string }) {
  const { reload } = useLive()
  const { data, error, loading, reload: reloadDetail } = usePoll((signal) => api<IncidentDetail>(`/api/incidents/${q(id)}`, { signal }), [id], 5)

  const askAi = () => {
    if (!data) return
    chat.setDraft(`Explain this incident on ${data.host_id} in plain English and tell me what I should do. Status: ${data.status}. Summary: ${data.summary || 'none yet'}`)
    location.hash = '#/assistant'
  }

  return (
    <div className="page">
      <a href="#/incidents" className="btn sm ghost" style={{ marginBottom: 'var(--s-3)' }}><ArrowLeft size={16} aria-hidden /> All incidents</a>
      <ErrorLine error={error} />
      {!data ? (loading ? <p className="muted">Loading…</p> : null) : (
        <>
          <div className="page-head">
            <div className="grow">
              <div className="row" style={{ gap: 'var(--s-2)', marginBottom: 'var(--s-2)' }}><IncidentBadge status={data.status} /><span className="muted">{data.host_id} · opened {timeAgo(data.opened_at)}</span></div>
              <h1>{data.summary ? 'What the AI agent found' : 'Waiting for the AI agent'}</h1>
            </div>
            <button className="btn" onClick={askAi}><Sparkles size={16} aria-hidden /> Ask my AI about this</button>
          </div>

          <div className="card" style={{ marginBottom: 'var(--s-5)' }}>
            <p style={{ maxWidth: '62ch', overflowWrap: 'anywhere' }}>{data.summary || 'No remediation agent has reported on this incident yet. If one is connected for this host, it will investigate shortly.'}</p>
          </div>

          <h2 style={{ marginBottom: 'var(--s-3)' }}>Proposed fixes</h2>
          {data.actions.length === 0
            ? <div className="card"><Empty title="No fixes proposed">The agent has not suggested an action for this incident.</Empty></div>
            : <div className="stack">{data.actions.map((a) => {
              const s = ACTION_TONE[a.status] ?? { tone: 'neutral' as Tone, label: a.status }
              return (
                <div className="card" key={a.id}>
                  <div className="row" style={{ gap: 'var(--s-2)', marginBottom: 'var(--s-2)' }}>
                    <strong>{a.action_id.replace(/_/g, ' ')}</strong>
                    <Badge tone={s.tone}>{s.label}</Badge>
                    {a.dangerous && <Badge tone="crit">Dangerous</Badge>}
                    {a.dry_run && <Badge>Dry run</Badge>}
                  </div>
                  <p className="mono muted" style={{ overflowWrap: 'anywhere' }}>{paramsText(a.params)}</p>
                  {a.rationale && <p className="small" style={{ margin: 'var(--s-2) 0' }}>{a.rationale}</p>}
                  {a.status === 'awaiting_approval' && <DecisionButtons id={a.id} label={`${a.action_id} on ${data.host_id}`} onDone={() => { reload(); reloadDetail() }} />}
                </div>
              )
            })}</div>}
        </>
      )}
    </div>
  )
}
