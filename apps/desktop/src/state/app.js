import { create } from 'zustand'

const THEMES = ['dark', 'light', 'system']

function savedTheme() {
  try {
    const t = localStorage.getItem('tk.theme')
    return THEMES.includes(t) ? t : 'dark'
  } catch {
    return 'dark'
  }
}

export function applyTheme(theme) {
  const root = document.documentElement
  if (theme === 'dark') delete root.dataset.theme
  else root.dataset.theme = theme
}

export const useApp = create((set) => ({
  // connecting | live | reconnecting | offline
  connection: 'connecting',
  connectionError: null,
  proxy: null,
  https: null,
  sources: [],
  clients: [],
  view: 'connect',
  theme: savedTheme(),

  // Inspector tabs stay put while moving between exchanges.
  inspectorTab: 'overview',
  requestTab: 'headers',
  responseTab: 'body',

  setConnection: (connection, connectionError = null) => set({ connection, connectionError }),
  setProxy: (proxy) => set({ proxy }),
  setHttps: (https) => set({ https }),
  setSources: (sources) => set({ sources: sources ?? [] }),
  setClients: (clients) => set({ clients: clients ?? [] }),
  setView: (view) => set({ view }),
  setInspectorTab: (inspectorTab) => set({ inspectorTab }),
  setRequestTab: (requestTab) => set({ requestTab }),
  setResponseTab: (responseTab) => set({ responseTab }),
  setTheme(theme) {
    if (!THEMES.includes(theme)) return
    try {
      localStorage.setItem('tk.theme', theme)
    } catch {
      // storage unavailable; the choice still applies until reload
    }
    applyTheme(theme)
    set({ theme })
  },
}))
