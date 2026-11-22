// Units are inferred from the metric name; a chart refuses to mix units (one axis, never two).

export type Unit = 'percent' | 'bytes' | 'load' | 'count'

export function unitOf(metric: string): Unit {
  if (metric.endsWith('_percent')) return 'percent'
  if (metric.endsWith('_bytes') || metric.endsWith('_bytes_total')) return 'bytes'
  if (metric.startsWith('cpu.load_')) return 'load'
  return 'count'
}

export function fmtBytes(n: number): string {
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  while (Math.abs(n) >= 1024 && i < units.length - 1) { n /= 1024; i++ }
  return `${n >= 100 || i === 0 ? Math.round(n) : n.toFixed(1)} ${units[i]}`
}

export function fmtValue(v: number, unit: Unit): { text: string; unit: string } {
  switch (unit) {
    case 'percent': return { text: v >= 100 ? '100' : v.toFixed(v < 10 ? 1 : 0), unit: '%' }
    case 'bytes': { const [t, u] = fmtBytes(v).split(' '); return { text: t, unit: u } }
    case 'load': return { text: v.toFixed(2), unit: '' }
    default: return { text: Number.isInteger(v) ? String(v) : v.toFixed(1), unit: '' }
  }
}

export function fmtPlain(v: number, unit: Unit): string {
  const f = fmtValue(v, unit)
  return `${f.text}${f.unit && f.unit !== '%' ? ' ' : ''}${f.unit}`
}

// axis ticks: clean numbers, never a stray decimal (0%, 25%, 50%)
export function fmtTick(v: number, unit: Unit): string {
  if (unit === 'percent') return `${+v.toFixed(1)}%`
  if (unit === 'bytes') return fmtBytes(v)
  return String(+v.toFixed(2))
}

export function timeAgo(iso: string | null, now = Date.now()): string {
  if (!iso) return 'never'
  const s = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000))
  if (s < 10) return 'just now'
  if (s < 60) return `${s}s ago`
  if (s < 3600) return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

export function clock(ms: number, withDate = false, seconds = false): string {
  const d = new Date(ms)
  const t = d.toLocaleTimeString([], seconds ? { hour: '2-digit', minute: '2-digit', second: '2-digit' } : { hour: '2-digit', minute: '2-digit' })
  return withDate ? `${d.toLocaleDateString([], { month: 'short', day: 'numeric' })} ${t}` : t
}

// "cpu.usage_percent" -> "CPU usage", "disk.used_percent" + "/" -> "Disk used · /"
const WORDS: Record<string, string> = { cpu: 'CPU', net: 'Network', rx: 'received', tx: 'sent' }
export function metricName(metric: string, label = ''): string {
  const base = metric
    .replace(/_percent$/, '').replace(/_bytes$/, '')
    .split(/[._]/).map((w, i) => WORDS[w] ?? (i === 0 ? w[0].toUpperCase() + w.slice(1) : w)).join(' ')
  return label ? `${base} · ${label}` : base
}

export const metricKey = (metric: string, label: string) => `${metric}|${label}`
export function splitKey(key: string): { metric: string; label: string } {
  const i = key.indexOf('|')
  return i < 0 ? { metric: key, label: '' } : { metric: key.slice(0, i), label: key.slice(i + 1) }
}
