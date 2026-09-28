import { useCallback, useDeferredValue, useMemo, useRef, useState } from 'react'
import { engine } from '../engine/client.js'
import { buildView } from '../lib/columns.js'
import { formatCount } from '../lib/format.js'
import { useHotkeys } from '../lib/hotkeys.js'
import { useApp } from '../state/app.js'
import { useTraffic } from '../state/traffic.js'
import { CloseIcon, CopyIcon, SearchIcon, TrashIcon } from '../components/icons.jsx'
import { INSPECTOR_TABS, Inspector } from '../components/inspector/Inspector.jsx'
import { toggleProxy } from '../components/ProxyControl.jsx'
import { SplitPane } from '../components/SplitPane.jsx'
import { TrafficTable } from '../components/TrafficTable.jsx'
import { Button, Kbd, useCopy } from '../components/ui.jsx'

export function TrafficView() {
  const [filter, setFilter] = useState('')
  const deferredFilter = useDeferredValue(filter)
  const [sort, setSort] = useState({ key: 'started', dir: 'asc' })
  const version = useTraffic((s) => s.version)
  const selectedId = useTraffic((s) => s.selectedId)
  const select = useTraffic((s) => s.select)
  const setInspectorTab = useApp((s) => s.setInspectorTab)
  const filterRef = useRef(null)

  // `all` is mutated in place; `version` is what tells us it changed.
  const { rows: all } = useTraffic.getState()
  const rows = useMemo(() => buildView(all, deferredFilter, sort), [all, version, deferredFilter, sort])

  const move = useCallback(
    (delta) => {
      if (rows.length === 0) return
      const i = rows.findIndex((r) => r.id === selectedId)
      const next = i < 0 ? (delta > 0 ? 0 : rows.length - 1) : Math.max(0, Math.min(rows.length - 1, i + delta))
      select(rows[next].id)
    },
    [rows, selectedId, select],
  )

  useHotkeys([
    { combo: '/', run: () => filterRef.current?.focus() },
    { combo: 'mod+f', inInputs: true, run: () => filterRef.current?.focus() },
    {
      combo: 'escape',
      inInputs: true,
      run: (e) => {
        if (e.target === filterRef.current) filterRef.current.blur()
        else select(null)
      },
    },
    { combo: 'arrowdown', run: () => move(1) },
    { combo: 'j', run: () => move(1) },
    { combo: 'arrowup', run: () => move(-1) },
    { combo: 'k', run: () => move(-1) },
    { combo: 'pagedown', run: () => move(20) },
    { combo: 'pageup', run: () => move(-20) },
    { combo: 'home', run: () => rows.length && select(rows[0].id) },
    { combo: 'end', run: () => rows.length && select(rows[rows.length - 1].id) },
    { combo: 'delete', run: () => selectedId != null && engine.deleteExchange(selectedId).catch(() => {}) },
    { combo: 'mod+shift+k', inInputs: true, run: () => engine.clearExchanges().catch(() => {}) },
    { combo: 'mod+shift+p', inInputs: true, run: () => toggleProxy().catch(() => {}) },
    ...INSPECTOR_TABS.map((tab, i) => ({ combo: String(i + 1), run: () => setInspectorTab(tab) })),
  ])

  const table =
    all.size === 0 ? (
      <GettingStarted />
    ) : rows.length === 0 ? (
      <NoMatches filter={deferredFilter} onClear={() => setFilter('')} />
    ) : (
      <TrafficTable rows={rows} selectedId={selectedId} onSelect={select} sort={sort} onSort={setSort} />
    )

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex h-11 shrink-0 items-center gap-2 border-b border-line bg-panel px-3">
        <label className="flex h-7 w-[min(420px,50%)] items-center gap-2 rounded border border-line-strong bg-bg px-2 focus-within:border-select-line">
          <SearchIcon className="shrink-0 text-faint" />
          <input
            ref={filterRef}
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter by host, path, status, type  (-term excludes)"
            spellCheck={false}
            className="min-w-0 flex-1 bg-transparent text-[12.5px] outline-none placeholder:text-faint"
            aria-label="Filter exchanges"
          />
          {filter ? (
            <button type="button" aria-label="Clear filter" onClick={() => setFilter('')} className="text-faint hover:text-text">
              <CloseIcon size={14} />
            </button>
          ) : (
            <Kbd>/</Kbd>
          )}
        </label>
        <span className="tabular text-[12px] text-faint">
          {filter ? `${formatCount(rows.length)} of ${formatCount(all.size)}` : `${formatCount(all.size)} ${all.size === 1 ? 'exchange' : 'exchanges'}`}
        </span>
        <div className="flex-1" />
        <Button variant="ghost" disabled={all.size === 0} onClick={() => engine.clearExchanges().catch(() => {})}>
          <TrashIcon size={14} />
          Clear
        </Button>
      </div>
      <div className="min-h-0 flex-1">
        {selectedId != null ? <SplitPane left={table} right={<Inspector id={selectedId} />} /> : table}
      </div>
    </div>
  )
}

function NoMatches({ filter, onClear }) {
  return (
    <div className="flex h-full flex-col items-center justify-center gap-3 bg-panel text-[13px] text-muted">
      <p>Nothing matches “{filter}”.</p>
      <Button onClick={onClear}>Clear filter</Button>
    </div>
  )
}

function CopyLine({ text }) {
  const [copied, copy] = useCopy()
  return (
    <div className="group flex items-center gap-2 rounded border border-line bg-bg px-3 py-2 font-mono text-[12px]">
      <code className="min-w-0 flex-1 break-all select-text">{text}</code>
      <button
        type="button"
        onClick={() => copy(text)}
        className="shrink-0 text-faint hover:text-text"
        aria-label="Copy command"
        title={copied ? 'Copied' : 'Copy'}
      >
        {copied ? <span className="text-[11px] text-ok">copied</span> : <CopyIcon size={14} />}
      </button>
    </div>
  )
}

// Shown before anything has been captured.
function GettingStarted() {
  const proxy = useApp((s) => s.proxy)
  const connection = useApp((s) => s.connection)
  const setView = useApp((s) => s.setView)
  const addr = proxy?.running ? proxy.address : null

  return (
    <div className="h-full overflow-auto bg-panel">
      <div className="mx-auto max-w-[560px] px-6 py-14 text-[13px]">
        <h1 className="text-[17px] font-semibold tracking-tight">No traffic yet</h1>
        {connection !== 'live' ? (
          <p className="mt-2 text-muted">Waiting for the engine…</p>
        ) : !addr ? (
          <p className="mt-2 text-muted">The proxy is stopped. Start it from the sidebar.</p>
        ) : (
          <>
            <p className="mt-2 text-muted">
              Launch a browser or terminal from Connect and everything it does shows up here.
            </p>
            <Button variant="primary" className="mt-4" onClick={() => setView('connect')}>
              Connect a source
            </Button>
            <p className="mt-8 mb-2 text-[12px] text-faint">Or send something through the proxy directly:</p>
            <CopyLine text={`curl -x http://${addr} http://example.com/`} />
          </>
        )}
      </div>
    </div>
  )
}
