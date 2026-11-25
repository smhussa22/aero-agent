import { useEffect, useMemo, useRef, useState } from 'react'
import { Responsive, WidthProvider, type Layout } from 'react-grid-layout'
import 'react-grid-layout/css/styles.css'
import 'react-resizable/css/styles.css'
import { Copy, Download, LayoutGrid, Pencil, Plus, RotateCcw, Settings2, Trash2, Upload } from 'lucide-react'
import { COLS, exportView, importView, loadDashboards, nextCell, saveDashboards, uid, type Dashboards, type View, type WidgetInstance } from '../lib/layouts'
import { Drawer, Modal, useToast } from '../components/ui'
import { ConfigForm } from '../widgets/ConfigForm'
import { useLive } from '../lib/live'
import { AddServer } from './Hosts'
import { isAdmin } from '../components/status'
import { isKnownWidget, registry, widgetList } from '../widgets/registry'

const Grid = WidthProvider(Responsive)
const w = (type: string, x: number, y: number, ww: number, h: number, config: Record<string, string> = {}): WidgetInstance =>
  ({ id: uid(), type, config, cell: { x, y, w: ww, h } })

// What a fresh install sees, in the order you need it at 3am: the key numbers, then what needs you, then the detail.
export function defaultDashboards(): Dashboards {
  const overview: View = {
    id: uid(), name: 'Overview',
    widgets: [
      w('stat', 0, 0, 3, 3, { metric: 'cpu.usage_percent|' }),
      w('stat', 3, 0, 3, 3, { metric: 'memory.used_percent|' }),
      w('stat', 6, 0, 3, 3, { metric: 'disk.used_percent|/' }),
      w('hosts', 9, 0, 3, 3),
      w('incidents', 0, 3, 7, 4),
      w('approvals', 7, 3, 5, 4),
      w('chart', 0, 7, 8, 6, { range: '3600', m1: 'cpu.usage_percent|', m2: 'memory.used_percent|' }),
      w('alerts', 8, 7, 4, 6),
      w('audit', 0, 13, 12, 6),
    ],
  }
  return { activeId: overview.id, views: [overview] }
}

