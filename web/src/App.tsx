import { useEffect, useState, useSyncExternalStore } from 'react'
import { Bell, LayoutDashboard, LogOut, Menu, Moon, Server, SlidersHorizontal, Sparkles, Siren, Settings as Cog, Sun } from 'lucide-react'
import { logout, session, type User } from './lib/api'
import { LiveProvider, useLive } from './lib/live'
import { timeAgo } from './lib/format'
import { updateSettings, useSettings } from './lib/settings'
import { Badge, Drawer, Empty, ToastProvider } from './components/ui'
import { DashboardPage } from './pages/Dashboard'
import { IncidentsPage } from './pages/Incidents'
import { RulesPage } from './pages/Rules'
import { HostsPage } from './pages/Hosts'
import { AssistantPage } from './pages/Assistant'
import { SettingsPage } from './pages/Settings'
import { LoginPage } from './pages/Login'

const useHash = () => useSyncExternalStore(
  (cb) => { window.addEventListener('hashchange', cb); return () => window.removeEventListener('hashchange', cb) },
  () => location.hash || '#/dashboard',
)

const NAV = [
  { href: '#/dashboard', label: 'Dashboard', icon: LayoutDashboard },
  { href: '#/incidents', label: 'Incidents', icon: Siren },
  { href: '#/rules', label: 'Rules', icon: SlidersHorizontal },
  { href: '#/hosts', label: 'Hosts', icon: Server },
  { href: '#/assistant', label: 'AI assistant', icon: Sparkles },
  { href: '#/settings', label: 'Settings', icon: Cog },
]

function Bell_() {
  const { alerts, unread, markAllRead } = useLive()
  const [open, setOpen] = useState(false)
  return (
    <>
      <button className="btn icon" onClick={() => { setOpen(true); markAllRead() }} aria-label={unread ? `Alerts, ${unread} new` : 'Alerts'} style={{ position: 'relative' }}>
        <Bell size={18} />
        {unread > 0 && <span aria-hidden style={{ position: 'absolute', top: 6, right: 6, minWidth: 18, height: 18, borderRadius: 9, padding: '0 5px', background: 'var(--crit)', color: '#fff', fontSize: 11, fontWeight: 600, display: 'grid', placeItems: 'center' }}>{unread > 9 ? '9+' : unread}</span>}
      </button>
      {open && (
        <Drawer title="Alerts" onClose={() => setOpen(false)}>
          {alerts.length === 0 ? <Empty title="All quiet">Nothing needs your attention right now.</Empty> : (
            <ul className="list">
              {alerts.map((a) => (
                <li key={a.id}>
                  <a href={a.href} onClick={() => setOpen(false)} style={{ flex: 1, textDecoration: 'none' }}>
                    <div className="row" style={{ gap: 'var(--s-2)' }}>
                      <Badge tone={a.tone}>{a.tone === 'crit' ? 'Critical' : a.tone === 'warn' ? 'Needs you' : 'AI finding'}</Badge>
                      <span className="muted small">{timeAgo(a.at)}</span>
                    </div>
                    <strong style={{ display: 'block', margin: 'var(--s-1) 0' }}>{a.title}</strong>
                    <p className="small" style={{ color: 'var(--ink-2)', overflowWrap: 'anywhere' }}>{a.detail}</p>
                  </a>
                </li>
              ))}
            </ul>
          )}
        </Drawer>
      )}
    </>
  )
}

function Shell({ user, onLogout }: { user: User; onLogout: () => void }) {
  const hash = useHash()
  const settings = useSettings()
  const [menu, setMenu] = useState(false)
  const [, page, id] = hash.split('/')
  const dark = settings.theme === 'dark' || (settings.theme === 'system' && matchMedia('(prefers-color-scheme: dark)').matches)

  useEffect(() => { setMenu(false); window.scrollTo(0, 0) }, [hash])

  let body
  switch (page) {
    case 'incidents': body = <IncidentsPage id={id} />; break
    case 'rules': body = <RulesPage />; break
    case 'hosts': body = <HostsPage />; break
    case 'assistant': body = <AssistantPage />; break
    case 'settings': body = <SettingsPage />; break
    default: body = <DashboardPage />
  }

  return (
    <div className="shell">
      <a className="skip" href="#main">Skip to content</a>
      <nav className={`nav ${menu ? 'open' : ''}`} aria-label="Main">
        <div className="brand"><span className="brand-mark" aria-hidden>▲</span> Aero Agent</div>
        {NAV.map((n) => (
          <a key={n.href} className="item" href={n.href} aria-current={(hash.startsWith(n.href) || (n.href === '#/dashboard' && !NAV.some((x) => hash.startsWith(x.href)))) ? 'page' : undefined}>
            <n.icon size={18} aria-hidden /> {n.label}
          </a>
        ))}
        <div className="spacer" />
        <p className="small muted" style={{ padding: '0 var(--s-3)', overflowWrap: 'anywhere' }}>{user.email}<br />{user.role === 'admin' ? 'Admin' : 'Viewer, read-only'}</p>
        <button className="btn ghost" style={{ justifyContent: 'flex-start' }} onClick={onLogout}><LogOut size={18} aria-hidden /> Sign out</button>
      </nav>
      <div className="main">
        <header className="topbar">
          <button className="btn icon menu-btn" aria-label="Menu" aria-expanded={menu} onClick={() => setMenu((m) => !m)}><Menu size={18} /></button>
          <div className="grow" />
          <button className="btn icon" aria-label={dark ? 'Switch to light theme' : 'Switch to dark theme'} onClick={() => updateSettings({ theme: dark ? 'light' : 'dark' })}>{dark ? <Sun size={18} /> : <Moon size={18} />}</button>
          <Bell_ />
        </header>
        <main id="main" tabIndex={-1}>{body}</main>
      </div>
    </div>
  )
}

export function App() {
  const [user, setUser] = useState<User | null>(() => session.get()?.user ?? null)
  useEffect(() => { session.onExpire(() => setUser(null)) }, [])

  return (
    <ToastProvider>
      {user ? (
        <LiveProvider>
          <Shell user={user} onLogout={async () => { await logout().catch(() => {}); setUser(null) }} />
        </LiveProvider>
      ) : <LoginPage onLogin={setUser} />}
    </ToastProvider>
  )
}
