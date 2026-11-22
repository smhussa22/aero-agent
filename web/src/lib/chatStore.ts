import { useSyncExternalStore } from 'react'
import { SYSTEM_PROMPT, snapshot, streamChat, type ChatMessage } from './chat'
import { aiKey, getSettings } from './settings'

// The conversation lives in memory only: reloading the page clears it, and nothing is sent to the Aero server.
type State = { messages: ChatMessage[]; busy: boolean; error: string | null; draft: string; includeLive: boolean }

let state: State = { messages: [], busy: false, error: null, draft: '', includeLive: true }
const listeners = new Set<() => void>()
const set = (patch: Partial<State>) => { state = { ...state, ...patch }; listeners.forEach((l) => l()) }
let abort: AbortController | null = null

export const useChat = () => useSyncExternalStore((l) => { listeners.add(l); return () => listeners.delete(l) }, () => state)

export const chat = {
  setDraft: (draft: string) => set({ draft }),
  setIncludeLive: (includeLive: boolean) => set({ includeLive }),
  clear: () => { abort?.abort(); set({ messages: [], busy: false, error: null }) },
  stop: () => abort?.abort(),

  async send(text: string) {
    const content = text.trim()
    if (!content || state.busy) return
    const ai = getSettings().ai
    const history: ChatMessage[] = [...state.messages, { role: 'user', content }]
    set({ messages: [...history, { role: 'assistant', content: '' }], busy: true, error: null, draft: '' })
    abort = new AbortController()

    try {
      let system = SYSTEM_PROMPT
      if (state.includeLive) system += '\n\n' + (await snapshot().catch(() => 'Dashboard data is unavailable right now.'))
      let reply = ''
      for await (const piece of streamChat({ provider: ai.provider, model: ai.model, baseUrl: ai.baseUrl, key: aiKey.get() }, system, history, abort.signal)) {
        reply += piece
        set({ messages: [...history, { role: 'assistant', content: reply }] })
      }
      if (!reply) throw new Error('The provider sent an empty reply.')
    } catch (e) {
      const stopped = e instanceof DOMException && e.name === 'AbortError'
      // keep whatever streamed before the stop; drop an empty placeholder
      const kept = state.messages.filter((m, i) => !(i === state.messages.length - 1 && m.role === 'assistant' && m.content === ''))
      set({ messages: kept, error: stopped ? null : e instanceof Error ? e.message : 'Something went wrong.' })
    } finally {
      set({ busy: false })
    }
  },
}
