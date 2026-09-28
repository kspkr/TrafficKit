import { useQuery } from '@tanstack/react-query'
import { engine } from '../../engine/client.js'
import { useApp } from '../../state/app.js'
import { useTraffic } from '../../state/traffic.js'
import { formatBytes, formatDuration } from '../../lib/format.js'
import {
  headerValue,
  headerValues,
  methodColor,
  parseCookieHeader,
  statusClass,
  parseQuery,
  rawMessage,
} from '../../lib/http.js'
import { decodeText, looksLikeText } from '../../lib/body.js'
import { AlertIcon, CopyIcon, LockIcon, TrashIcon } from '../icons.jsx'
import { Button, IconButton, SubTabs, Tabs, useCopy } from '../ui.jsx'
import { BodyView, useBody } from './BodyView.jsx'
import { MessagesView } from './MessagesView.jsx'
import { TimingView } from './TimingView.jsx'
import { Empty, KeyValueTable, SetCookieTable } from './tables.jsx'

export const INSPECTOR_TABS = ['overview', 'request', 'response', 'timing', 'messages']

export function Inspector({ id }) {
  const summary = useTraffic((s) => s.rows.get(id))
  const chosenTab = useApp((s) => s.inspectorTab)
  const setTab = useApp((s) => s.setInspectorTab)

  // Keyed by revision so an in-flight exchange refreshes as it progresses.
  const q = useQuery({
    queryKey: ['exchange', id, summary?.rev],
    queryFn: ({ signal }) => engine.exchange(id, signal),
    enabled: summary != null,
    staleTime: Infinity,
    placeholderData: (prev) => (prev?.id === id ? prev : undefined),
  })
  const x = q.data

  if (!summary) return null
  // Messages only exists for WebSocket connections.
  const tab = chosenTab === 'messages' && !summary.websocket ? 'overview' : chosenTab
  return (
    <section className="flex h-full min-h-0 flex-col bg-panel" aria-label="Exchange inspector">
      <Header summary={summary} />
      <Tabs
        value={tab}
        onChange={setTab}
        tabs={[
          { id: 'overview', label: 'Overview' },
          { id: 'request', label: 'Request' },
          { id: 'response', label: 'Response' },
          { id: 'timing', label: 'Timing' },
          ...(summary.websocket ? [{ id: 'messages', label: 'Messages', badge: summary.messages ?? 0 }] : []),
        ]}
      />
      {tab === 'messages' ? (
        <div className="min-h-0 flex-1" key={id}>
          <MessagesView exchangeId={id} count={summary.messages ?? 0} />
        </div>
      ) : (
        <div className="min-h-0 flex-1 overflow-auto" key={id}>
          {q.error ? (
            <Empty>Couldn't load this exchange: {q.error.message}</Empty>
          ) : !x ? (
            <Empty>Loading…</Empty>
          ) : (
            <>
              {x.error && <FailureNotice exchange={x} />}
              {x.kind === 'tunnel' && !x.error && <TunnelNotice exchange={x} />}
              {tab === 'overview' && <Overview x={x} />}
              {tab === 'request' && <RequestPane x={x} />}
              {tab === 'response' && <ResponsePane x={x} />}
              {tab === 'timing' && <TimingView exchange={x} />}
            </>
          )}
        </div>
      )}
    </section>
  )
}

const STATUS_TONE = {
  ok: 'text-ok bg-ok/10 ring-ok/25',
  redirect: 'text-redirect bg-redirect/10 ring-redirect/25',
  'client-err': 'text-client-err bg-client-err/10 ring-client-err/25',
  'server-err': 'text-server-err bg-server-err/10 ring-server-err/25',
  info: 'text-muted bg-raised ring-line-strong',
  none: 'text-muted bg-raised ring-line-strong',
}

function Pill({ className, children }) {
  return (
    <span className={`inline-flex h-[22px] items-center rounded px-2 text-[11.5px] font-semibold ring-1 ring-inset ${className}`}>
      {children}
    </span>
  )
}

