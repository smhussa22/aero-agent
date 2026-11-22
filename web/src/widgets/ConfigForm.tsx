import { useMemo } from 'react'
import { metricKey, metricName, splitKey, unitOf } from '../lib/format'
import { useLive } from '../lib/live'
import { RANGES, type FieldDef } from './registry'
import { useHostId, useLatest } from './hooks'

// Renders the config fields a widget declares. Metric choices come from what the host really reports.
export function ConfigForm({ fields, config, onChange }: { fields: FieldDef[]; config: Record<string, string>; onChange: (c: Record<string, string>) => void }) {
  const { hosts } = useLive()
  const host = useHostId(config.host ?? '')
  const { data: latest } = useLatest(host)
  const set = (key: string, value: string) => onChange({ ...config, [key]: value })

  const metrics = useMemo(() => {
    const seen = new Set<string>()
    return (latest ?? []).filter((l) => !seen.has(metricKey(l.metric, l.label)) && seen.add(metricKey(l.metric, l.label)))
  }, [latest])

  return (
    <div className="stack">
      {fields.map((f) => {
        const id = `cfg-${f.key}`
        let control
        if (f.kind === 'host') {
          control = (
            <select id={id} className="input" value={config[f.key] ?? ''} onChange={(e) => set(f.key, e.target.value)}>
              <option value="">First host</option>
              {hosts.map((h) => <option key={h.id} value={h.id}>{h.id}</option>)}
            </select>
          )
        } else if (f.kind === 'metric') {
          const unit = f.sameUnitAs && config[f.sameUnitAs] ? unitOf(splitKey(config[f.sameUnitAs]).metric) : null
          const options = metrics.filter((m) => !unit || unitOf(m.metric) === unit)
          const current = config[f.key] ?? ''
          control = (
            <select id={id} className="input" value={current} onChange={(e) => set(f.key, e.target.value)}>
              <option value="">{f.sameUnitAs ? 'None' : 'Choose a metric'}</option>
              {current && !options.some((m) => metricKey(m.metric, m.label) === current) && <option value={current}>{metricName(splitKey(current).metric, splitKey(current).label)}</option>}
              {options.map((m) => <option key={metricKey(m.metric, m.label)} value={metricKey(m.metric, m.label)}>{metricName(m.metric, m.label)}</option>)}
            </select>
          )
        } else if (f.kind === 'range') {
          control = (
            <select id={id} className="input" value={config[f.key] ?? '3600'} onChange={(e) => set(f.key, e.target.value)}>
              {RANGES.map((r) => <option key={r.value} value={r.value}>{r.label}</option>)}
            </select>
          )
        } else if (f.kind === 'textarea') {
          control = <textarea id={id} className="input" value={config[f.key] ?? ''} maxLength={4000} onChange={(e) => set(f.key, e.target.value)} />
        } else {
          control = <input id={id} className="input" type={f.kind === 'number' ? 'number' : 'text'} inputMode={f.kind === 'number' ? 'decimal' : undefined}
            value={config[f.key] ?? ''} onChange={(e) => set(f.key, e.target.value)} />
        }
        return (
          <label className="field" key={f.key} htmlFor={id}>
            <span>{f.label}</span>
            {control}
            {f.hint && <span className="hint">{f.hint}</span>}
          </label>
        )
      })}
      {fields.length === 0 && <p className="muted">This widget has nothing to configure.</p>}
    </div>
  )
}
