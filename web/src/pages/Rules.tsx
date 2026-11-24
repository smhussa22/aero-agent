import { useState } from 'react'
import { Pencil, Plus, Trash2 } from 'lucide-react'
import { api, ApiError, type Rule } from '../lib/api'
import { metricName } from '../lib/format'
import { useLive } from '../lib/live'
import { usePoll } from '../lib/poll'
import { Badge, Empty, ErrorLine, Modal, useToast } from '../components/ui'
import { isAdmin } from '../components/status'

const COMMON = ['cpu.usage_percent', 'memory.used_percent', 'disk.used_percent', 'cpu.load_1m', 'cpu.load_5m', 'net.rx_errors', 'net.tx_errors']
const blank: Omit<Rule, 'id'> = { host_id: null, name: '', metric: 'cpu.usage_percent', label: '', operator: '>', threshold: 90, duration_seconds: 60, severity: 'warning', enabled: true }

export function RulesPage() {
  const toast = useToast()
  const admin = isAdmin()
  const { hosts } = useLive()
  const { data, error, loading, reload } = usePoll((signal) => api<Rule[]>('/api/rules', { signal }), [], 30)
  const [editing, setEditing] = useState<Rule | (Omit<Rule, 'id'> & { id?: undefined }) | null>(null)

  const remove = async (r: Rule) => {
    if (!confirm(`Delete the rule “${r.name}”?`)) return
    try { await api(`/api/rules/${r.id}`, { method: 'DELETE' }); toast({ tone: 'info', title: 'Rule deleted' }); reload() }
    catch (e) { toast({ tone: 'crit', title: 'Could not delete', detail: e instanceof Error ? e.message : undefined }) }
  }

  return (
    <div className="page">
      <div className="page-head">
        <div className="grow"><h1>Alert rules</h1><p>When a metric crosses a threshold for long enough, an incident opens and the AI agent investigates. Rule changes apply within a minute.</p></div>
        {admin && <button className="btn primary" onClick={() => setEditing({ ...blank })}><Plus size={16} aria-hidden /> New rule</button>}
      </div>
      <ErrorLine error={error} />
      {!data || data.length === 0 ? (
        <div className="card"><Empty title="No rules yet">{loading ? 'Loading…' : admin ? 'Create a rule, for example “CPU above 90% for 60 seconds”.' : 'An admin can create rules.'}</Empty></div>
      ) : (
        <div className="card" style={{ padding: 0, overflowX: 'auto' }}>
          <table className="table">
            <thead><tr><th>Name</th><th>Condition</th><th>Applies to</th><th>Severity</th>{admin && <th><span className="sr">Actions</span></th>}</tr></thead>
            <tbody>
              {data.map((r) => (
                <tr key={r.id}>
                  <td><strong>{r.name}</strong>{!r.enabled && <> <Badge>Off</Badge></>}</td>
                  <td>{metricName(r.metric, r.label)} {r.operator} {r.threshold}{r.duration_seconds ? ` for ${r.duration_seconds}s` : ''}</td>
                  <td>{r.host_id ? hosts.find((h) => h.id === r.host_id)?.name ?? 'A removed host' : 'All hosts'}</td>
                  <td><Badge tone={r.severity === 'critical' ? 'crit' : r.severity === 'warning' ? 'warn' : 'info'}>{r.severity[0].toUpperCase() + r.severity.slice(1)}</Badge></td>
                  {admin && <td style={{ whiteSpace: 'nowrap' }}>
                    <button className="btn sm ghost" aria-label={`Edit ${r.name}`} onClick={() => setEditing(r)}><Pencil size={16} aria-hidden /> Edit</button>
                    <button className="btn sm ghost" aria-label={`Delete ${r.name}`} onClick={() => remove(r)}><Trash2 size={16} aria-hidden /> Delete</button>
                  </td>}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {editing && <RuleDialog rule={editing} onClose={() => setEditing(null)} onSaved={() => { setEditing(null); reload() }} />}
    </div>
  )
}

function RuleDialog({ rule, onClose, onSaved }: { rule: Rule | (Omit<Rule, 'id'> & { id?: undefined }); onClose: () => void; onSaved: () => void }) {
  const { hosts } = useLive()
  const toast = useToast()
  const [r, setR] = useState(rule)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const set = <K extends keyof Rule>(k: K, v: Rule[K]) => setR((cur) => ({ ...cur, [k]: v }))

  const save = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true); setError(null)
    const { id, ...body } = r as Rule
    try {
      await api(id ? `/api/rules/${id}` : '/api/rules', { method: id ? 'PUT' : 'POST', body })
      toast({ tone: 'good', title: id ? 'Rule saved' : 'Rule created' })
      onSaved()
    } catch (err) { setError(err instanceof ApiError || err instanceof Error ? err.message : 'Could not save.') }
    finally { setBusy(false) }
  }

  return (
    <Modal title={r.id ? 'Edit rule' : 'New rule'} onClose={onClose}>
      <form className="stack" onSubmit={save}>
        <label className="field"><span>Name</span><input className="input" required maxLength={200} value={r.name} onChange={(e) => set('name', e.target.value)} placeholder="CPU high" /></label>
        <label className="field"><span>Applies to</span>
          <select className="input" value={r.host_id ?? ''} onChange={(e) => set('host_id', e.target.value || null)}>
            <option value="">All hosts</option>{hosts.map((h) => <option key={h.id} value={h.id}>{h.name}</option>)}
          </select>
        </label>
        <label className="field"><span>Metric</span>
          <input className="input" required list="metrics" maxLength={100} value={r.metric} onChange={(e) => set('metric', e.target.value)} spellCheck={false} />
          <datalist id="metrics">{COMMON.map((m) => <option key={m} value={m} />)}</datalist>
        </label>
        <label className="field"><span>Label (optional)</span><input className="input" maxLength={200} value={r.label} onChange={(e) => set('label', e.target.value)} placeholder="a mount point like /" />
          <span className="hint">Leave empty to match every disk, interface or process.</span></label>
        <div className="row" style={{ alignItems: 'flex-end' }}>
          <label className="field" style={{ width: 96 }}><span>Operator</span>
            <select className="input" value={r.operator} onChange={(e) => set('operator', e.target.value as Rule['operator'])}>{['>', '>=', '<', '<='].map((o) => <option key={o}>{o}</option>)}</select>
          </label>
          <label className="field" style={{ flex: 1, minWidth: 120 }}><span>Threshold</span><input className="input" type="number" step="any" required value={r.threshold} onChange={(e) => set('threshold', Number(e.target.value))} /></label>
          <label className="field" style={{ flex: 1, minWidth: 120 }}><span>For (seconds)</span><input className="input" type="number" min={0} max={86400} required value={r.duration_seconds} onChange={(e) => set('duration_seconds', Number(e.target.value))} /></label>
        </div>
        {r.duration_seconds === 0 && <p className="hint" role="note">With 0 seconds, a single spike opens an incident. Use 60 or more to ignore short blips.</p>}
        {r.metric.endsWith('_percent') && (r.operator === '>' || r.operator === '>=') && r.threshold <= 1 && <p className="hint" role="alert">This will fire almost all the time, because normal usage is above {r.threshold}%.</p>}
        <label className="field"><span>Severity</span>
          <select className="input" value={r.severity} onChange={(e) => set('severity', e.target.value as Rule['severity'])}><option value="info">Info</option><option value="warning">Warning</option><option value="critical">Critical</option></select>
        </label>
        <label className="toggle"><input type="checkbox" checked={r.enabled} onChange={(e) => set('enabled', e.target.checked)} /> Enabled</label>
        <ErrorLine error={error} />
        <div className="row"><button className="btn primary" type="submit" disabled={busy}>Save rule</button><button type="button" className="btn" onClick={onClose}>Cancel</button></div>
      </form>
    </Modal>
  )
}
