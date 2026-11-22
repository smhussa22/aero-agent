import { metricName, splitKey, unitOf } from '../lib/format'
import { Empty, ErrorLine } from '../components/ui'
import { LineChart } from '../components/LineChart'
import { useHostId, useSeries } from './hooks'
import type { WidgetProps } from './registry'

export function ChartWidget({ config }: WidgetProps) {
  const host = useHostId(config.host ?? '')
  const keys = [config.m1 ?? '', config.m2 ?? '', config.m3 ?? ''].filter(Boolean)

  // one chart, one axis: metrics in another unit than the first are left out, with a note saying why
  const unit = keys.length ? unitOf(splitKey(keys[0]).metric) : 'percent'
  const used = keys.filter((k) => unitOf(splitKey(k).metric) === unit)
  const skipped = keys.filter((k) => !used.includes(k))
  const names = used.map((k) => metricName(splitKey(k).metric, splitKey(k).label))

  const { data, error, loading } = useSeries(host, used, Number(config.range ?? 3600), names)
  const series = (data?.series ?? []).filter((s) => s.points.length > 0)

  if (!host) return <Empty title="No hosts yet">Add a host and its charts will appear here.</Empty>
  if (used.length === 0) return <Empty title="Pick a metric">Open this widget’s settings and choose what to plot.</Empty>
  return (
    <div style={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      <ErrorLine error={error} />
      {series.length === 0
        ? <Empty title="No data in this range">{loading ? 'Loading…' : `${host} has not reported these metrics in this time range.`}</Empty>
        : <LineChart series={series} unit={unit} stale={loading} domain={data?.domain} title={`${names.join(' and ')} on ${host}`} />}
      {skipped.length > 0 && (
        <p className="hint" style={{ marginTop: 'var(--s-2)' }}>
          {skipped.map((k) => metricName(splitKey(k).metric, splitKey(k).label)).join(', ')} {skipped.length > 1 ? 'are' : 'is'} not shown: a chart has one axis, so every metric must use the same unit. Give it its own chart.
        </p>
      )}
    </div>
  )
}
