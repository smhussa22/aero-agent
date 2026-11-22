import { useState } from 'react'
import { Trash2, UserPlus } from 'lucide-react'
import { api, current, type Team } from '../lib/api'
import { timeAgo } from '../lib/format'
import { usePoll } from '../lib/poll'
import { Badge, ErrorLine, Modal, useToast } from '../components/ui'
import { isAdmin } from '../components/status'

export function TeamPage() {
  const toast = useToast()
  const admin = isAdmin()
  const me = current.get()
  const { data, error, reload } = usePoll((signal) => api<Team>('/api/team', { signal }), [], 30)
  const [email, setEmail] = useState('')
  const [role, setRole] = useState<'admin' | 'viewer'>('viewer')
  const [formError, setFormError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)

  const act = async (fn: () => Promise<unknown>, done: string) => {
    try { await fn(); toast({ tone: 'good', title: done }); reload() }
    catch (e) { toast({ tone: 'crit', title: 'That did not work', detail: e instanceof Error ? e.message : undefined }) }
  }

  const invite = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true); setFormError(null)
    try {
      const r = await api<{ emailed: boolean; sign_in_url: string }>('/api/invites', { method: 'POST', body: { email, role } })
      toast({
        tone: 'good', title: 'Invitation created',
        detail: r.emailed ? `We emailed ${email}.` : `No email was sent. Tell ${email} to sign in at ${r.sign_in_url} with that address.`,
      })
      setEmail(''); reload()
    } catch (err) { setFormError(err instanceof Error ? err.message : 'Could not invite.') }
    finally { setBusy(false) }
  }

  return (
    <div className="page">
      <div className="page-head"><div className="grow"><h1>Team</h1><p>Everyone here sees the same servers and incidents. Invite someone by email: they sign in with that address and join {me?.org.name ?? 'your organization'}.</p></div></div>
      <ErrorLine error={error} />

      {admin && (
        <form className="card row" style={{ alignItems: 'flex-end', marginBottom: 'var(--s-5)' }} onSubmit={invite}>
          <label className="field" style={{ flex: 2, minWidth: 220 }}><span>Email address</span>
            <input className="input" type="email" required value={email} maxLength={254} onChange={(e) => setEmail(e.target.value)} autoComplete="off" />
          </label>
          <label className="field" style={{ width: 160 }}><span>Role</span>
            <select className="input" value={role} onChange={(e) => setRole(e.target.value as 'admin' | 'viewer')}><option value="viewer">Viewer (read-only)</option><option value="admin">Admin</option></select>
          </label>
          <button className="btn primary" type="submit" disabled={busy}><UserPlus size={16} aria-hidden /> Invite</button>
          <div style={{ flexBasis: '100%' }}><ErrorLine error={formError} /></div>
        </form>
      )}

      <div className="card" style={{ padding: 0, overflowX: 'auto' }}>
        <table className="table">
          <thead><tr><th>Member</th><th>Role</th><th>Joined</th>{admin && <th><span className="sr">Actions</span></th>}</tr></thead>
          <tbody>
            {(data?.members ?? []).map((m) => {
              const self = m.id === me?.id
              return (
                <tr key={m.id}>
                  <td>{m.email}{self && <> <Badge>You</Badge></>}</td>
                  <td>
                    {admin && !self ? (
                      <select className="input" style={{ minHeight: 36, width: 140 }} aria-label={`Role for ${m.email}`} value={m.role}
                        onChange={(e) => act(() => api(`/api/members/${m.id}`, { method: 'PATCH', body: { role: e.target.value } }), 'Role changed')}>
                        <option value="viewer">Viewer</option><option value="admin">Admin</option>
                      </select>
                    ) : m.role === 'admin' ? 'Admin' : 'Viewer'}
                  </td>
                  <td className="muted">{timeAgo(m.created_at)}</td>
                  {admin && <td>{!self && <button className="btn icon sm ghost" aria-label={`Remove ${m.email}`}
                    onClick={() => confirm(`Remove ${m.email}? They are signed out right away.`) && act(() => api(`/api/members/${m.id}`, { method: 'DELETE' }), 'Member removed')}><Trash2 size={16} /></button>}</td>}
                </tr>
              )
            })}
          </tbody>
        </table>
      </div>

      {(data?.invites.length ?? 0) > 0 && (
        <>
          <h2 style={{ margin: 'var(--s-6) 0 var(--s-3)' }}>Waiting to join</h2>
          <div className="card" style={{ padding: 0, overflowX: 'auto' }}>
            <table className="table">
              <tbody>
                {data!.invites.map((i) => (
                  <tr key={i.id}>
                    <td>{i.email}</td><td>{i.role === 'admin' ? 'Admin' : 'Viewer'}</td>
                    <td className="muted">expires {new Date(i.expires_at).toLocaleDateString()}</td>
                    {admin && <td><button className="btn sm" onClick={() => act(() => api(`/api/invites/${i.id}`, { method: 'DELETE' }), 'Invitation revoked')}>Revoke</button></td>}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}

      {admin && (
        <>
          <h2 style={{ margin: 'var(--s-6) 0 var(--s-3)' }}>Delete this organization</h2>
          <div className="card stack">
            <p className="small" style={{ color: 'var(--ink-2)' }}>Erases every server, metric, rule, incident, audit entry and member of {me?.org.name}. Agents stop working. This cannot be undone.</p>
            <div><button className="btn danger" onClick={() => setConfirmDelete(true)}>Delete organization…</button></div>
          </div>
        </>
      )}
      {confirmDelete && <DeleteDialog name={me?.org.name ?? ''} onClose={() => setConfirmDelete(false)} />}
    </div>
  )
}

function DeleteDialog({ name, onClose }: { name: string; onClose: () => void }) {
  const [typed, setTyped] = useState('')
  const [error, setError] = useState<string | null>(null)
  const go = async (e: React.FormEvent) => {
    e.preventDefault()
    try { await api('/api/org/delete', { method: 'POST', body: { confirm: typed } }); current.set(null); location.href = '/' }
    catch (err) { setError(err instanceof Error ? err.message : 'Could not delete.') }
  }
  return (
    <Modal title="Delete organization?" onClose={onClose}>
      <form className="stack" onSubmit={go}>
        <p>Type <strong>{name}</strong> to confirm.</p>
        <input className="input" aria-label="Organization name" value={typed} onChange={(e) => setTyped(e.target.value)} autoComplete="off" />
        <ErrorLine error={error} />
        <div className="row"><button className="btn danger" type="submit" disabled={typed !== name}>Delete everything</button><button type="button" className="btn" onClick={onClose}>Cancel</button></div>
      </form>
    </Modal>
  )
}
