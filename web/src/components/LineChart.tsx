import { useMemo, useState } from 'react'
import { clock, fmtPlain, fmtTick, type Unit } from '../lib/format'
import { linear, nearest, niceTicks, timeTicks } from '../lib/scales'
import { useSize } from '../lib/useSize'

export type Series = { id: string; name: string; color: string; points: { t: number; v: number }[] }

const M = { top: 12, right: 16, bottom: 26, left: 48 }

// Line chart built to the dataviz spec: 2px lines, 8px end dot with a 2px surface ring, hairline grid,
// one axis (the caller guarantees one unit), a crosshair that snaps to the nearest time with one tooltip
// listing every series, a legend for two or more series, and a table view as the non-visual route.
// `domain` is the time window that was asked for, so a short history shows as a short line, not a stretched one.
export function LineChart({ series, unit, stale, title, domain }: { series: Series[]; unit: Unit; stale?: boolean; title: string; domain?: [number, number] }) {
  const [box, { w: width, h: height }] = useSize<HTMLDivElement>({ w: 560, h: 240 })
  const [hover, setHover] = useState<number | null>(null)
  const [table, setTable] = useState(false)

  const geo = useMemo(() => {
    const all = series.flatMap((s) => s.points)
    if (all.length === 0) return null
    const t0 = domain?.[0] ?? Math.min(...all.map((p) => p.t))
    const t1 = domain?.[1] ?? Math.max(...all.map((p) => p.t))
    const dataMax = Math.max(...all.map((p) => p.v))
    const ticks = niceTicks(unit === 'percent' ? Math.max(10, dataMax) : dataMax)
    const x = linear(t0, t1 === t0 ? t0 + 1 : t1, M.left, width - M.right)
    const y = linear(0, ticks[ticks.length - 1], height - M.bottom, M.top)
    // the crosshair snaps over the first series' timestamps (all series share one bucket grid)
    return { t0, t1, ticks, x, y, ts: (series[0]?.points ?? []).map((p) => p.t) }
  }, [series, unit, width, height, domain])

  const toggle = (
    <div className="row" style={{ justifyContent: 'flex-end', marginBottom: 'var(--s-1)' }}>
      <button className="btn sm ghost" onClick={() => setTable((v) => !v)} aria-pressed={table}>{table ? 'Show chart' : 'Show table'}</button>
    </div>
  )

  if (!geo) return <div className="chart-box"><div className="chart" ref={box} /></div>
  const { ticks, x, y, ts, t0, t1 } = geo
  const span = t1 - t0
  const day = span > 20 * 3600_000
  const secs = span < 20 * 60_000
  const idx = hover === null || ts.length === 0 ? null : Math.min(Math.max(hover, 0), ts.length - 1)
  const cursorX = idx === null ? null : x(ts[idx])

  const onMove = (e: React.PointerEvent<SVGSVGElement>) => {
    const r = e.currentTarget.getBoundingClientRect()
    setHover(nearest(ts, t0 + ((e.clientX - r.left - M.left) / (width - M.left - M.right)) * span))
  }
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'ArrowLeft') setHover((h) => Math.max(0, (h ?? ts.length) - 1))
    else if (e.key === 'ArrowRight') setHover((h) => Math.min(ts.length - 1, (h ?? -1) + 1))
    else if (e.key === 'Escape') setHover(null)
  }

  const path = (s: Series) => s.points.map((p, i) => `${i ? 'L' : 'M'}${x(p.t).toFixed(1)},${y(p.v).toFixed(1)}`).join('')
  const valueAt = (s: Series, t: number) => s.points.find((p) => p.t === t) ?? s.points[nearest(s.points.map((p) => p.t), t)]

  return (
    <div className="chart-box">
      {toggle}
      {table ? (
        <div style={{ flex: 1, minHeight: 0, overflow: 'auto' }}>
          <table className="table">
            <caption className="sr">{title}</caption>
            <thead><tr><th>Time</th>{series.map((s) => <th key={s.id} className="num">{s.name}</th>)}</tr></thead>
            <tbody>
              {ts.slice(-40).reverse().map((t) => (
                <tr key={t}><td>{clock(t, day, secs)}</td>{series.map((s) => <td key={s.id} className="num">{fmtPlain(valueAt(s, t).v, unit)}</td>)}</tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className={`chart ${stale ? 'dim' : ''}`} ref={box}>
          <svg
            width={width} height={height} viewBox={`0 0 ${width} ${height}`} role="img" tabIndex={0}
            aria-label={`${title}. Use the left and right arrow keys to read values, or open the table view.`}
            onPointerMove={onMove} onPointerLeave={() => setHover(null)} onKeyDown={onKey} onBlur={() => setHover(null)}
          >
            <g className="grid axis">
              {ticks.map((v) => (
                <g key={v}>
                  <line x1={M.left} x2={width - M.right} y1={y(v)} y2={y(v)} />
                  <text x={M.left - 8} y={y(v) + 4} textAnchor="end">{fmtTick(v, unit)}</text>
                </g>
              ))}
            </g>
            <line className="baseline" x1={M.left} x2={width - M.right} y1={y(0)} y2={y(0)} />
            <g className="axis">
              {timeTicks(t0, t1, width < 480 ? 3 : 5).map((t, i, a) => (
                <text key={t} x={x(t)} y={height - 6} textAnchor={i === 0 ? 'start' : i === a.length - 1 ? 'end' : 'middle'}>{clock(t, day, secs)}</text>
              ))}
            </g>

            {series.length === 1 && (
              <path d={`${path(series[0])}L${x(series[0].points.at(-1)!.t)},${y(0)}L${x(series[0].points[0].t)},${y(0)}Z`} fill={series[0].color} opacity={0.1} />
            )}
            {series.map((s) => <path key={s.id} d={path(s)} fill="none" stroke={s.color} strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />)}

            {cursorX !== null && <line className="cursor" x1={cursorX} x2={cursorX} y1={M.top} y2={height - M.bottom} />}
            {series.map((s) => {
              const p = idx === null ? s.points.at(-1)! : valueAt(s, ts[idx])
              return <circle key={s.id} cx={x(p.t)} cy={y(p.v)} r={4} fill={s.color} stroke="var(--surface)" strokeWidth={2} />
            })}
          </svg>

          {idx !== null && cursorX !== null && (
            <div className="tip" style={{ left: Math.min(Math.max(cursorX, 90), width - 90), top: M.top + 8 }} role="status">
              <div className="t">{clock(ts[idx], true, true)}</div>
              {series.map((s) => (
                <div className="r" key={s.id}>
                  <i className="key" style={{ borderColor: s.color }} />
                  <b>{fmtPlain(valueAt(s, ts[idx]).v, unit)}</b>
                  <span>{s.name}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}

      {series.length >= 2 && !table && (
        <div className="legend">
          {series.map((s) => <span key={s.id}><i className="key" style={{ borderColor: s.color }} />{s.name}</span>)}
        </div>
      )}
    </div>
  )
}

// Sparkline: de-emphasised line, with the current value as the accent dot. Drawn at its real width so the dot stays round.
export function Sparkline({ values, color = 'var(--s1)', label }: { values: number[]; color?: string; label: string }) {
  const [ref, { w }] = useSize<HTMLDivElement>({ w: 160, h: 36 })
  const h = 36
  const body = (() => {
    if (values.length < 2) return null
    const max = Math.max(...values, 1), min = Math.min(...values, 0)
    const x = linear(0, values.length - 1, 4, w - 6)
    const y = linear(min, max, h - 5, 5)
    const d = values.map((v, i) => `${i ? 'L' : 'M'}${x(i).toFixed(1)},${y(v).toFixed(1)}`).join('')
    return (
      <svg width={w} height={h} role="img" aria-label={label} style={{ display: 'block' }}>
        <path d={d} fill="none" stroke="var(--axis)" strokeWidth={2} strokeLinejoin="round" strokeLinecap="round" />
        <circle cx={x(values.length - 1)} cy={y(values[values.length - 1])} r={4} fill={color} stroke="var(--surface)" strokeWidth={2} />
      </svg>
    )
  })()
  return <div ref={ref} style={{ height: h, width: '100%' }}>{body}</div>
}
