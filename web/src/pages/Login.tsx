import { useEffect, useState } from 'react'
import { authConfig, resetPassword, signIn, signUp } from '../lib/supabase'

type Mode = 'signin' | 'signup' | 'reset'

export function LoginPage() {
  const [configured, setConfigured] = useState<boolean | null>(null)
  const [mode, setMode] = useState<Mode>('signin')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  useEffect(() => { authConfig().then((c) => setConfigured(!!c.supabase_url)).catch(() => setConfigured(false)) }, [])

  async function submit(e: React.FormEvent) {
    e.preventDefault()
    setBusy(true); setError(''); setNotice('')
    try {
      if (mode === 'reset') {
        await resetPassword(email)
        setNotice('If that address has an account, a reset link is on its way.')
      } else if (mode === 'signup') {
        if (await signUp(email, password) === 'signed-in') { location.reload(); return }
        setNotice('Check your email and click the confirmation link, then come back and sign in.')
        setMode('signin')
      } else {
        await signIn(email, password)
        location.reload()
        return
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Something went wrong, please try again.')
    }
    setBusy(false)
  }

  const title = mode === 'signup' ? 'Create your account' : mode === 'reset' ? 'Reset your password' : 'Sign in'
  return (
    <main className="login">
      <div className="login-grid">
      <section className="login-pitch" aria-label="About Aero Agent">
        <div className="brand" style={{ padding: 0 }}><span className="brand-mark" aria-hidden>▲</span> Aero Agent</div>
        <h2>Watch your servers. Let your own AI help fix them.</h2>
        <ul>
          <li><strong>One command to install.</strong> Paste it on a Linux server and it shows up on your dashboard.</li>
          <li><strong>Your AI key, your cost.</strong> The AI runs on your own account, so there is nothing to buy from us.</li>
          <li><strong>You stay in control.</strong> Fixes come from a short allowed list and wait for your approval. Every step is logged.</li>
        </ul>
      </section>
      <form className="card stack" onSubmit={submit}>
        <h1>{title}</h1>

        {configured === false && (
          <p role="alert" className="hint">Sign-in is not set up on this server. Sign in with your identity provider instead.{' '}
            <a href="/auth/login">Continue</a></p>
        )}

        {configured && (
          <>
            <label className="field"><span>Email address</span>
              <input className="input" type="email" required autoComplete="email" value={email} maxLength={254} onChange={(e) => setEmail(e.target.value)} />
            </label>
            {mode !== 'reset' && (
              <label className="field"><span>Password</span>
                <input className="input" type="password" required minLength={8} maxLength={200} value={password}
                  autoComplete={mode === 'signup' ? 'new-password' : 'current-password'} onChange={(e) => setPassword(e.target.value)} />
              </label>
            )}
            {error && <p role="alert" style={{ color: 'var(--crit)' }}>{error}</p>}
            {notice && <p role="status" className="muted">{notice}</p>}
            <button className="btn primary" type="submit" disabled={busy}>
              {busy ? 'One moment…' : mode === 'signup' ? 'Create account' : mode === 'reset' ? 'Send reset link' : 'Sign in'}
            </button>
            <p className="hint">
              {mode === 'signin' && <><button type="button" className="btn sm ghost" onClick={() => { setMode('signup'); setError(''); setNotice('') }}>Create an account</button>
                <button type="button" className="btn sm ghost" onClick={() => { setMode('reset'); setError(''); setNotice('') }}>Forgot password?</button></>}
              {mode !== 'signin' && <button type="button" className="btn sm ghost" onClick={() => { setMode('signin'); setError(''); setNotice('') }}>Back to sign in</button>}
            </p>
          </>
        )}
      </form>
      </div>
    </main>
  )
}
