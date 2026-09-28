import { useEffect, useRef } from 'react'

const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)

// Shown in Settings. Keep in sync with the bindings in the views.
export const SHORTCUTS = [
  { keys: ['/'], alt: [isMac ? '⌘ F' : 'Ctrl F'], action: 'Focus the filter' },
  { keys: ['↑', '↓'], alt: ['k', 'j'], action: 'Select previous / next exchange' },
  { keys: ['Home', 'End'], action: 'Jump to first / last exchange' },
  { keys: ['Esc'], action: 'Clear filter focus, then selection' },
  { keys: ['1', '2', '3', '4'], action: 'Inspector: Overview, Request, Response, Timing' },
  { keys: ['Delete'], action: 'Delete the selected exchange' },
  { keys: [isMac ? '⌘ ⇧ K' : 'Ctrl Shift K'], action: 'Clear all traffic' },
  { keys: [isMac ? '⌘ ⇧ P' : 'Ctrl Shift P'], action: 'Start or stop the proxy' },
]

export function isTyping(target) {
  if (!target) return false
  const tag = target.tagName
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable
}

// "mod+shift+k", "/", "arrowdown". mod is Cmd on macOS, Ctrl elsewhere.
function matches(e, combo) {
  const parts = combo.toLowerCase().split('+')
  const key = parts.pop()
  const mods = new Set(parts)
  if (mods.has('mod') !== (isMac ? e.metaKey : e.ctrlKey)) return false
  if (mods.has('alt') !== e.altKey) return false
  // A printable key already encodes shift ("?" vs "/"), so only check shift
  // when the combo names it or the key is a named one like ArrowDown.
  if ((mods.has('shift') || key.length > 1) && mods.has('shift') !== e.shiftKey) return false
  return e.key.toLowerCase() === key
}

// bindings: [{ combo, run, inInputs? }]. The first match wins and the event
// is swallowed. Bindings run outside text fields unless inInputs is set.
export function useHotkeys(bindings) {
  const ref = useRef(bindings)
  ref.current = bindings
  useEffect(() => {
    const onKey = (e) => {
      if (e.defaultPrevented || e.isComposing) return
      const typing = isTyping(e.target)
      for (const b of ref.current) {
        if (typing && !b.inInputs) continue
        if (matches(e, b.combo)) {
          e.preventDefault()
          b.run(e)
          return
        }
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])
}
