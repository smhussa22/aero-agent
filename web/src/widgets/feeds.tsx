import { CheckCheck, Server as ServerIcon } from 'lucide-react'
import { api, type AuditEntry } from '../lib/api'
import { fmtValue, metricName, timeAgo } from '../lib/format'
import { SILENT_AFTER_MS } from '../lib/alerts'
import { useLive } from '../lib/live'
import { usePoll } from '../lib/poll'
import { Badge, Empty, ErrorLine, Meter } from '../components/ui'
import { IncidentBadge, ModeBadge, PendingItem } from '../components/status'
import { useHostId, useLatest } from './hooks'
import type { WidgetProps } from './registry'

export function MetersWidget({ config }: WidgetProps) {
  const host = useHostId(config.host ?? '')
  const { data, loading } = useLatest(host)
  const rows = (data ?? []).filter((l) => l.metric === 'memory.used_percent' || l.metric === 'disk.used_percent')
  if (!host) return <Empty title="No hosts yet" />
  if (rows.length === 0) return <Empty title="No usage data">{loading ? 'Loading…' : `${host} has not reported memory or disk usage recently.`}</Empty>
  return (
    <ul className="list">
      {rows.map((r) => (
        <li key={r.metric + r.label} style={{ flexDirection: 'column', gap: 'var(--s-2)' }}>
          <div className="row" style={{ justifyContent: 'space-between', width: '100%' }}>
            <span>{metricName(r.metric, r.label)}</span>
            <strong>{fmtValue(r.value, 'percent').text}%</strong>
          </div>
          <div style={{ width: '100%' }}><Meter value={r.value} warnAt={80} critAt={90} label={metricName(r.metric, r.label)} /></div>
        </li>
      ))}
    </ul>
  )
}

export function HostsWidget(_: WidgetProps) {
  const { hosts, loading } = useLive()
  if (hosts.length === 0) return <Empty icon={<ServerIcon size={22} />} title="No hosts yet">{loading ? 'Loading…' : 'Run hostctl add and start the monitoring agent to see a server here.'}</Empty>
  return (
    <ul className="list">
      {hosts.map((h) => {
        const silent = h.last_seen ? Date.now() - new Date(h.last_seen).getTime() > SILENT_AFTER_MS : false
        return (
          <li key={h.id} style={{ alignItems: 'center' }}>
            <div className="grow">
              <strong style={{ overflowWrap: 'break-word' }}>{h.id}</strong>
              <p className="muted small">{h.last_seen ? `Reported ${timeAgo(h.last_seen)}` : 'Has not reported yet'}</p>
            </div>
            {silent ? <Badge tone="warn">Silent</Badge> : h.last_seen ? <Badge tone="good">Reporting</Badge> : null}
            <ModeBadge mode={h.mode} />
          </li>
        )
      })}
    </ul>
  )
}

export function IncidentsWidget(_: WidgetProps) {
  const { incidents, loading } = useLive()
  if (incidents.length === 0) return <Empty title="No incidents">{loading ? 'Loading…' : 'Nothing has fired. When it does, the AI agent’s explanation lands here.'}</Empty>
  return (
    <ul className="list">
      {incidents.slice(0, 8).map((i) => (
        <li key={i.id}>
          <a href={`#/incidents/${i.id}`} style={{ flex: 1, minWidth: 0, textDecoration: 'none' }}>
            <div className="row" style={{ gap: 'var(--s-2)' }}>
              <IncidentBadge status={i.status} />
              <strong>{i.host_id}</strong>
              <span className="muted small">{timeAgo(i.opened_at)}</span>
            </div>
            <p className="small" style={{ marginTop: 'var(--s-1)', color: 'var(--ink-2)', overflowWrap: 'anywhere' }}>
              {i.summary ? (i.summary.length > 180 ? i.summary.slice(0, 180) + '…' : i.summary) : 'Waiting for the AI agent to investigate.'}
            </p>
          </a>
        </li>
      ))}
    </ul>
  )
}

export function ApprovalsWidget(_: WidgetProps) {
  const { pending, loading } = useLive()
  if (pending.length === 0) return <Empty icon={<CheckCheck size={22} />} title="Nothing to approve">{loading ? 'Loading…' : 'When the AI agent proposes a fix that needs you, it shows up here.'}</Empty>
  return <ul className="list">{pending.map((p) => <li key={p.id}><PendingItem p={p} /></li>)}</ul>
}

export function AlertsWidget(_: WidgetProps) {
  const { alerts, unread, markAllRead } = useLive()
  if (alerts.length === 0) return <Empty icon={<CheckCheck size={22} />} title="All quiet">Nothing needs your attention right now.</Empty>
  return (
    <div>
      <div className="row" style={{ justifyContent: 'space-between', marginBottom: 'var(--s-2)' }}>
        <span className="label">{alerts.length} need attention{unread ? `, ${unread} new` : ''}</span>
        {unread > 0 && <button className="btn sm ghost" onClick={markAllRead}>Mark all read</button>}
      </div>
      <ul className="list">
        {alerts.map((a) => (
          <li key={a.id}>
            <a href={a.href} style={{ flex: 1, minWidth: 0, textDecoration: 'none' }}>
              <div className="row" style={{ gap: 'var(--s-2)' }}>
                <Badge tone={a.tone}>{a.tone === 'crit' ? 'Critical' : a.tone === 'warn' ? 'Needs you' : 'AI finding'}</Badge>
                <span className="muted small">{timeAgo(a.at)}</span>
              </div>
              <strong style={{ display: 'block', marginTop: 'var(--s-1)' }}>{a.title}</strong>
              <p className="small" style={{ color: 'var(--ink-2)', overflowWrap: 'anywhere' }}>{a.detail.length > 160 ? a.detail.slice(0, 160) + '…' : a.detail}</p>
            </a>
          </li>
        ))}
      </ul>
    </div>
  )
}

export function AuditWidget(_: WidgetProps) {
  const { data, error, loading } = usePoll((signal) => api<AuditEntry[]>('/api/audit?limit=15', { signal }), [])
  if (error && !data) return <ErrorLine error={error} />
  if (!data || data.length === 0) return <Empty title="No audit entries yet">{loading ? 'Loading…' : 'Decisions and actions are recorded here.'}</Empty>
  return (
    <ul className="list">
      {data.map((e) => (
        <li key={e.id} style={{ flexDirection: 'column', gap: 2 }}>
          <div className="row" style={{ justifyContent: 'space-between', width: '100%' }}>
            <strong className="small">{e.event.replace(/_/g, ' ')}</strong>
            <span className="muted small">{timeAgo(e.at)}</span>
          </div>
          <span className="muted small" style={{ overflowWrap: 'anywhere' }}>{e.actor}</span>
        </li>
      ))}
    </ul>
  )
}
