import { useState } from 'react'
import { aiKey, updateSettings, useSettings } from '../lib/settings'
import { AiSetup } from '../components/ChatPanel'
import { useToast } from '../components/ui'

export function SettingsPage() {
  const s = useSettings()
  const toast = useToast()
  const [permission, setPermission] = useState<NotificationPermission | 'unsupported'>('Notification' in window ? Notification.permission : 'unsupported')

  const toggleNotify = async (on: boolean) => {
    if (on && 'Notification' in window && Notification.permission !== 'granted') {
      const p = await Notification.requestPermission()
      setPermission(p)
      if (p !== 'granted') { toast({ tone: 'warn', title: 'Notifications are blocked', detail: 'Allow them for this site in your browser settings, then try again.' }); return }
    }
    updateSettings({ notify: on })
  }

  return (
    <div className="page">
      <div className="page-head"><div className="grow"><h1>Settings</h1><p>Saved in this browser only.</p></div></div>
      <div className="stack" style={{ maxWidth: 720 }}>
        <section className="card stack" aria-labelledby="s-look">
          <h2 id="s-look">Appearance and refresh</h2>
          <div className="field"><span>Theme</span>
            <div className="seg" role="group" aria-label="Theme">
              {(['system', 'light', 'dark'] as const).map((t) => <button key={t} aria-pressed={s.theme === t} onClick={() => updateSettings({ theme: t })}>{t[0].toUpperCase() + t.slice(1)}</button>)}
            </div>
          </div>
          <label className="field"><span>Refresh data every</span>
            <select className="input" value={s.refreshSeconds} onChange={(e) => updateSettings({ refreshSeconds: Number(e.target.value) })}>
              <option value={5}>5 seconds</option><option value={10}>10 seconds</option><option value={30}>30 seconds</option><option value={60}>1 minute</option>
            </select>
          </label>
        </section>

        <section className="card stack" aria-labelledby="s-alerts">
          <h2 id="s-alerts">Alerts</h2>
          <p className="small" style={{ color: 'var(--ink-2)' }}>New alerts always appear in the bell and as a pop-up while this tab is open. Turn this on to also get a desktop notification when the tab is in the background.</p>
          <label className="toggle"><input type="checkbox" checked={s.notify && permission === 'granted'} onChange={(e) => toggleNotify(e.target.checked)} disabled={permission === 'unsupported'} /> Desktop notifications</label>
          {permission === 'denied' && <p className="hint">Your browser is blocking notifications for this site.</p>}
          {permission === 'unsupported' && <p className="hint">This browser does not support notifications.</p>}
        </section>

        <section className="card stack" aria-labelledby="s-ai">
          <h2 id="s-ai">Your AI</h2>
          <p className="small" style={{ color: 'var(--ink-2)' }}>Powers the assistant chat. Aero never stores or sees this key. It is only sent from your browser to the provider you choose.</p>
          <AiSetup />
        </section>

        <section className="card stack" aria-labelledby="s-data">
          <h2 id="s-data">This browser’s data</h2>
          <p className="small" style={{ color: 'var(--ink-2)' }}>Layouts and settings live here. Clear them if you share this computer.</p>
          <div className="row">
            <button className="btn" onClick={() => { aiKey.clear(); toast({ tone: 'info', title: 'AI key forgotten' }) }}>Forget AI key</button>
            <button className="btn danger" onClick={() => { if (confirm('Clear all saved layouts and settings in this browser?')) { localStorage.clear(); sessionStorage.clear(); location.href = '/' } }}>Clear everything</button>
          </div>
        </section>
      </div>
    </div>
  )
}
