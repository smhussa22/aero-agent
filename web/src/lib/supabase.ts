// Sign-up and sign-in against Supabase Auth, straight from the browser, so the form stays on our own page.
// Supabase checks the password and emails the confirmation; we only pass its signed token to our server.

export type AuthConfig = { supabase_url: string; supabase_anon_key: string }

let cached: AuthConfig | null = null
export async function authConfig(): Promise<AuthConfig> {
  if (cached) return cached
  const res = await fetch('/api/config')
  cached = await res.json()
  return cached!
}

async function call(cfg: AuthConfig, path: string, body: unknown) {
  const res = await fetch(`${cfg.supabase_url}/auth/v1${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', apikey: cfg.supabase_anon_key },
    body: JSON.stringify(body),
  })
  const data = await res.json().catch(() => ({}))
  if (!res.ok) throw new Error(data.msg || data.error_description || data.message || 'Something went wrong, please try again.')
  return data as Record<string, unknown>
}

// Hands Supabase's token to our server, which verifies it and sets the session cookie.
async function startSession(accessToken: string) {
  const res = await fetch('/auth/session', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ access_token: accessToken }) })
  if (!res.ok) throw new Error(((await res.json().catch(() => ({}))) as { error?: string }).error ?? 'Sign-in failed, please try again.')
}

export async function signIn(email: string, password: string) {
  const cfg = await authConfig()
  const out = await call(cfg, '/token?grant_type=password', { email, password })
  await startSession(out.access_token as string)
}

// Returns 'confirm' when Supabase wants the email address confirmed first (the normal case).
export async function signUp(email: string, password: string): Promise<'confirm' | 'signed-in'> {
  const cfg = await authConfig()
  const out = await call(cfg, '/signup', { email, password })
  if (typeof out.access_token === 'string') { await startSession(out.access_token); return 'signed-in' }
  return 'confirm'
}

export async function resetPassword(email: string) {
  await call(await authConfig(), '/recover', { email })
}
