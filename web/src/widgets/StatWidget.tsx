import { api, q, type Point } from '../lib/api'
import { fmtValue, metricName, splitKey, unitOf } from '../lib/format'
import { usePoll } from '../lib/poll'
import { Badge, Empty, Meter } from '../components/ui'
import { Sparkline } from '../components/LineChart'
import { useHostId, useLatest } from './hooks'
import type { WidgetProps } from './registry'

const nz = (s: string | undefined, d: number) => (s !== undefined && s.trim() !== '' && Number.isFinite(Number(s)) ? Number(s) : d)

export function StatWidget({ config }: WidgetProps) {
  const host = useHostId(config.host ?? '')
  const { metric, label } = splitKey(config.metric ?? 'cpu.usage_percent|')
  const latest = useLatest(host)
  const trend = usePoll(async (signal) => {
    if (!host) return [] as Point[]
    const to = new Date(), from = new Date(to.getTime() - 30 * 60_000)
    return api<Point[]>(`/api/hosts/${q(host)}/metrics?metric=${q(metric)}&label=${q(label)}&from=${q(from.toISOString())}&to=${q(to.toISOString())}&bucket=60`, { signal })
  }, [host, metric, label])

  const current = latest.data?.find((l) => l.metric === metric && l.label === label)
  if (!host) return <Empty title="No hosts yet">Add a host and its numbers will show here.</Empty>
  if (!current) return <Empty title="No data">{latest.loading ? 'Loading…' : `${host} has not reported ${metricName(metric, label)} recently.`}</Empty>

  const unit = unitOf(metric)
  const v = fmtValue(current.value, unit)
  const warn = nz(config.warn, 80), crit = nz(config.crit, 90)
  const isPct = unit === 'percent'
  const level = !isPct ? 'neutral' : current.value >= crit ? 'crit' : current.value >= warn ? 'warn' : 'good'

  return (
    <div className="stat">
      <div className="row" style={{ justifyContent: 'space-between' }}>
        <span className="label">{host}</span>
        {isPct && <Badge tone={level as 'good' | 'warn' | 'crit'}>{level === 'crit' ? 'Critical' : level === 'warn' ? 'High' : 'Normal'}</Badge>}
      </div>
      <div><span className="value">{v.text}</span><span className="unit">{v.unit}</span></div>
      {isPct ? <Meter value={current.value} warnAt={warn} critAt={crit} label={metricName(metric, label)} /> : null}
      <Sparkline values={(trend.data ?? []).map((p) => p.value)} label={`${metricName(metric, label)}, last 30 minutes`} color={level === 'crit' ? 'var(--crit)' : level === 'warn' ? 'var(--warn)' : 'var(--s1)'} />
    </div>
  )
}
