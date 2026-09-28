import { create } from 'zustand'

// Summaries only; headers and bodies are fetched per selection. `rows` is
// mutated in place and `version` bumps on every change. Copying a Map of
// 50k entries on every batch would cost more than the rendering it feeds.
export const useTraffic = create((set, get) => ({
  rows: new Map(),
  version: 0,
  selectedId: null,

  // Upserts summaries, ignoring any that are older than what we have.
  apply(summaries) {
    const { rows } = get()
    let changed = false
    for (const s of summaries) {
      const cur = rows.get(s.id)
      if (!cur || cur.rev < s.rev) {
        rows.set(s.id, s)
        changed = true
      }
    }
    if (changed) set({ version: get().version + 1 })
  },

  // Replaces everything with a fresh list from the engine, keeping rows the
  // event stream already delivered at a newer revision.
  replace(list) {
    const old = get().rows
    const rows = new Map()
    for (const s of list) {
      const cur = old.get(s.id)
      rows.set(s.id, cur && cur.rev > s.rev ? cur : s)
    }
    const { selectedId } = get()
    set({
      rows,
      version: get().version + 1,
      selectedId: rows.has(selectedId) ? selectedId : null,
    })
  },

  remove(ids) {
    const { rows, selectedId } = get()
    for (const id of ids) rows.delete(id)
    set({
      version: get().version + 1,
      selectedId: ids.includes(selectedId) ? null : selectedId,
    })
  },

  clear() {
    set({ rows: new Map(), version: get().version + 1, selectedId: null })
  },

  select(id) {
    set({ selectedId: id })
  },
}))