export function DashboardPage() {
  const toast = useToast()
  const live = useLive()
  const [dash, setDash] = useState<Dashboards>(() => loadDashboards(isKnownWidget, defaultDashboards))
  const [editing, setEditing] = useState(false)
  const [gallery, setGallery] = useState(false)
  const [configId, setConfigId] = useState<string | null>(null)
  const [dialog, setDialog] = useState<null | 'new' | 'rename' | 'export' | 'import' | 'reset'>(null)
  const breakpoint = useRef('lg')

  useEffect(() => saveDashboards(dash), [dash])

  const view = dash.views.find((v) => v.id === dash.activeId) ?? dash.views[0]
  const patchView = (fn: (v: View) => View) => setDash((d) => ({ ...d, views: d.views.map((v) => (v.id === view.id ? fn(v) : v)) }))
  const patchWidget = (id: string, fn: (x: WidgetInstance) => WidgetInstance) => patchView((v) => ({ ...v, widgets: v.widgets.map((x) => (x.id === id ? fn(x) : x)) }))

  const layout: Layout[] = useMemo(() => view.widgets.map((x) => ({
    i: x.id, ...x.cell, minW: registry[x.type].size.minW ?? 2, minH: registry[x.type].size.minH ?? 2,
  })), [view.widgets])

  // positions are only saved from the full-width layout, so a phone never rewrites the desktop arrangement
  const onLayoutChange = (cur: Layout[]) => {
    if (!editing || breakpoint.current !== 'lg') return
    patchView((v) => ({ ...v, widgets: v.widgets.map((x) => {
      const l = cur.find((c) => c.i === x.id)
      return l ? { ...x, cell: { x: l.x, y: l.y, w: l.w, h: l.h } } : x
    }) }))
  }

  const addWidget = (type: string) => {
    const def = registry[type]
    patchView((v) => ({ ...v, widgets: [...v.widgets, { id: uid(), type, config: { ...(def.defaults ?? {}) }, cell: nextCell(v.widgets, def.size.w, def.size.h) }] }))
    setGallery(false)
    toast({ tone: 'good', title: `${def.title} added`, detail: 'Drag it into place, then press Done.' })
  }

  const removeView = () => {
    if (dash.views.length === 1) return
    const rest = dash.views.filter((v) => v.id !== view.id)
    setDash({ activeId: rest[0].id, views: rest })
  }

  const configWidget = view.widgets.find((x) => x.id === configId)

  return (
    <div className="page">
      <div className="page-head">
        <div className="grow">
          <h1>Dashboard</h1>
          <p>{editing ? 'Drag a widget by its title to move it, pull the corner to resize it.' : 'Everything is live. Customize it to show what matters to you.'}</p>
        </div>
        <div className="tools">
          {editing && <button className="btn" onClick={() => setGallery(true)}><Plus size={16} aria-hidden /> Add widget</button>}
          <button className={`btn ${editing ? 'primary' : ''}`} onClick={() => setEditing((e) => !e)} aria-pressed={editing}>
            {editing ? 'Done' : <><LayoutGrid size={16} aria-hidden /> Customize</>}
          </button>
        </div>
      </div>

      {!live.loading && live.hosts.length === 0 && (
        <section className="card" style={{ marginBottom: 'var(--s-5)', borderColor: 'var(--accent)' }} aria-label="Connect your first server">
          <h2>Welcome. Let’s connect your first server.</h2>
          <p style={{ color: 'var(--ink-2)', maxWidth: '62ch', marginTop: 'var(--s-1)' }}>It takes one command on a Linux server. Your dashboard fills with live numbers within a minute, and you can turn on the AI agent whenever you like.</p>
          <AddServer hosts={live.hosts} admin={isAdmin()} />
        </section>
      )}

      <div className="row" style={{ marginBottom: 'var(--s-4)', justifyContent: 'space-between' }}>
        <div className="views" role="group" aria-label="Dashboard views">
          {dash.views.map((v) => <button key={v.id} aria-pressed={v.id === view.id} onClick={() => setDash((d) => ({ ...d, activeId: v.id }))}>{v.name}</button>)}
          {editing && dash.views.length < 12 && <button onClick={() => setDialog('new')} aria-label="New view"><Plus size={14} aria-hidden /> New view</button>}
        </div>
        {editing && (
          <div className="tools">
            <button className="btn sm" onClick={() => setDialog('rename')}><Pencil size={14} aria-hidden /> Rename</button>
            <button className="btn sm" onClick={() => setDialog('export')}><Download size={14} aria-hidden /> Export</button>
            <button className="btn sm" onClick={() => setDialog('import')}><Upload size={14} aria-hidden /> Import</button>
            <button className="btn sm" onClick={() => setDialog('reset')}><RotateCcw size={14} aria-hidden /> Reset</button>
            {dash.views.length > 1 && <button className="btn sm danger" onClick={removeView}><Trash2 size={14} aria-hidden /> Delete view</button>}
          </div>
        )}
      </div>

      {view.widgets.length === 0 && (
        <div className="card empty">
          <strong>This view is empty</strong>
          <p className="small">Press Customize, then Add widget to build it.</p>
        </div>
      )}

      <Grid
        className="layout" layouts={{ lg: layout }} breakpoints={{ lg: 900, sm: 0 }} cols={{ lg: COLS, sm: 1 }}
        rowHeight={56} margin={[16, 16]} containerPadding={[0, 0]}
        isDraggable={editing} isResizable={editing} draggableHandle=".widget-bar" draggableCancel="button,select,input,textarea,a"
        onBreakpointChange={(bp: string) => { breakpoint.current = bp }} onLayoutChange={onLayoutChange}
      >
        {view.widgets.map((x) => {
          const def = registry[x.type]
          const Body = def.Component
          return (
            <div key={x.id}>
              <section className={`widget ${editing ? 'editing' : ''}`} aria-label={x.config.title || def.autoTitle?.(x.config) || def.title}>
                <div className="widget-bar">
                  <h3>{x.config.title || def.autoTitle?.(x.config) || def.title}</h3>
                  {editing && (
                    <>
                      <button className="btn icon sm ghost" aria-label={`Settings for ${def.title}`} onClick={() => setConfigId(x.id)}><Settings2 size={16} /></button>
                      <button className="btn icon sm ghost" aria-label={`Remove ${def.title}`} onClick={() => patchView((v) => ({ ...v, widgets: v.widgets.filter((i) => i.id !== x.id) }))}><Trash2 size={16} /></button>
                    </>
                  )}
                </div>
                <div className="widget-body">
                  <Body config={x.config} setConfig={(c) => patchWidget(x.id, (i) => ({ ...i, config: c }))} editing={editing} />
                </div>
              </section>
            </div>
          )
        })}
      </Grid>

      {gallery && (
        <Drawer title="Add a widget" onClose={() => setGallery(false)}>
          <div className="gallery">
            {widgetList.map((d) => (
              <button key={d.type} className="card link" onClick={() => addWidget(d.type)}>
                <span className="glyph"><d.icon size={18} aria-hidden /></span>
                <span><strong>{d.title}</strong><br /><span className="small muted">{d.description}</span></span>
              </button>
            ))}
          </div>
        </Drawer>
      )}

      {configWidget && (
        <Modal title={`${registry[configWidget.type].title} settings`} onClose={() => setConfigId(null)}>
          <div className="stack">
            <label className="field">
              <span>Title</span>
              <input className="input" value={configWidget.config.title ?? ''} placeholder={registry[configWidget.type].title} maxLength={60}
                onChange={(e) => patchWidget(configWidget.id, (i) => ({ ...i, config: { ...i.config, title: e.target.value } }))} />
            </label>
            <ConfigForm fields={registry[configWidget.type].fields} config={configWidget.config}
              onChange={(c) => patchWidget(configWidget.id, (i) => ({ ...i, config: c }))} />
            <div className="row"><button className="btn primary" onClick={() => setConfigId(null)}>Done</button></div>
          </div>
        </Modal>
      )}

      {dialog === 'new' && <NameDialog title="New view" initial="" cta="Create" onClose={() => setDialog(null)}
        onSave={(name) => { const v: View = { id: uid(), name, widgets: [] }; setDash((d) => ({ activeId: v.id, views: [...d.views, v] })); setDialog(null) }} />}
      {dialog === 'rename' && <NameDialog title="Rename view" initial={view.name} cta="Save" onClose={() => setDialog(null)}
        onSave={(name) => { patchView((v) => ({ ...v, name })); setDialog(null) }} />}
      {dialog === 'export' && <ExportDialog text={exportView(view)} onClose={() => setDialog(null)} />}
      {dialog === 'import' && <ImportDialog onClose={() => setDialog(null)}
        onImport={(v) => { setDash((d) => ({ activeId: v.id, views: [...d.views, v].slice(0, 12) })); setDialog(null); toast({ tone: 'good', title: `Imported “${v.name}”` }) }} />}
      {dialog === 'reset' && (
        <Modal title="Reset dashboard?" onClose={() => setDialog(null)}>
          <p style={{ marginBottom: 'var(--s-4)' }}>This replaces all your views with the default Overview. Export first if you want to keep your current layout.</p>
          <div className="row">
            <button className="btn danger" onClick={() => { setDash(defaultDashboards()); setDialog(null) }}>Reset to default</button>
            <button className="btn" onClick={() => setDialog(null)}>Cancel</button>
          </div>
        </Modal>
      )}
    </div>
  )
}