function Header({ summary: s }) {
  const [copied, copy] = useCopy()
  const url = s.kind === 'tunnel' ? s.host : `${s.scheme}://${s.host}${s.path}`
  const tone = s.error ? STATUS_TONE['server-err'] : STATUS_TONE[statusClass(s.status)]
  return (
    <header className="border-b border-line px-4 pt-3 pb-3">
      <div className="flex items-center gap-2">
        <Pill className="bg-raised ring-line-strong">
          <span style={{ color: methodColor(s.method) }}>{s.method}</span>
        </Pill>
        <Pill className={tone}>
          {s.error ? `Failed · ${s.error}` : s.status ? `${s.status} ${s.statusText ?? ''}` : 'Pending'}
        </Pill>
        <span className="tabular ml-1 truncate text-[12px] text-muted">
          {[
            s.duration >= 0 && formatDuration(s.duration),
            s.respSize > 0 && formatBytes(s.respSize),
            s.proto,
          ]
            .filter(Boolean)
            .join(' · ')}
        </span>
        <div className="ml-auto flex shrink-0">
          <IconButton label={copied ? 'Copied' : 'Copy URL'} onClick={() => copy(url)}>
            <CopyIcon />
          </IconButton>
          <IconButton label="Delete exchange (Del)" onClick={() => engine.deleteExchange(s.id).catch(() => {})}>
            <TrashIcon />
          </IconButton>
        </div>
      </div>
      <p className="mt-2.5 truncate text-[14px] font-semibold tracking-tight text-text" title={url}>
        {s.host}
      </p>
      {s.kind !== 'tunnel' && (
        <p className="mt-0.5 truncate font-mono text-[12px] text-muted select-text" title={s.path}>
          {s.path}
        </p>
      )}
    </header>
  )
}

// Enough to report a problem without leaking headers or bodies, which is
// where credentials live.
function diagnostics(x) {
  return JSON.stringify(
    {
      id: x.id,
      kind: x.kind,
      method: x.request.method,
      host: x.request.host,
      proto: x.request.proto,
      state: x.state,
      error: x.error,
      timings: x.timings,
      started: x.started,
    },
    null,
    2,
  )
}

function FailureNotice({ exchange: x }) {
  const [copied, copy] = useCopy()
  const e = x.error
  return (
    <div className="m-3 rounded border border-server-err/40 bg-server-err/5 p-3 text-[12.5px]">
      <div className="flex items-start gap-2">
        <AlertIcon className="mt-0.5 shrink-0 text-server-err" />
        <div className="min-w-0 flex-1">
          <p className="font-medium text-text">{e.message}</p>
          {e.hint && <p className="mt-1 text-muted">{e.hint}</p>}
          <pre className="mt-2 font-mono text-[11.5px] break-all whitespace-pre-wrap text-faint">{e.detail}</pre>
          <Button className="mt-2" onClick={() => copy(diagnostics(x))}>
            <CopyIcon size={14} />
            {copied ? 'Copied' : 'Copy diagnostics'}
          </Button>
          <span className="ml-2 text-[11.5px] text-faint">Excludes headers and bodies.</span>
        </div>
      </div>
    </div>
  )
}

function TunnelNotice({ exchange: x }) {
  return (
    <div className="m-3 flex items-start gap-2 rounded border border-line-strong bg-raised p-3 text-[12.5px]">
      <LockIcon className="mt-0.5 shrink-0 text-[var(--m-connect)]" />
      <div className="text-muted">
        <p className="text-text">Encrypted tunnel to {x.request.host}</p>
        <p className="mt-1">
          The client sent CONNECT and TrafficKit relayed the bytes without reading them, so there are no HTTP
          details to show. That happens when HTTPS interception is off, the host is on the passthrough list,
          or the connection wasn't TLS.
        </p>
      </div>
    </div>
  )
}

function Overview({ x }) {
  const req = x.request
  const res = x.response
  const rows = [
    ['URL', req.url],
    ['Method', req.method],
    ['Status', res ? `${res.status} ${res.statusText}` : x.error ? 'no response' : 'waiting'],
    ['Protocol', res && res.proto !== req.proto ? `${req.proto} → ${res.proto}` : req.proto],
    ['Client', x.client],
    ['Started', new Date(x.started).toLocaleString(undefined, { fractionalSecondDigits: 3 })],
    ['Duration', x.timings.total >= 0 ? formatDuration(x.timings.total) : '—'],
    ['Request body', describeBody(req.body)],
    ['Response body', res ? describeBody(res.body) : '—'],
    ['Content type', (res && headerValue(res.headers, 'Content-Type')) || '—'],
  ]
  if (x.upgraded) rows.push(['Upgrade', `${headerValue(res?.headers, 'Upgrade') ?? 'protocol'} (frames not decoded)`])
  return (
    <dl className="grid grid-cols-[140px_1fr] gap-x-4 px-4 py-3 text-[12.5px]">
      {rows.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="border-b border-line/60 py-1.5 text-muted">{k}</dt>
          <dd className="border-b border-line/60 py-1.5 font-mono text-[12px] break-all select-text">{v}</dd>
        </div>
      ))}
    </dl>
  )
}

