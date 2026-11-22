import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from 'react'
import { AlertTriangle, CheckCircle2, Info, OctagonAlert, X } from 'lucide-react'

export type Tone = 'good' | 'warn' | 'crit' | 'info' | 'neutral'

// Status is colour + icon + word, so it never relies on colour alone.
export function Badge({ tone = 'neutral', children }: { tone?: Tone; children: ReactNode }) {
  const Icon = tone === 'good' ? CheckCircle2 : tone === 'warn' ? AlertTriangle : tone === 'crit' ? OctagonAlert : tone === 'info' ? Info : null
  return (
    <span className={`badge ${tone === 'neutral' ? '' : tone}`}>
      {Icon && <Icon size={14} aria-hidden />}
      {children}
    </span>
  )
}

export function Meter({ value, warnAt, critAt, label }: { value: number; warnAt?: number; critAt?: number; label: string }) {
  const tone = critAt !== undefined && value >= critAt ? 'crit' : warnAt !== undefined && value >= warnAt ? 'warn' : ''
  const pct = Math.max(0, Math.min(100, value))
  return (
    <div className={`meter ${tone}`} role="meter" aria-label={label} aria-valuemin={0} aria-valuemax={100} aria-valuenow={Math.round(pct)}>
      <i style={{ width: `${pct}%` }} />
    </div>
  )
}

export function Empty({ icon, title, children }: { icon?: ReactNode; title: string; children?: ReactNode }) {
  return (
    <div className="empty">
      {icon}
      <strong>{title}</strong>
      {children && <p className="small">{children}</p>}
    </div>
  )
}

export function ErrorLine({ error }: { error: string | null }) {
  return error ? <p className="err" role="alert">{error}</p> : null
}

// Closes on Escape and returns focus to what opened it.
function useDialog(onClose: () => void) {
  const ref = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null
    ref.current?.querySelector<HTMLElement>('input, select, textarea, button')?.focus()
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape') onClose() }
    document.addEventListener('keydown', key)
    return () => { document.removeEventListener('keydown', key); opener?.focus?.() }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  return ref
}

export function Drawer({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const ref = useDialog(onClose)
  return (
    <>
      <div className="scrim" onClick={onClose} />
      <div className="drawer" role="dialog" aria-modal="true" aria-label={title} ref={ref}>
        <header>
          <h2>{title}</h2>
          <button className="btn icon ghost" onClick={onClose} aria-label="Close"><X size={18} /></button>
        </header>
        <div className="body">{children}</div>
      </div>
    </>
  )
}

export function Modal({ title, onClose, children }: { title: string; onClose: () => void; children: ReactNode }) {
  const ref = useDialog(onClose)
  return (
    <>
      <div className="scrim" onClick={onClose} />
      <div className="modal" role="dialog" aria-modal="true" aria-label={title} ref={ref}>
        <div className="row" style={{ marginBottom: 'var(--s-4)' }}>
          <h2 style={{ flex: 1 }}>{title}</h2>
          <button className="btn icon sm ghost" onClick={onClose} aria-label="Close"><X size={18} /></button>
        </div>
        {children}
      </div>
    </>
  )
}

// ---- toasts ----
type ToastItem = { id: number; tone: Tone; title: string; detail?: string }
const ToastCtx = createContext<(t: Omit<ToastItem, 'id'>) => void>(() => {})
export const useToast = () => useContext(ToastCtx)

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([])
  const push = useCallback((t: Omit<ToastItem, 'id'>) => {
    const id = Date.now() + Math.random()
    setItems((x) => [...x.slice(-3), { ...t, id }])
    setTimeout(() => setItems((x) => x.filter((i) => i.id !== id)), 7000)
  }, [])
  return (
    <ToastCtx.Provider value={push}>
      {children}
      <div className="toasts" role="status" aria-live="polite">
        {items.map((t) => (
          <div className="toast" key={t.id}>
            <Badge tone={t.tone}>{t.tone === 'crit' ? 'Critical' : t.tone === 'warn' ? 'Needs you' : t.tone === 'good' ? 'Done' : 'Note'}</Badge>
            <div><strong>{t.title}</strong>{t.detail && <p>{t.detail}</p>}</div>
            <button className="btn icon sm ghost" onClick={() => setItems((x) => x.filter((i) => i.id !== t.id))} aria-label="Dismiss"><X size={16} /></button>
          </div>
        ))}
      </div>
    </ToastCtx.Provider>
  )
}
