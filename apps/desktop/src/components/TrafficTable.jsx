import { memo, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { gridTemplate, visibleColumns } from '../lib/columns.js'
import { formatBytes, formatClock, formatDuration } from '../lib/format.js'
import { methodColor, statusClass } from '../lib/http.js'

const ROW_HEIGHT = 28
const HEADER_HEIGHT = 30

const STATUS_COLOR = {
  ok: 'text-ok',
  redirect: 'text-redirect',
  'client-err': 'text-client-err',
  'server-err': 'text-server-err',
  info: 'text-muted',
  none: 'text-faint',
}

function typeLabel(row) {
  if (row.kind === 'tunnel') return 'encrypted'
  if (row.upgraded) return 'upgraded'
  const t = row.contentType ?? ''
  return t.replace(/^application\/|^text\//, '').replace(/^x-/, '')
}

function Cell({ col, row }) {
  const inFlight = row.state === 'pending' || row.state === 'streaming'
  switch (col.key) {
    case 'status':
      if (row.error) {
        return (
          <span className="font-medium text-server-err" title={row.error}>
            failed
          </span>
        )
      }
      if (!row.status) return <span className="text-faint">···</span>
      return (
        <span className={`tabular font-medium ${STATUS_COLOR[statusClass(row.status)]}`} title={row.statusText}>
          {row.status}
        </span>
      )
    case 'method':
      return (
        <span className="text-[11.5px] font-semibold tracking-wide" style={{ color: methodColor(row.method) }}>
          {row.method}
        </span>
      )
    case 'host':
      return (
        <span className="text-text" title={row.host}>
          {row.host}
        </span>
      )
    case 'path':
      return (
        <span className="text-muted" title={row.path}>
          {row.kind === 'tunnel' ? '' : row.path}
        </span>
      )
    case 'type':
      return <span className="text-faint">{typeLabel(row)}</span>
    case 'size':
      return <span className="tabular text-muted">{inFlight && !row.respSize ? '' : formatBytes(row.respSize)}</span>
    case 'duration':
      return <span className="tabular text-muted">{inFlight ? '' : formatDuration(row.duration)}</span>
    case 'proto':
      return <span className="text-faint">{row.proto?.replace('HTTP/', 'HTTP ')}</span>
    case 'started':
      return <span className="tabular text-faint">{formatClock(row.started)}</span>
    default:
      return null
  }
}

const Row = memo(function Row({ row, cols, template, selected, onSelect, style }) {
  return (
    <div
      role="row"
      aria-selected={selected}
      onMouseDown={() => onSelect(row.id)}
      style={{ ...style, gridTemplateColumns: template }}
      className={`absolute left-0 grid w-full cursor-default items-center px-3 text-[12.5px] ${
        selected ? 'bg-select' : 'hover:bg-hover'
      }`}
    >
      {cols.map((c) => (
        <span key={c.key} className={`truncate pr-4 ${c.align === 'right' ? 'text-right' : ''}`}>
          <Cell col={c} row={row} />
        </span>
      ))}
    </div>
  )
})

function HeaderRow({ cols, template, sort, onSort }) {
  return (
    <div
      role="row"
      style={{ gridTemplateColumns: template, height: HEADER_HEIGHT }}
      className="sticky top-0 z-10 grid items-center border-b border-line bg-panel px-3 text-[11.5px] font-medium text-faint select-none"
    >
      {cols.map((c) => {
        const active = sort.key === c.key
        return (
          <button
            key={c.key}
            role="columnheader"
            type="button"
            onClick={() => onSort({ key: c.key, dir: active && sort.dir === 'asc' ? 'desc' : 'asc' })}
            className={`flex h-full items-center gap-1 truncate pr-4 hover:text-text ${
              c.align === 'right' ? 'justify-end' : ''
            } ${active ? 'text-text' : ''}`}
          >
            {c.label}
            {active && (
              <span aria-hidden="true" className="text-[10px] text-accent">
                {sort.dir === 'asc' ? '▲' : '▼'}
              </span>
            )}
          </button>
        )
      })}
    </div>
  )
}

function useWidth(ref) {
  const [width, setWidth] = useState(0)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    setWidth(el.offsetWidth)
    const ro = new ResizeObserver(([entry]) => setWidth(entry.contentRect.width))
    ro.observe(el)
    return () => ro.disconnect()
  }, [ref])
  return width
}

// Virtualized list of exchanges. Follows new rows while scrolled to the
// bottom, like a terminal; scrolling up pauses that. Columns that don't fit
// are hidden rather than scrolling the table sideways.
export function TrafficTable({ rows, selectedId, onSelect, sort, onSort }) {
  const scrollRef = useRef(null)
  const atBottom = useRef(true)
  const width = useWidth(scrollRef)
  const cols = visibleColumns(width || 2000)
  const template = gridTemplate(cols)

  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW_HEIGHT,
    scrollMargin: HEADER_HEIGHT, // rows start below the sticky header
    scrollPaddingStart: HEADER_HEIGHT, // and must not scroll underneath it
    overscan: 16,
  })

  const onScroll = () => {
    const el = scrollRef.current
    atBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < ROW_HEIGHT
  }

  useLayoutEffect(() => {
    if (atBottom.current && rows.length > 0 && sort.key === 'started' && sort.dir === 'asc') {
      virtualizer.scrollToIndex(rows.length - 1, { align: 'end' })
    }
  }, [rows.length, sort, virtualizer])

  const selectedIndex = selectedId == null ? -1 : rows.findIndex((r) => r.id === selectedId)
  useEffect(() => {
    if (selectedIndex >= 0) virtualizer.scrollToIndex(selectedIndex, { align: 'auto' })
  }, [selectedIndex, virtualizer])

  return (
    <div
      ref={scrollRef}
      onScroll={onScroll}
      role="grid"
      aria-rowcount={rows.length}
      className="h-full min-h-0 overflow-x-hidden overflow-y-auto bg-panel"
    >
      <HeaderRow cols={cols} template={template} sort={sort} onSort={onSort} />
      <div style={{ height: virtualizer.getTotalSize() }} className="relative">
        {virtualizer.getVirtualItems().map((v) => {
          const row = rows[v.index]
          return (
            <Row
              key={row.id}
              row={row}
              cols={cols}
              template={template}
              selected={row.id === selectedId}
              onSelect={onSelect}
              style={{ top: v.start - HEADER_HEIGHT, height: ROW_HEIGHT }}
            />
          )
        })}
      </div>
    </div>
  )
}
