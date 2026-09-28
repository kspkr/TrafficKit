import { useMemo, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useVirtualizer } from '@tanstack/react-virtual'
import { engine } from '../../engine/client.js'
import { hexDump, jsonTokens, prettyJson } from '../../lib/body.js'
import { formatBytes, formatClock } from '../../lib/format.js'
import { CopyIcon, SearchIcon } from '../icons.jsx'
import { Segmented, useCopy } from '../ui.jsx'
import { CodeView } from './CodeView.jsx'
import { Empty } from './tables.jsx'

const ROW = 28

function Arrow({ dir }) {
  const sent = dir === 'send'
  return (
    <svg width="12" height="12" viewBox="0 0 12 12" aria-label={sent ? 'Sent' : 'Received'} className="shrink-0">
      <path
        d={sent ? 'M6 10V2M2.5 5.5L6 2l3.5 3.5' : 'M6 2v8M2.5 6.5L6 10l3.5-3.5'}
        fill="none"
        stroke={sent ? 'var(--accent)' : 'var(--m-get)'}
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

function bytesOf(m) {
  if (m.text != null) return new TextEncoder().encode(m.text)
  if (!m.base64) return new Uint8Array()
  const bin = atob(m.base64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out
}

function preview(m) {
  if (m.type === 'close') return `close ${m.closeCode || ''} ${m.text ?? ''}`.trim()
  if (m.text != null) return m.text.replace(/\s+/g, ' ').slice(0, 300)
  if (m.note) return m.note
  return `${formatBytes(m.size)} binary`
}

function Detail({ m }) {
  const [copied, copy] = useCopy()
  const view = useMemo(() => {
    if (m.text == null) return { text: hexDump(bytesOf(m)) }
    const pretty = prettyJson(m.text)
    return pretty != null ? { text: pretty, tokens: jsonTokens(pretty) } : { text: m.text }
  }, [m])
  return (
    <div className="flex min-h-0 flex-col border-t border-line">
      <div className="flex items-center gap-3 border-b border-line bg-raised/40 px-4 py-2 text-[11.5px] text-muted">
        <Arrow dir={m.dir} />
        <span className="font-medium text-text">{m.dir === 'send' ? 'Sent' : 'Received'}</span>
        <span className="tabular">
          #{m.seq} · {m.type} · {formatBytes(m.size)}
          {m.compressed && ' · compressed'} · {formatClock(m.time)}
        </span>
        <button
          type="button"
          onClick={() => copy(m.text ?? view.text)}
          className="ml-auto flex items-center gap-1 rounded px-1.5 py-0.5 hover:bg-hover hover:text-text"
        >
          <CopyIcon size={13} />
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      {(m.note || m.truncated) && (
        <div className="border-b border-line px-4 py-1.5 text-[12px] text-client-err">
          {m.note || 'Only the start of this message was kept.'}
        </div>
      )}
      <div className="min-h-0 flex-1 overflow-auto">
        <CodeView text={view.text} tokens={view.tokens} />
      </div>
    </div>
  )
}

// Live list of a WebSocket connection's messages. `count` comes from the
// exchange summary and changes as messages arrive, which refetches.
export function MessagesView({ exchangeId, count }) {
  const [dir, setDir] = useState('all')
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState(null)
  const scrollRef = useRef(null)

  const q = useQuery({
    queryKey: ['messages', exchangeId, count],
    queryFn: ({ signal }) => engine.messages(exchangeId, { limit: 5000 }, signal),
    placeholderData: (prev) => prev,
    staleTime: Infinity,
  })

  const items = useMemo(() => {
    const all = q.data?.items ?? []
    const needle = search.trim().toLowerCase()
    return all.filter(
      (m) => (dir === 'all' || m.dir === dir) && (!needle || (m.text ?? '').toLowerCase().includes(needle)),
    )
  }, [q.data, dir, search])

  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => ROW,
    overscan: 12,
  })

  if (q.isPending) return <Empty>Loading messages…</Empty>
  if (q.error) return <Empty>Couldn't load messages: {q.error.message}</Empty>

  const current = items.find((m) => m.seq === selected)
  const total = q.data.items.length

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex items-center gap-2 border-b border-line px-3 py-2">
        <Segmented
          value={dir}
          onChange={setDir}
          options={[
            { id: 'all', label: 'All' },
            { id: 'send', label: 'Sent' },
            { id: 'receive', label: 'Received' },
          ]}
        />
        <label className="flex h-7 min-w-0 flex-1 items-center gap-1.5 rounded-md bg-bg px-2 ring-1 ring-line-strong ring-inset focus-within:ring-select-line">
          <SearchIcon size={13} className="shrink-0 text-faint" />
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search payloads"
            className="min-w-0 flex-1 bg-transparent text-[12px] outline-none placeholder:text-faint"
            aria-label="Search messages"
          />
        </label>
        <span className="tabular shrink-0 text-[11.5px] text-faint">
          {items.length === total ? total : `${items.length} of ${total}`}
        </span>
      </div>
      {q.data.dropped > 0 && (
        <div className="border-b border-line px-4 py-1.5 text-[12px] text-muted">
          {q.data.dropped} older messages were dropped; the latest {total} are kept.
        </div>
      )}

      {total === 0 ? (
        <Empty>No messages yet. They appear here as the connection sends and receives them.</Empty>
      ) : (
        <div ref={scrollRef} className={`min-h-0 overflow-y-auto ${current ? 'h-[45%] shrink-0' : 'flex-1'}`}>
          <div style={{ height: virtualizer.getTotalSize() }} className="relative">
            {virtualizer.getVirtualItems().map((v) => {
              const m = items[v.index]
              const active = m.seq === selected
              return (
                <button
                  key={m.seq}
                  type="button"
                  onClick={() => setSelected(active ? null : m.seq)}
                  style={{ top: v.start, height: ROW }}
                  className={`absolute left-0 grid w-full grid-cols-[14px_84px_minmax(0,1fr)_64px] items-center gap-2 px-3 text-left text-[12px] ${
                    active ? 'bg-select' : 'hover:bg-hover'
                  }`}
                >
                  <Arrow dir={m.dir} />
                  <span className="tabular text-faint">{formatClock(m.time).slice(0, 12)}</span>
                  <span className={`truncate font-mono text-[11.5px] ${m.type === 'text' ? 'text-text' : 'text-muted'}`}>
                    {m.type !== 'text' && (
                      <span className="mr-2 rounded bg-raised px-1 py-px text-[10.5px] text-muted ring-1 ring-line-strong">
                        {m.type}
                      </span>
                    )}
                    {preview(m)}
                  </span>
                  <span className="tabular text-right text-faint">{formatBytes(m.size)}</span>
                </button>
              )
            })}
          </div>
        </div>
      )}
      {current && <Detail m={current} />}
    </div>
  )
}
