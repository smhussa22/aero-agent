import { useEffect, useRef, useState } from 'react'

// Measures an element so SVG charts can draw at its real pixel size.
export function useSize<T extends HTMLElement>(initial = { w: 320, h: 220 }) {
  const ref = useRef<T>(null)
  const [size, setSize] = useState(initial)
  useEffect(() => {
    const el = ref.current
    if (!el) return
    const ro = new ResizeObserver(([e]) => {
      const w = Math.floor(e.contentRect.width), h = Math.floor(e.contentRect.height)
      setSize((s) => (s.w === w && s.h === h ? s : { w: Math.max(120, w), h: Math.max(60, h) }))
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, size] as const
}