function NameDialog({ title, initial, cta, onSave, onClose }: { title: string; initial: string; cta: string; onSave: (name: string) => void; onClose: () => void }) {
  const [name, setName] = useState(initial)
  return (
    <Modal title={title} onClose={onClose}>
      <form className="stack" onSubmit={(e) => { e.preventDefault(); if (name.trim()) onSave(name.trim().slice(0, 40)) }}>
        <label className="field"><span>Name</span><input className="input" value={name} maxLength={40} onChange={(e) => setName(e.target.value)} /></label>
        <div className="row"><button className="btn primary" type="submit" disabled={!name.trim()}>{cta}</button></div>
      </form>
    </Modal>
  )
}

function ExportDialog({ text, onClose }: { text: string; onClose: () => void }) {
  const toast = useToast()
  return (
    <Modal title="Export this view" onClose={onClose}>
      <p className="small muted" style={{ marginBottom: 'var(--s-3)' }}>Share this JSON with a teammate or keep it as a backup. It contains layout and widget settings only, never keys or data.</p>
      <textarea className="input mono" readOnly rows={10} value={text} onFocus={(e) => e.currentTarget.select()} />
      <div className="row" style={{ marginTop: 'var(--s-3)' }}>
        <button className="btn primary" onClick={() => navigator.clipboard.writeText(text).then(() => toast({ tone: 'good', title: 'Copied' }), () => toast({ tone: 'warn', title: 'Could not copy', detail: 'Select the text and copy it by hand.' }))}><Copy size={16} aria-hidden /> Copy</button>
      </div>
    </Modal>
  )
}

function ImportDialog({ onImport, onClose }: { onImport: (v: View) => void; onClose: () => void }) {
  const [text, setText] = useState('')
  const [error, setError] = useState<string | null>(null)
  return (
    <Modal title="Import a view" onClose={onClose}>
      <form className="stack" onSubmit={(e) => { e.preventDefault(); const r = importView(text, isKnownWidget); typeof r === 'string' ? setError(r) : onImport(r) }}>
        <label className="field"><span>Exported JSON</span><textarea className="input mono" rows={10} value={text} onChange={(e) => setText(e.target.value)} spellCheck={false} /></label>
        {error && <p className="err" role="alert">{error}</p>}
        <div className="row"><button className="btn primary" type="submit" disabled={!text.trim()}>Import</button></div>
      </form>
    </Modal>
  )
}
