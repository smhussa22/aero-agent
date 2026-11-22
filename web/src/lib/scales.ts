// Small pure helpers for the charts, kept apart so they can be tested.

// "Nice" tick values from 0 up to at least `max`: 0, 25, 50, 75, 100 rather than 0, 23.4, 46.8...
export function niceTicks(max: number, target = 4): number[] {
  if (!(max > 0)) return [0, 1]
  const rough = max / target
  const mag = Math.pow(10, Math.floor(Math.log10(rough)))
  const step = [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => s >= rough) ?? 10 * mag
  const ticks: number[] = []
  for (let v = 0; v < max + step * 0.999; v += step) ticks.push(Math.round(v / step) * step)
  return ticks
}

export function linear(d0: number, d1: number, r0: number, r1: number) {
  const span = d1 - d0 || 1
  return (v: number) => r0 + ((v - d0) / span) * (r1 - r0)
}

// Index of the value in sorted `xs` closest to `x` (binary search).
export function nearest(xs: number[], x: number): number {
  let lo = 0, hi = xs.length - 1
  if (hi < 0) return -1
  while (lo < hi) {
    const mid = (lo + hi) >> 1
    if (xs[mid] < x) lo = mid + 1; else hi = mid
  }
  return lo > 0 && Math.abs(xs[lo - 1] - x) <= Math.abs(xs[lo] - x) ? lo - 1 : lo
}

// Evenly spaced timestamps across [t0, t1] for the x axis.
export function timeTicks(t0: number, t1: number, count = 5): number[] {
  if (t1 <= t0) return [t0]
  return Array.from({ length: count }, (_, i) => t0 + ((t1 - t0) * i) / (count - 1))
}

// Averaging bucket (seconds) that keeps a range near `points` samples.
export function bucketFor(rangeSeconds: number, points = 240): number {
  const raw = rangeSeconds / points
  return raw <= 10 ? 0 : [15, 30, 60, 120, 300, 600, 900, 1800, 3600].find((b) => b >= raw) ?? 3600
}
