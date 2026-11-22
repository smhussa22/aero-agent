import { useState } from 'react'
import { api, type Host } from '../lib/api'
import { SILENT_AFTER_MS } from '../lib/alerts'
import { timeAgo } from '../lib/format'
import { useLive } from '../lib/live'
import { Badge, Empty, ErrorLine, useToast } from '../components/ui'
import { isAdmin } from '../components/status'

const MODES: { value: Host['mode']; label: string; hint: string }[] = [
  { value: 'observe', label: 'Observe', hint: 'The AI agent investigates and explains, but never changes anything.' },
  { value: 'recommend', label: 'Recommend', hint: 'It proposes fixes and waits for you to approve each one.' },
  { value: 'autonomous', label: 'Autonomous', hint: 'It runs safe, allowlisted fixes by itself. Dangerous ones still need you.' },
]

export function HostsPage() {
  const { hosts, loading, error, reload } = useLive()
  const toast = useToast()
  const admin = isAdmin()
  const [saveError, setSaveError] = useState<string | null>(null)

  const setMode = async (h: Host, mode: Host['mode']) => {
    setSaveError(null)
    try { await api(`/api/hosts/${encodeURIComponent(h.id)}`, { method: 'PATCH', body: { mode } }); toast({ tone: 'good', title: `${h.id} is now ${mode}` }); reload() }
    catch (e) { setSaveError(e instanceof Error ? e.message : 'Could not change the mode.') }
  }

  return (
    <div className="page">
      <div className="page-head"><div className="grow"><h1>Hosts</h1><p>Each server’s permission mode decides how much its AI agent may do. The agent’s own local setting can lower it further, never raise it.</p></div></div>
      <ErrorLine error={error ?? saveError} />

      {hosts.length === 0 ? (
        <div className="card"><Empty title="No hosts yet">{loading ? 'Loading…' : 'Add your first server below.'}</Empty></div>
      ) : (
        <div className="stack">
          {hosts.map((h) => {
            const silent = h.last_seen ? Date.now() - new Date(h.last_seen).getTime() > SILENT_AFTER_MS : false
            return (
              <div className="card" key={h.id}>
                <div className="row" style={{ justifyContent: 'space-between' }}>
                  <div>
                    <strong style={{ overflowWrap: 'anywhere' }}>{h.id}</strong>
                    <p className="muted small">{h.last_seen ? `Reported ${timeAgo(h.last_seen)}` : 'Has not reported yet'}</p>
                  </div>
                  {silent ? <Badge tone="warn">Silent</Badge> : h.last_seen ? <Badge tone="good">Reporting</Badge> : <Badge>Waiting</Badge>}
                </div>
                <fieldset style={{ border: 0, padding: 0, margin: 'var(--s-4) 0 0' }} disabled={!admin}>
                  <legend className="label" style={{ padding: 0, marginBottom: 'var(--s-2)' }}>Permission mode{admin ? '' : ' (admins can change this)'}</legend>
                  <div className="seg" role="group" aria-label={`Permission mode for ${h.id}`}>
                    {MODES.map((m) => <button key={m.value} aria-pressed={h.mode === m.value} onClick={() => setMode(h, m.value)}>{m.label}</button>)}
                  </div>
                  <p className="hint" style={{ marginTop: 'var(--s-2)' }}>{MODES.find((m) => m.value === h.mode)?.hint}</p>
                </fieldset>
              </div>
            )
          })}
        </div>
      )}

      <h2 style={{ margin: 'var(--s-6) 0 var(--s-3)' }}>Add a server</h2>
      <div className="card stack">
        <p className="small" style={{ color: 'var(--ink-2)' }}>Create a token for the server, then start the monitoring agent with it. The token is shown once.</p>
        <pre className="mono card" style={{ margin: 0, overflow: 'auto', background: 'var(--surface-2)' }}>{`hostctl add web-1
AERO_TOKEN=<token> aero-agent <gateway-host>:50051`}</pre>
        <p className="hint">Want the AI agent to investigate alerts on that server? Run aero-remediation there too, with the same token.</p>
      </div>
    </div>
  )
}
