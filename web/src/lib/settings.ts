import { useSyncExternalStore } from 'react'

// The AI key never goes to the Aero server. It lives in this browser only: sessionStorage by default
// (gone when the tab closes), localStorage only when the user explicitly chooses "remember".

export type Provider = 'anthropic' | 'openai' | 'local'
export type Settings = {
  theme: 'system' | 'light' | 'dark'
  refreshSeconds: number
  notify: boolean // browser notifications for new alerts
  ai: { provider: Provider; model: string; baseUrl: string; remember: boolean }
}

const SETTINGS_KEY = 'aero.settings.v1'
const AI_KEY = 'aero.ai-key'

export const defaults: Settings = {
  theme: 'system', refreshSeconds: 10, notify: false,
  ai: { provider: 'anthropic', model: '', baseUrl: '', remember: false },
}

function load(): Settings {
  try {
    const s = JSON.parse(localStorage.getItem(SETTINGS_KEY) ?? 'null')
    if (s && typeof s === 'object') {
      return { ...defaults, ...s, ai: { ...defaults.ai, ...(s.ai ?? {}) } }
    }
  } catch { /* corrupt settings fall back to defaults */ }
  return defaults
}

let current = load()
const listeners = new Set<() => void>()
const emit = () => listeners.forEach((l) => l())

export function updateSettings(patch: Partial<Settings> | ((s: Settings) => Settings)) {
  current = typeof patch === 'function' ? patch(current) : { ...current, ...patch }
  try { localStorage.setItem(SETTINGS_KEY, JSON.stringify(current)) } catch { /* private mode */ }
  applyTheme(current.theme)
  emit()
}

export function useSettings(): Settings {
  return useSyncExternalStore((l) => { listeners.add(l); return () => listeners.delete(l) }, () => current)
}

export function getSettings() { return current }

export function applyTheme(t: Settings['theme']) {
  const root = document.documentElement
  if (t === 'system') root.removeAttribute('data-theme')
  else root.setAttribute('data-theme', t)
}

export const aiKey = {
  get: (): string => sessionStorage.getItem(AI_KEY) ?? localStorage.getItem(AI_KEY) ?? '',
  set: (key: string, remember: boolean) => {
    sessionStorage.removeItem(AI_KEY); localStorage.removeItem(AI_KEY)
    if (key) (remember ? localStorage : sessionStorage).setItem(AI_KEY, key)
    emit()
  },
  clear: () => { sessionStorage.removeItem(AI_KEY); localStorage.removeItem(AI_KEY); emit() },
}

export function useAiKey(): string {
  return useSyncExternalStore((l) => { listeners.add(l); return () => listeners.delete(l) }, aiKey.get)
}

applyTheme(current.theme)
