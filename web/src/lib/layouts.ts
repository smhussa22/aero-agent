// Dashboard views: named sets of widgets with positions. Stored in this browser; can be exported
// and imported as JSON, so a layout is shareable and never trusted blindly on the way in.

export type Cell = { x: number; y: number; w: number; h: number }
export type WidgetInstance = { id: string; type: string; config: Record<string, string>; cell: Cell }
export type View = { id: string; name: string; widgets: WidgetInstance[] }
export type Dashboards = { activeId: string; views: View[] }

export const COLS = 12
const KEY = 'aero.dashboards.v1'
const MAX_VIEWS = 12
const MAX_WIDGETS = 40

export const uid = () => Math.random().toString(36).slice(2, 10)

const num = (v: unknown, min: number, max: number, fallback: number) =>
  typeof v === 'number' && Number.isFinite(v) ? Math.min(max, Math.max(min, Math.round(v))) : fallback

export function cleanWidget(raw: unknown, isKnown: (type: string) => boolean): WidgetInstance | null {
  if (!raw || typeof raw !== 'object') return null
  const w = raw as Record<string, unknown>
  if (typeof w.type !== 'string' || !isKnown(w.type)) return null
  const cell = (w.cell ?? {}) as Record<string, unknown>
  const config: Record<string, string> = {}
  if (w.config && typeof w.config === 'object') {
    for (const [k, v] of Object.entries(w.config as Record<string, unknown>).slice(0, 20)) {
      if (typeof v === 'string') config[k.slice(0, 40)] = v.slice(0, 4000)
    }
  }
  const width = num(cell.w, 1, COLS, 4)
  return {
    id: typeof w.id === 'string' && /^[a-z0-9]{1,16}$/.test(w.id) ? w.id : uid(),
    type: w.type, config,
    cell: { x: Math.min(num(cell.x, 0, COLS - 1, 0), COLS - width), y: num(cell.y, 0, 1000, 0), w: width, h: num(cell.h, 1, 40, 4) },
  }
}

export function cleanDashboards(raw: unknown, isKnown: (type: string) => boolean): Dashboards | null {
  if (!raw || typeof raw !== 'object') return null
  const r = raw as Record<string, unknown>
  if (!Array.isArray(r.views)) return null
  const views: View[] = []
  for (const v of r.views.slice(0, MAX_VIEWS)) {
    if (!v || typeof v !== 'object') continue
    const view = v as Record<string, unknown>
    const widgets = (Array.isArray(view.widgets) ? view.widgets : []).slice(0, MAX_WIDGETS)
      .map((w) => cleanWidget(w, isKnown)).filter((w): w is WidgetInstance => w !== null)
    views.push({
      id: typeof view.id === 'string' && /^[a-z0-9]{1,16}$/.test(view.id) ? view.id : uid(),
      name: typeof view.name === 'string' && view.name.trim() ? view.name.trim().slice(0, 40) : 'Untitled',
      widgets,
    })
  }
  if (views.length === 0) return null
  const activeId = views.some((v) => v.id === r.activeId) ? (r.activeId as string) : views[0].id
  return { activeId, views }
}

export function loadDashboards(isKnown: (type: string) => boolean, fallback: () => Dashboards): Dashboards {
  try {
    const parsed = cleanDashboards(JSON.parse(localStorage.getItem(KEY) ?? 'null'), isKnown)
    if (parsed) return parsed
  } catch { /* corrupt storage: start over with the default view */ }
  return fallback()
}

export function saveDashboards(d: Dashboards) {
  try { localStorage.setItem(KEY, JSON.stringify(d)) } catch { /* private mode: layout lasts this session only */ }
}

export function exportView(v: View): string {
  return JSON.stringify({ aero: 1, view: { name: v.name, widgets: v.widgets.map(({ type, config, cell }) => ({ type, config, cell })) } }, null, 2)
}

// Returns a fresh view (new ids) or an error message; imported JSON is validated like any untrusted input.
export function importView(text: string, isKnown: (type: string) => boolean): View | string {
  let parsed: unknown
  try { parsed = JSON.parse(text) } catch { return 'That is not valid JSON.' }
  const view = (parsed as { view?: unknown })?.view
  if (!view || typeof view !== 'object' || !Array.isArray((view as { widgets?: unknown }).widgets)) return 'That file is not an Aero layout.'
  const clean = cleanDashboards({ views: [{ ...(view as object), id: uid() }] }, isKnown)
  if (!clean) return 'That file is not an Aero layout.'
  const v = clean.views[0]
  return { ...v, id: uid(), widgets: v.widgets.map((w) => ({ ...w, id: uid() })) }
}

// First free row for a new widget of the given width: below everything already placed.
export function nextCell(widgets: WidgetInstance[], w: number, h: number): Cell {
  const bottom = widgets.reduce((m, x) => Math.max(m, x.cell.y + x.cell.h), 0)
  return { x: 0, y: bottom, w: Math.min(w, COLS), h }
}
