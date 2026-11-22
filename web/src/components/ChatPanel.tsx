import { useEffect, useRef, useState } from 'react'
import { KeyRound, SendHorizontal, Square, Trash2 } from 'lucide-react'
import { validateAi } from '../lib/chat'
import { chat, useChat } from '../lib/chatStore'
import { aiKey, updateSettings, useAiKey, useSettings, type Provider } from '../lib/settings'
import { ErrorLine } from './ui'

const PLACEHOLDER: Record<Provider, string> = { anthropic: 'e.g. claude-sonnet-5-5', openai: 'e.g. gpt-4o-mini', local: 'e.g. llama3.1' }

// Where the user drops in their own key. It is stored in this browser only; nothing here is sent to Aero.
export function AiSetup({ onSaved }: { onSaved?: () => void }) {
  const s = useSettings()
  const storedKey = useAiKey()
  const [provider, setProvider] = useState<Provider>(s.ai.provider)
  const [model, setModel] = useState(s.ai.model)
  const [baseUrl, setBaseUrl] = useState(s.ai.baseUrl)
  const [key, setKey] = useState('')
  const [remember, setRemember] = useState(s.ai.remember)
  const [error, setError] = useState<string | null>(null)
  const [saved, setSaved] = useState(false)

  const save = (e: React.FormEvent) => {
    e.preventDefault()
    const effectiveKey = key.trim() || storedKey
    const bad = validateAi({ provider, model, baseUrl, key: effectiveKey })
    if (bad) { setError(bad); setSaved(false); return }
    setError(null)
    updateSettings((cur) => ({ ...cur, ai: { provider, model: model.trim(), baseUrl: baseUrl.trim(), remember } }))
    aiKey.set(effectiveKey, remember)
    setKey(''); setSaved(true)
    onSaved?.()
  }

  return (
    <form className="stack" onSubmit={save} autoComplete="off">
      <label className="field">
        <span>Provider</span>
        <select className="input" value={provider} onChange={(e) => setProvider(e.target.value as Provider)}>
          <option value="anthropic">Anthropic (Claude)</option>
          <option value="openai">OpenAI</option>
          <option value="local">Local or other OpenAI-compatible model</option>
        </select>
      </label>
      <label className="field">
        <span>Model</span>
        <input className="input" value={model} onChange={(e) => setModel(e.target.value)} placeholder={PLACEHOLDER[provider]} spellCheck={false} />
      </label>
      {(provider === 'local' || baseUrl) && (
        <label className="field">
          <span>Base URL{provider === 'local' ? '' : ' (optional)'}</span>
          <input className="input" value={baseUrl} onChange={(e) => setBaseUrl(e.target.value)} placeholder="http://localhost:11434/v1" spellCheck={false} />
          <span className="hint">Local models usually need no key. A hosted provider must use https.</span>
        </label>
      )}
      {provider !== 'local' && !baseUrl && (
        <button type="button" className="btn sm ghost" style={{ alignSelf: 'flex-start' }} onClick={() => setBaseUrl(provider === 'anthropic' ? 'https://api.anthropic.com' : 'https://api.openai.com/v1')}>Use a custom base URL</button>
      )}
      <label className="field">
        <span>API key{storedKey ? ' (saved, leave blank to keep it)' : ''}</span>
        <input className="input" type="password" value={key} onChange={(e) => setKey(e.target.value)} autoComplete="off" spellCheck={false}
          placeholder={storedKey ? '••••••••••••' : provider === 'local' ? 'Not needed for most local models' : 'Paste your key'} />
      </label>
      <label className="toggle">
        <input type="checkbox" checked={remember} onChange={(e) => setRemember(e.target.checked)} />
        Remember on this device
      </label>
      <p className="hint">
        Your key stays in this browser ({remember ? 'saved on this device until you forget it' : 'cleared when you close the tab'}) and goes only to your AI provider. Aero’s server never receives it, and it is not part of exported layouts.
      </p>
      <ErrorLine error={error} />
      {saved && !error && <p className="hint" role="status">Saved.</p>}
      <div className="row">
        <button className="btn primary" type="submit"><KeyRound size={16} aria-hidden /> Save</button>
        {storedKey && <button type="button" className="btn" onClick={() => { aiKey.clear(); setSaved(false) }}>Forget key</button>}
      </div>
    </form>
  )
}

const QUICK = ['What needs my attention right now?', 'Summarize the open incidents', 'Is anything about to run out of disk or memory?']

export function ChatPanel({ compact = false }: { compact?: boolean }) {
  const s = useSettings()
  const key = useAiKey()
  const { messages, busy, error, draft, includeLive } = useChat()
  const log = useRef<HTMLDivElement>(null)
  const configured = validateAi({ provider: s.ai.provider, model: s.ai.model, baseUrl: s.ai.baseUrl, key }) === null

  useEffect(() => { log.current?.scrollTo({ top: log.current.scrollHeight }) }, [messages])

  if (!configured) {
    return (
      <div>
        <p className="small" style={{ marginBottom: 'var(--s-3)', color: 'var(--ink-2)' }}>
          Chat with your own AI about your servers. Add a provider and key to start; they stay in this browser.
        </p>
        <AiSetup />
      </div>
    )
  }

  return (
    <div className="chat">
      <div className="log chat-log" ref={log} role="log" aria-live="polite" aria-label="Conversation">
        {messages.length === 0 && (
          <div className="chips" style={{ flexDirection: compact ? 'column' : 'row', alignItems: 'flex-start' }}>
            {QUICK.map((qs) => <button key={qs} onClick={() => chat.send(qs)}>{qs}</button>)}
          </div>
        )}
        {messages.map((m, i) => (
          <div key={i} className={`msg ${m.role}`}>
            <span className="sr">{m.role === 'user' ? 'You: ' : 'Assistant: '}</span>
            {m.content || (busy ? 'Thinking…' : '')}
          </div>
        ))}
      </div>
      <ErrorLine error={error} />
      <form className="chat-form" onSubmit={(e) => { e.preventDefault(); chat.send(draft) }}>
        <input className="input" aria-label="Message" placeholder="Ask about your servers…" value={draft} maxLength={4000} onChange={(e) => chat.setDraft(e.target.value)} />
        {busy
          ? <button type="button" className="btn icon" onClick={chat.stop} aria-label="Stop"><Square size={16} /></button>
          : <button type="submit" className="btn primary icon" aria-label="Send" disabled={!draft.trim()}><SendHorizontal size={18} /></button>}
      </form>
      <div className="row" style={{ marginTop: 'var(--s-2)', justifyContent: 'space-between' }}>
        <label className="toggle" style={{ minHeight: 36 }}>
          <input type="checkbox" checked={includeLive} onChange={(e) => chat.setIncludeLive(e.target.checked)} />
          <span className="small">Share live dashboard data</span>
        </label>
        {messages.length > 0 && <button className="btn sm ghost" onClick={chat.clear}><Trash2 size={14} aria-hidden /> Clear</button>}
      </div>
    </div>
  )
}
