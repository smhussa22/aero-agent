import type { ComponentType, FC } from 'react'
import type { LucideIcon } from 'lucide-react'
import { Activity, BellRing, Gauge, HardDrive, History, MessagesSquare, NotebookPen, Server, ShieldCheck, Siren } from 'lucide-react'
import { metricName, splitKey, type Unit } from '../lib/format'
import { StatWidget } from './StatWidget'
import { ChartWidget } from './ChartWidget'
import { MetersWidget, HostsWidget, IncidentsWidget, ApprovalsWidget, AlertsWidget, AuditWidget } from './feeds'
import { ChatWidget } from './ChatWidget'
import { NotesWidget } from './NotesWidget'

// A widget is one entry here. To add your own: write a component that takes WidgetProps,
// describe its config fields, and add a line to `list`. The gallery, config form, layout
// saving, import/export and validation all pick it up with no other changes.

export type FieldDef = {
  key: string
  label: string
  kind: 'host' | 'metric' | 'range' | 'text' | 'number' | 'textarea'
  hint?: string
  sameUnitAs?: string // metric field: only offer metrics in the unit of that other field
}

export type WidgetProps = { config: Record<string, string>; setConfig: (c: Record<string, string>) => void; editing: boolean }

export type WidgetDef = {
  type: string
  title: string
  description: string
  icon: LucideIcon
  size: { w: number; h: number; minW?: number; minH?: number }
  fields: FieldDef[]
  defaults?: Record<string, string>
  autoTitle?: (config: Record<string, string>) => string // shown when the user has not set a title
  Component: FC<WidgetProps> | ComponentType<WidgetProps>
}

export const RANGES = [
  { value: '900', label: 'Last 15 minutes' }, { value: '3600', label: 'Last hour' },
  { value: '21600', label: 'Last 6 hours' }, { value: '86400', label: 'Last 24 hours' },
]

const host: FieldDef = { key: 'host', label: 'Host', kind: 'host', hint: 'Leave on "first host" to follow whichever host is listed first.' }

const list: WidgetDef[] = [
  { type: 'stat', title: 'Stat tile', description: 'One number for one host, with a meter, a status and a trend line.', icon: Gauge,
    size: { w: 3, h: 2, minW: 2, minH: 2 }, fields: [host, { key: 'metric', label: 'Metric', kind: 'metric' },
      { key: 'warn', label: 'Warn at', kind: 'number', hint: 'Percent metrics only. Default 80.' }, { key: 'crit', label: 'Critical at', kind: 'number', hint: 'Default 90.' }],
    defaults: { metric: 'cpu.usage_percent|' }, autoTitle: (c) => { const k = splitKey(c.metric ?? ''); return metricName(k.metric, k.label) }, Component: StatWidget },
  { type: 'chart', title: 'Chart', description: 'Up to three metrics over time on one axis, with hover readout and a table view.', icon: Activity,
    size: { w: 6, h: 5, minW: 3, minH: 4 }, fields: [host, { key: 'range', label: 'Time range', kind: 'range' },
      { key: 'm1', label: 'Metric 1', kind: 'metric' }, { key: 'm2', label: 'Metric 2', kind: 'metric', sameUnitAs: 'm1', hint: 'Must use the same unit as metric 1: one chart, one axis.' },
      { key: 'm3', label: 'Metric 3', kind: 'metric', sameUnitAs: 'm1' }],
    defaults: { range: '3600', m1: 'cpu.usage_percent|', m2: 'memory.used_percent|' },
    autoTitle: (c) => [c.m1, c.m2, c.m3].filter(Boolean).map((k) => { const s = splitKey(k as string); return metricName(s.metric, s.label) }).join(' and ') || 'Chart', Component: ChartWidget },
  { type: 'meters', title: 'Usage meters', description: 'Memory and every disk as aligned meters, each against its own limit.', icon: HardDrive,
    size: { w: 4, h: 4, minW: 3, minH: 3 }, fields: [host], Component: MetersWidget },
  { type: 'hosts', title: 'Hosts', description: 'Every monitored server, its permission mode and when it last reported.', icon: Server,
    size: { w: 4, h: 4, minW: 3, minH: 3 }, fields: [], Component: HostsWidget },
  { type: 'incidents', title: 'Incidents', description: 'Recent incidents with the AI agent’s plain-English cause.', icon: Siren,
    size: { w: 6, h: 5, minW: 3, minH: 3 }, fields: [], Component: IncidentsWidget },
  { type: 'approvals', title: 'Approvals', description: 'Fixes the AI agent proposed and is waiting on you to approve or reject.', icon: ShieldCheck,
    size: { w: 6, h: 5, minW: 3, minH: 3 }, fields: [], Component: ApprovalsWidget },
  { type: 'alerts', title: 'Alert feed', description: 'What needs your attention: approvals, escalations, AI findings and silent hosts.', icon: BellRing,
    size: { w: 4, h: 5, minW: 3, minH: 3 }, fields: [], Component: AlertsWidget },
  { type: 'audit', title: 'Audit log', description: 'The append-only record of what the system and people did.', icon: History,
    size: { w: 6, h: 5, minW: 3, minH: 3 }, fields: [], Component: AuditWidget },
  { type: 'chat', title: 'AI assistant', description: 'Chat with your own AI about your servers. Uses your key, from this browser.', icon: MessagesSquare,
    size: { w: 6, h: 6, minW: 3, minH: 4 }, fields: [], Component: ChatWidget },
  { type: 'notes', title: 'Notes', description: 'A scratchpad for runbook links, on-call details or anything else.', icon: NotebookPen,
    size: { w: 4, h: 3, minW: 2, minH: 2 }, fields: [{ key: 'text', label: 'Text', kind: 'textarea' }], Component: NotesWidget },
]

export const registry: Record<string, WidgetDef> = Object.fromEntries(list.map((w) => [w.type, w]))
export const widgetList = list
export const isKnownWidget = (type: string) => Object.prototype.hasOwnProperty.call(registry, type)
export type { Unit }
