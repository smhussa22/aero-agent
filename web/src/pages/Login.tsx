import { useState } from 'react'
import { login, type User } from '../lib/api'
import { ErrorLine } from '../components/ui'

export function LoginPage({ onLogin }: { onLogin: (u: User) => void }) {
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const submit = async (e: React.FormEvent) => {
    e.preventDefault(); setBusy(true); setError(null)
    try { onLogin(await login(email, password)) }
    catch (err) { setError(err instanceof Error ? err.message : 'Could not sign in.') }
    finally { setBusy(false) }
  }

  return (
    <main className="login">
      <form className="card stack" onSubmit={submit}>
        <div className="brand" style={{ padding: 0 }}><span className="brand-mark" aria-hidden>▲</span> Aero Agent</div>
        <div><h1>Sign in</h1><p className="muted" style={{ marginTop: 'var(--s-1)' }}>See your servers, and what the AI agent found.</p></div>
        <label className="field"><span>Email</span><input className="input" type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} /></label>
        <label className="field"><span>Password</span><input className="input" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} /></label>
        <ErrorLine error={error} />
        <button className="btn primary" type="submit" disabled={busy}>{busy ? 'Signing in…' : 'Sign in'}</button>
      </form>
    </main>
  )
}
