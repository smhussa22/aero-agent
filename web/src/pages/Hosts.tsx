import { useEffect, useRef, useState } from 'react'
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
    try { await api(`/api/hosts/${encodeURIComponent(h.id)}`, { method: 'PATCH', body: { mode } }); toast({ tone: 'good', title: `${h.name} is now ${mode}` }); reload() }
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
                    <strong style={{ overflowWrap: 'anywhere' }}>{h.name}</strong>
                    <p className="muted small">{h.last_seen ? `Reported ${timeAgo(h.last_seen)}` : 'Has not reported yet'}</p>
                  </div>
                  {silent ? <Badge tone="warn">Silent</Badge> : h.last_seen ? <Badge tone="good">Reporting</Badge> : <Badge>Waiting</Badge>}
                </div>
                <fieldset style={{ border: 0, padding: 0, margin: 'var(--s-4) 0 0' }} disabled={!admin}>
                  <legend className="label" style={{ padding: 0, marginBottom: 'var(--s-2)' }}>Permission mode{admin ? '' : ' (admins can change this)'}</legend>
                  <div className="seg" role="group" aria-label={`Permission mode for ${h.name}`}>
                    {MODES.map((m) => <button key={m.value} aria-pressed={h.mode === m.value} onClick={() => setMode(h, m.value)}>{m.label}</button>)}
                  </div>
                  <p className="hint" style={{ marginTop: 'var(--s-2)' }}>{MODES.find((m) => m.value === h.mode)?.hint}</p>
                </fieldset>
              </div>
            )
          })}
        </div>
      )}

      <AddServer hosts={hosts} admin={admin} />
    </div>
  )
}

type Enrollment = { code: string; expires_at: string; command: string }

// One click makes a single-use install command; the page then watches for the new server to report in.
export function AddServer({ hosts, admin }: { hosts: Host[]; admin: boolean }) {
  const toast = useToast()
  const [name, setName] = useState('')
  const [ai, setAi] = useState(false)
  const [enrollment, setEnrollment] = useState<Enrollment | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const known = useRef<Set<string>>(new Set())
  const joined = enrollment ? hosts.find((h) => !known.current.has(h.id) && h.last_seen) : undefined

  useEffect(() => { if (joined) toast({ tone: 'good', title: `${joined.name} is connected` }) }, [joined?.id]) // eslint-disable-line react-hooks/exhaustive-deps

  const create = async () => {
    setBusy(true); setError(null)
    try {
      known.current = new Set(hosts.map((h) => h.id))
      setEnrollment(await api<Enrollment>('/api/enrollments', { method: 'POST' }))
    } catch (e) { setError(e instanceof Error ? e.message : 'Could not create the command.') }
    setBusy(false)
  }

  const validName = /^[A-Za-z0-9._-]{1,100}$/.test(name)
  const command = enrollment ? enrollment.command + (validName ? ` --name ${name}` : '') + (ai ? ' --ai' : '') : ''
  const copy = async () => {
    try { await navigator.clipboard.writeText(command); toast({ tone: 'good', title: 'Copied' }) }
    catch { toast({ tone: 'warn', title: 'Could not copy, select the text instead' }) }
  }

  return (
    <>
      <h2 style={{ margin: 'var(--s-6) 0 var(--s-3)' }}>Add a server</h2>
      <div className="card stack">
        {!admin ? <p className="small" style={{ color: 'var(--ink-2)' }}>Ask an admin of your team to add a server.</p> : (
          <>
            <p className="small" style={{ color: 'var(--ink-2)' }}>Run one command as root on a Linux server (x86-64 or ARM, with systemd). It installs the monitoring agent and connects it to your account. The command works once and expires in 15 minutes.</p>
            <label className="field" style={{ maxWidth: 360 }}><span>Server name (optional, defaults to its hostname)</span>
              <input className="input" value={name} maxLength={100} placeholder="web-1" onChange={(e) => setName(e.target.value)} aria-invalid={name !== '' && !validName} />
            </label>
            <label className="toggle" style={{ minHeight: 36 }}>
              <input type="checkbox" checked={ai} onChange={(e) => setAi(e.target.checked)} />
              <span className="small">Also install the AI agent (it asks for your AI provider and key on the server, and starts in observe-only mode)</span>
            </label>
            {name !== '' && !validName && <p className="hint" role="alert">Use letters, digits, dots, dashes and underscores only.</p>}
            <div><button className="btn primary" onClick={create} disabled={busy}>{enrollment ? 'Make a new command' : 'Make an install command'}</button></div>
            <ErrorLine error={error} />
            {enrollment && (
              <>
                <pre className="mono card" style={{ margin: 0, overflow: 'auto', background: 'var(--surface-2)', whiteSpace: 'pre-wrap', overflowWrap: 'anywhere' }}>{command}</pre>
                <div className="row"><button className="btn" onClick={copy}>Copy command</button>
                  <span className="muted small" role="status">{joined ? `${joined.name} is connected.` : 'Waiting for the server to report in…'}</span></div>
                <p className="hint">{ai ? 'The AI key stays on that server and never reaches this dashboard.' : 'Tick “Also install the AI agent” above to have your own AI investigate alerts on that server.'}</p>
              </>
            )}
          </>
        )}
      </div>
    </>
  )
}
