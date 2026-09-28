import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { engine } from '../../engine/client.js'
import { decodeText, detectMode, hexDump, jsonTokens, prettyJson, stripXssi } from '../../lib/body.js'
import { formatBytes } from '../../lib/format.js'
import { CopyIcon } from '../icons.jsx'
import { Segmented, useCopy } from '../ui.jsx'
import { CodeView } from './CodeView.jsx'
import { Empty } from './tables.jsx'

// Rendering a multi-megabyte body stalls the webview; show a prefix first.
const PREVIEW_CHARS = 256 * 1024
const HIGHLIGHT_LIMIT = 200 * 1024

export function useBody(id, side, rev, enabled) {
  return useQuery({
    queryKey: ['body', id, side, rev],
    queryFn: ({ signal }) => engine.body(id, side, signal),
    enabled,
    staleTime: Infinity,
  })
}

function Notice({ tone = 'muted', children }) {
  const color = tone === 'warn' ? 'text-client-err' : 'text-muted'
  return <div className={`border-b border-line bg-raised/60 px-4 py-1.5 text-[12px] ${color}`}>{children}</div>
}

function render(bytes, mode) {
  if (mode === 'hex') return { mode, text: hexDump(bytes) }
  const raw = decodeText(bytes)
  if (mode === 'json') {
    const { text, prefix } = stripXssi(raw)
    const pretty = prettyJson(text)
    if (pretty != null) {
      return { mode, text: pretty, tokens: pretty.length <= HIGHLIGHT_LIMIT ? jsonTokens(pretty) : null, prefix }
    }
    return { mode: 'text', text: raw, jsonFailed: true }
  }
  return { mode, text: raw }
}

export function BodyView({ exchangeId, rev, side, body, contentType, inFlight }) {
  const hasData = body.captured > 0
  const q = useBody(exchangeId, side, rev, hasData)
  const [modeOverride, setMode] = useState(null)
  const [showAll, setShowAll] = useState(false)
  const [copied, copy] = useCopy()

  const auto = useMemo(() => (q.data ? detectMode(contentType, q.data.bytes) : null), [q.data, contentType])
  const view = useMemo(() => (q.data ? render(q.data.bytes, modeOverride ?? auto) : null), [q.data, modeOverride, auto])

  if (body.size === 0 && !inFlight) return <Empty>No body.</Empty>
  if (!hasData) {
    if (inFlight) return <Empty>Still transferring. The body appears here once it completes.</Empty>
    return <Empty>{formatBytes(body.size)} transferred, none captured (capture limit is 0).</Empty>
  }
  if (q.isPending) return <Empty>Loading…</Empty>
  if (q.error) return <Empty>Couldn't load the body: {q.error.message}</Empty>

  const { truncated, decoded, decodeError, bytes } = q.data
  const clipped = !showAll && view.text.length > PREVIEW_CHARS
  const shown = clipped ? view.text.slice(0, PREVIEW_CHARS) : view.text

  return (
    <div className="flex min-h-full flex-col">
      <div className="flex items-center gap-3 border-b border-line px-4 py-2 text-[11.5px] text-faint">
        <Segmented
          value={view.mode}
          onChange={(m) => setMode(m === auto ? null : m)}
          options={[
            { id: 'json', label: 'JSON' },
            { id: 'text', label: 'Text' },
            { id: 'hex', label: 'Hex' },
          ]}
        />
        <span className="tabular min-w-0 truncate">
          {formatBytes(bytes.length)}
          {decoded && ` · ${decoded} ${formatBytes(body.captured)}`}
          {contentType && ` · ${contentType.split(';')[0]}`}
        </span>
        <button
          type="button"
          onClick={() => copy(view.mode === 'hex' ? view.text : decodeText(bytes))}
          className="ml-auto flex shrink-0 items-center gap-1 rounded px-1.5 py-0.5 text-muted hover:bg-hover hover:text-text"
        >
          <CopyIcon size={13} />
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      {truncated && (
        <Notice tone="warn">
          Only the first {formatBytes(body.captured)} of {formatBytes(body.size)} was captured. Raise
          capture.maxBodyBytes in the config file to keep more.
        </Notice>
      )}
      {decodeError && <Notice tone="warn">Showing raw bytes: {decodeError}</Notice>}
      {view.prefix && <Notice>Removed the anti-hijacking prefix {view.prefix} before formatting.</Notice>}
      {view.jsonFailed && <Notice>Not valid JSON, shown as text.</Notice>}
      {view.mode === 'hex' && bytes.length > 64 * 1024 && <Notice>Hex view shows the first 64 KB.</Notice>}
      <div className="flex-1">
        <CodeView text={shown} tokens={clipped ? null : view.tokens} />
      </div>
      {clipped && (
        <div className="border-t border-line px-4 py-2 text-[12px] text-muted">
          Showing {formatBytes(PREVIEW_CHARS)} of {formatBytes(view.text.length)}.{' '}
          <button type="button" className="text-accent hover:underline" onClick={() => setShowAll(true)}>
            Show everything
          </button>
        </div>
      )}
    </div>
  )
}