function describeBody(b) {
  if (!b || b.size === 0) return 'none'
  let s = formatBytes(b.size)
  if (b.encoding) s += `, ${b.encoding}`
  if (b.truncated) s += `, first ${formatBytes(b.captured)} captured`
  return s
}

function RequestPane({ x }) {
  const tab = useApp((s) => s.requestTab)
  const setTab = useApp((s) => s.setRequestTab)
  const req = x.request
  if (x.kind === 'tunnel') {
    return <KeyValueTable rows={req.headers} empty="The CONNECT request had no headers." />
  }
  const query = parseQuery(req.path)
  const cookies = parseCookieHeader(headerValues(req.headers, 'Cookie'))
  return (
    <>
      <SubTabs
        value={tab}
        onChange={setTab}
        tabs={[
          { id: 'headers', label: 'Headers', badge: req.headers.length },
          { id: 'query', label: 'Query', badge: query.length || null },
          { id: 'cookies', label: 'Cookies', badge: cookies.length || null },
          { id: 'body', label: 'Body', badge: req.body.size ? formatBytes(req.body.size) : null },
          { id: 'raw', label: 'Raw' },
        ]}
      />
      {tab === 'headers' && <KeyValueTable rows={req.headers} />}
      {tab === 'query' && <KeyValueTable rows={query} empty="No query parameters." />}
      {tab === 'cookies' && <KeyValueTable rows={cookies} empty="The request sent no cookies." />}
      {tab === 'body' && (
        <BodyView
          exchangeId={x.id}
          rev={x.rev}
          side="request"
          body={req.body}
          contentType={headerValue(req.headers, 'Content-Type')}
          inFlight={x.state === 'pending'}
        />
      )}
      {tab === 'raw' && (
        <RawView
          x={x}
          side="request"
          startLine={`${req.method} ${req.path} ${req.proto}`}
          headers={req.headers}
          body={req.body}
        />
      )}
    </>
  )
}

function ResponsePane({ x }) {
  const tab = useApp((s) => s.responseTab)
  const setTab = useApp((s) => s.setResponseTab)
  const res = x.response
  if (!res) {
    return <Empty>{x.error ? 'No response: the exchange failed before one arrived.' : 'Waiting for the response…'}</Empty>
  }
  if (x.kind === 'tunnel') return <Empty>The tunnel carries encrypted data; there is no HTTP response to show.</Empty>
  const setCookies = headerValues(res.headers, 'Set-Cookie')
  return (
    <>
      <SubTabs
        value={tab}
        onChange={setTab}
        tabs={[
          { id: 'headers', label: 'Headers', badge: res.headers.length },
          { id: 'cookies', label: 'Cookies', badge: setCookies.length || null },
          { id: 'body', label: 'Body', badge: res.body.size ? formatBytes(res.body.size) : null },
          { id: 'raw', label: 'Raw' },
        ]}
      />
      {tab === 'headers' && <KeyValueTable rows={res.headers} />}
      {tab === 'cookies' && <SetCookieTable values={setCookies} />}
      {tab === 'body' && (
        <BodyView
          exchangeId={x.id}
          rev={x.rev}
          side="response"
          body={res.body}
          contentType={headerValue(res.headers, 'Content-Type')}
          inFlight={x.state === 'streaming'}
        />
      )}
      {tab === 'raw' && (
        <RawView
          x={x}
          side="response"
          startLine={`${res.proto} ${res.status} ${res.statusText}`}
          headers={res.headers}
          body={res.body}
        />
      )}
    </>
  )
}

function RawView({ x, side, startLine, headers, body }) {
  const q = useBody(x.id, side, x.rev, body.captured > 0)
  const [copied, copy] = useCopy()
  let bodyText = ''
  if (body.captured > 0) {
    if (!q.data) bodyText = '…'
    else if (looksLikeText(q.data.bytes)) bodyText = decodeText(q.data.bytes)
    else bodyText = `[${formatBytes(q.data.bytes.length)} of binary data; see the Body tab]`
  }
  const text = rawMessage(startLine, headers, bodyText)
  return (
    <div>
      <div className="flex items-center gap-3 border-b border-line px-4 py-1.5 text-[12px] text-faint">
        <span className="flex-1">
          Reconstructed{q.data?.decoded ? `, body decoded from ${q.data.decoded}` : ''}. Headers are not in
          the order they were sent.
        </span>
        <Button variant="ghost" onClick={() => copy(text)}>
          <CopyIcon size={14} />
          {copied ? 'Copied' : 'Copy'}
        </Button>
      </div>
      <pre className="px-4 py-3 font-mono text-[12px] leading-[1.55] break-all whitespace-pre-wrap select-text">
        {text}
      </pre>
    </div>
  )
}
