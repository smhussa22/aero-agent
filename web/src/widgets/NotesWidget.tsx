import type { WidgetProps } from './registry'

// Notes are plain text rendered as text (never as HTML), saved inside the layout itself.
export function NotesWidget({ config, setConfig, editing }: WidgetProps) {
  return (
    <textarea
      className="input" style={{ height: '100%', minHeight: 0 }} aria-label="Notes" placeholder="Runbook links, on-call details, anything…"
      value={config.text ?? ''} maxLength={4000} readOnly={editing}
      onChange={(e) => setConfig({ ...config, text: e.target.value })}
    />
  )
}
