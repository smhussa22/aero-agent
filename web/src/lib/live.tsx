import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { api, type Host, type Incident, type Pending } from './api'
import { deriveAlerts, type AlertItem } from './alerts'
import { usePoll } from './poll'
import { getSettings } from './settings'
import { useToast } from '../components/ui'

type Live = {
  hosts: Host[]
  incidents: Incident[]
  pending: Pending[]
  alerts: AlertItem[]
  unread: number
  markAllRead: () => void
  loading: boolean
  error: string | null
  reload: () => void
}

const Ctx = createContext<Live | null>(null)
export const useLive = () => {
  const v = useContext(Ctx)
  if (!v) throw new Error('useLive outside LiveProvider')
  return v
}

const READ_KEY = 'aero.alerts.read'
const readSet = (): Set<string> => {
  try { return new Set(JSON.parse(sessionStorage.getItem(READ_KEY) ?? '[]')) } catch { return new Set() }
}

// One shared poll of hosts, incidents and pending approvals feeds every widget, the alert bell and the
// toasts, so the numbers on screen always agree and the server is asked once.
export function LiveProvider({ children }: { children: ReactNode }) {
  const toast = useToast()
  const { data, error, loading, reload } = usePoll(async (signal) => {
    const [hosts, incidents, pending] = await Promise.all([
      api<Host[]>('/api/hosts', { signal }),
      api<Incident[]>('/api/incidents?limit=50', { signal }),
      api<Pending[]>('/api/approvals/pending', { signal }),
    ])
    return { hosts, incidents, pending }
  }, [])

  const hosts = data?.hosts ?? []
  const incidents = data?.incidents ?? []
  const pending = data?.pending ?? []
  const alerts = useMemo(() => deriveAlerts(hosts, incidents, pending), [data]) // eslint-disable-line react-hooks/exhaustive-deps

  const [read, setRead] = useState(readSet)
  const announced = useRef<Set<string> | null>(null)

  // announce each new alert once; the first load only records what already exists so opening the app is quiet
  useEffect(() => {
    if (!data) return
    if (announced.current === null) { announced.current = new Set(alerts.map((a) => a.id)); return }
    for (const a of alerts) {
      if (announced.current.has(a.id)) continue
      announced.current.add(a.id)
      toast({ tone: a.tone, title: a.title, detail: a.detail })
      if (getSettings().notify && document.hidden && 'Notification' in window && Notification.permission === 'granted') {
        new Notification(a.title, { body: a.detail.slice(0, 160), tag: a.id })
      }
    }
  }, [data, alerts, toast])

  const markAllRead = useCallback(() => {
    const next = new Set([...read, ...alerts.map((a) => a.id)])
    setRead(next)
    try { sessionStorage.setItem(READ_KEY, JSON.stringify([...next].slice(-200))) } catch { /* ignore */ }
  }, [read, alerts])

  const value: Live = { hosts, incidents, pending, alerts, unread: alerts.filter((a) => !read.has(a.id)).length, markAllRead, loading, error, reload }
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>
}
