import { useMemo } from 'react'
import { api, q, type Latest, type Point } from '../lib/api'
import { splitKey } from '../lib/format'
import { useLive } from '../lib/live'
import { usePoll } from '../lib/poll'
import { bucketFor } from '../lib/scales'
import type { Series } from '../components/LineChart'

// '' means "the first host", so a freshly added widget works on a one-server setup with no configuration.
export function useHostId(configured: string): string {
  const { hosts } = useLive()
  if (configured && hosts.some((h) => h.id === configured)) return configured
  return hosts[0]?.id ?? ''
}

export function useLatest(host: string) {
  return usePoll((signal) => (host ? api<Latest[]>(`/api/hosts/${q(host)}/latest`, { signal }) : Promise.resolve([])), [host])
}

const SERIES_COLORS = ['var(--s1)', 'var(--s2)', 'var(--s3)']

export function useSeries(host: string, keys: string[], rangeSeconds: number, names: string[]) {
  const wanted = keys.filter(Boolean)
  const res = usePoll(async (signal) => {
    const to = Date.now(), from = to - rangeSeconds * 1000
    if (!host || wanted.length === 0) return { series: [] as Series[], domain: [from, to] as [number, number] }
    const bucket = bucketFor(rangeSeconds)
    const series = await Promise.all(wanted.map(async (key, i): Promise<Series> => {
      const { metric, label } = splitKey(key)
      const pts = await api<Point[]>(
        `/api/hosts/${q(host)}/metrics?metric=${q(metric)}&label=${q(label)}&from=${q(new Date(from).toISOString())}&to=${q(new Date(to).toISOString())}&bucket=${bucket}`, { signal })
      return { id: key, name: names[i] ?? metric, color: SERIES_COLORS[i] ?? SERIES_COLORS[0], points: pts.map((p) => ({ t: Date.parse(p.time), v: p.value })) }
    }))
    return { series, domain: [from, to] as [number, number] }
  }, [host, wanted.join(','), rangeSeconds])
  return useMemo(() => res, [res])
}
