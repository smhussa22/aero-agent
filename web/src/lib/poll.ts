import { useCallback, useEffect, useRef, useState } from 'react'
import { useSettings } from './settings'

export type Polled<T> = { data: T | undefined; error: string | null; loading: boolean; reload: () => void }

// Fetches now and then every `every` seconds (default: the user's refresh setting). While a refetch is
// in flight the previous data stays, so charts keep their frame instead of flashing a skeleton.
export function usePoll<T>(fetcher: (signal: AbortSignal) => Promise<T>, deps: unknown[], every?: number): Polled<T> {
  const { refreshSeconds } = useSettings()
  const seconds = every ?? refreshSeconds
  const [state, setState] = useState<{ data: T | undefined; error: string | null; loading: boolean }>({ data: undefined, error: null, loading: true })
  const fetchRef = useRef(fetcher)
  fetchRef.current = fetcher
  const [tick, setTick] = useState(0)
  const reload = useCallback(() => setTick((t) => t + 1), [])

  useEffect(() => {
    const ctrl = new AbortController()
    let timer: ReturnType<typeof setTimeout>
    const run = async () => {
      setState((s) => ({ ...s, loading: true }))
      try {
        const data = await fetchRef.current(ctrl.signal)
        if (!ctrl.signal.aborted) setState({ data, error: null, loading: false })
      } catch (e) {
        if (ctrl.signal.aborted) return
        setState((s) => ({ ...s, error: e instanceof Error ? e.message : 'request failed', loading: false }))
      }
      if (!ctrl.signal.aborted && seconds > 0) timer = setTimeout(run, seconds * 1000)
    }
    run()
    return () => { ctrl.abort(); clearTimeout(timer) }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, seconds, tick])

  return { ...state, reload }
}
