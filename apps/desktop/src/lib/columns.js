import { compileFilter } from './filter.js'

// Table columns, in display order. Fixed columns have a pixel width; flexible
// ones share what's left and never go below `min`. When the table is too
// narrow, columns are dropped highest `drop` first instead of scrolling
// sideways. `sortValue` returns something comparable; in-flight values (-1
// durations, missing statuses) sort as smallest.
export const COLUMNS = [
  { key: 'status', label: 'Status', width: 64, sortValue: (r) => r.status ?? (r.error ? 999 : 0) },
  { key: 'method', label: 'Method', width: 76, sortValue: (r) => r.method },
  { key: 'host', label: 'Host', flex: 1, min: 140, sortValue: (r) => r.host },
  { key: 'path', label: 'Path', flex: 1.6, min: 150, sortValue: (r) => r.path },
  { key: 'type', label: 'Type', width: 96, drop: 2, sortValue: (r) => r.contentType ?? '' },
  { key: 'size', label: 'Size', width: 72, align: 'right', drop: 1, sortValue: (r) => r.respSize },
  { key: 'duration', label: 'Time', width: 72, align: 'right', drop: 1, sortValue: (r) => r.duration },
  { key: 'proto', label: 'Protocol', width: 76, drop: 3, sortValue: (r) => r.proto },
  { key: 'started', label: 'Started', width: 96, drop: 4, sortValue: (r) => r.id },
]

const PADDING = 24 // row padding, left + right

function minWidth(cols) {
  return cols.reduce((sum, c) => sum + (c.width ?? c.min), PADDING)
}

// Columns that fit in `width` pixels.
export function visibleColumns(width) {
  let cols = COLUMNS
  for (const level of [4, 3, 2, 1]) {
    if (minWidth(cols) <= width) break
    cols = cols.filter((c) => !c.drop || c.drop < level)
  }
  return cols
}

export function gridTemplate(cols) {
  return cols.map((c) => (c.flex ? `minmax(${c.min}px, ${c.flex}fr)` : `${c.width}px`)).join(' ')
}

const byKey = Object.fromEntries(COLUMNS.map((c) => [c.key, c]))

export function buildView(rows, filterText, sort) {
  const match = compileFilter(filterText)
  const out = []
  for (const r of rows.values()) {
    if (!match || match(r)) out.push(r)
  }
  const col = byKey[sort.key]
  const dir = sort.dir === 'desc' ? -1 : 1
  if (!col || col.key === 'started') {
    out.sort((a, b) => (a.id - b.id) * dir)
  } else {
    out.sort((a, b) => {
      const x = col.sortValue(a)
      const y = col.sortValue(b)
      if (x < y) return -dir
      if (x > y) return dir
      return a.id - b.id
    })
  }
  return out
}
