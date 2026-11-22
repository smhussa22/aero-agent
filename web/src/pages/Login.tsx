export function LoginPage() {
  return (
    <main className="login">
      <div className="card stack">
        <div className="brand" style={{ padding: 0 }}><span className="brand-mark" aria-hidden>▲</span> Aero Agent</div>
        <div>
          <h1>Sign in</h1>
          <p className="muted" style={{ marginTop: 'var(--s-1)' }}>See your servers, and what the AI agent found.</p>
        </div>
        <a className="btn primary" href="/auth/login">Continue to sign in</a>
        <p className="hint">You sign in with our identity provider. We never see or store your password.</p>
      </div>
    </main>
  )
}
