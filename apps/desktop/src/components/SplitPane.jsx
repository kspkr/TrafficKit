import { useCallback, useRef, useState } from 'react'

const KEY = 'tk.inspectorWidth'
const MIN_SIDE = 320

function savedWidth(fallback) {
  try {
    const n = Number(localStorage.getItem(KEY))
    return n > 0 ? n : fallback
  } catch {
    return fallback
  }
}

// Left pane takes the remaining space; the right pane has a draggable width.
export function SplitPane({ left, right }) {
  const container = useRef(null)
  const [width, setWidth] = useState(() => savedWidth(520))

  const onPointerDown = useCallback((e) => {
    e.preventDefault()
    const el = container.current
    const handle = e.currentTarget
    handle.setPointerCapture(e.pointerId)
    const move = (ev) => {
      const rect = el.getBoundingClientRect()
      const w = Math.round(rect.right - ev.clientX)
      setWidth(Math.max(MIN_SIDE, Math.min(w, rect.width - MIN_SIDE)))
    }
    const up = () => {
      handle.removeEventListener('pointermove', move)
      handle.removeEventListener('pointerup', up)
      setWidth((w) => {
        try {
          localStorage.setItem(KEY, String(w))
        } catch {
          // not persisted; fine
        }
        return w
      })
    }
    handle.addEventListener('pointermove', move)
    handle.addEventListener('pointerup', up)
  }, [])

  return (
    <div ref={container} className="flex h-full min-h-0 min-w-0">
      <div className="min-w-0 flex-1">{left}</div>
      <div
        role="separator"
        aria-orientation="vertical"
        onPointerDown={onPointerDown}
        className="w-px shrink-0 cursor-col-resize bg-line after:absolute after:-mx-1 after:h-full after:w-2 relative hover:bg-select-line"
      />
      <div className="min-w-0 shrink-0" style={{ width, maxWidth: `calc(100% - ${MIN_SIDE}px)` }}>
        {right}
      </div>
    </div>
  )
}
