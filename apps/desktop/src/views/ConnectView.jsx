import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { engine } from '../engine/client.js'
import { formatClock } from '../lib/format.js'
import { useApp } from '../state/app.js'
import { AlertIcon, CloseIcon, CopyIcon, GlobeIcon, TerminalIcon, WrenchIcon } from '../components/icons.jsx'
import { Button, IconButton, useCopy } from '../components/ui.jsx'

// Each browser gets a tint so the tiles are easy to tell apart at a glance.
// Deliberately not brand logos.
const TINTS = {
  chrome: '#4f8ff7',
  edge: '#2fb3b3',
  brave: '#f0643c',
  chromium: '#7aa2f7',
  vivaldi: '#ef4f5f',
  terminal: '#8bc34a',
}

function Monogram({ id, kind }) {
  const tint = TINTS[id] ?? 'var(--accent)'
  const Icon = kind === 'terminal' ? TerminalIcon : GlobeIcon
  return (
    <span
      className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md"
      style={{ background: `color-mix(in srgb, ${tint} 16%, transparent)`, color: tint }}
      aria-hidden="true"
    >
      <Icon size={18} />
    </span>
  )
}

function SectionTitle({ children, note }) {
  return (
    <div className="mb-2.5 flex items-baseline gap-3">
      <h2 className="text-[11.5px] font-semibold tracking-wider text-faint uppercase">{children}</h2>
      {note && <span className="text-[12px] text-faint">{note}</span>}
    </div>
  )
}

function LaunchTile({ target, onLaunch, busy, disabled }) {
  return (
    <button
      type="button"
      onClick={() => onLaunch(target)}
      disabled={disabled || busy}
      className="group flex items-start gap-3 rounded-md border border-line bg-panel p-3.5 text-left transition-colors hover:border-line-strong hover:bg-hover disabled:cursor-default disabled:opacity-50"
    >
      <Monogram id={target.id} kind={target.kind} />
      <span className="min-w-0 flex-1">
        <span className="flex items-center gap-2">
          <span className="text-[13.5px] font-medium">{target.name}</span>
          <span className="ml-auto text-[12px] text-accent opacity-0 transition-opacity group-hover:opacity-100">
            {busy ? 'Starting…' : 'Launch →'}
          </span>
        </span>
        <span className="mt-0.5 block text-[12px] leading-snug text-muted">{target.description}</span>
      </span>
    </button>
  )
}

function CopyField({ label, value, mono = true }) {
  const [copied, copy] = useCopy()
  return (
    <div className="grid grid-cols-[110px_1fr_auto] items-center gap-3 border-b border-line/60 py-2 text-[12.5px] last:border-0">
      <span className="text-muted">{label}</span>
      <span className={`min-w-0 truncate select-text ${mono ? 'font-mono text-[12px]' : ''}`} title={value}>
        {value}
      </span>
      <IconButton label={copied ? 'Copied' : `Copy ${label.toLowerCase()}`} onClick={() => copy(value)}>
        {copied ? <span className="text-[11px] text-ok">✓</span> : <CopyIcon size={14} />}
      </IconButton>
    </div>
  )
}

function ManualSetup() {
  const proxy = useApp((s) => s.proxy)
  const https = useApp((s) => s.https)
  const [revealError, setRevealError] = useState(null)
  const addr = proxy?.running ? proxy.address : null
  return (
    <div className="rounded-md border border-line bg-panel p-4">
      <div className="flex items-start gap-3">
        <span className="flex h-9 w-9 shrink-0 items-center justify-center rounded-md bg-raised text-muted" aria-hidden="true">
          <WrenchIcon size={18} />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-[13.5px] font-medium">Anything else</p>
          <p className="mt-0.5 text-[12px] text-muted">
            Point any app or device at the proxy yourself. For HTTPS, it has to trust the TrafficKit certificate;
            only do that on devices you control, and remove it when you're done.
          </p>
        </div>
      </div>
      <div className="mt-3">
        <CopyField label="Proxy" value={addr ?? 'not running'} />
        {https && (
          <>
            <CopyField label="Certificate" value={https.ca.path} />
            <CopyField label="SHA-256" value={https.ca.sha256.match(/.{2}/g).join(':')} />
          </>
        )}
      </div>
      <div className="mt-3 flex flex-wrap items-center gap-2">
        <Button
          onClick={() => {
            setRevealError(null)
            engine.revealCA().catch(setRevealError)
          }}
        >
          Show certificate file
        </Button>
        {addr && (
          <span className="text-[12px] text-faint">
            Devices using the proxy can also download it from <span className="font-mono">http://{addr}/certificate</span>
          </span>
        )}
      </div>
      {revealError && <p className="mt-2 text-[12px] text-server-err">{revealError.message}</p>}
    </div>
  )
}

function ActiveSources() {
  const sources = useApp((s) => s.sources)
  const setView = useApp((s) => s.setView)
  return (
    <aside className="rounded-md border border-line bg-panel">
      <div className="flex items-center justify-between border-b border-line px-4 py-2.5">
        <h2 className="text-[11.5px] font-semibold tracking-wider text-faint uppercase">Active sources</h2>
        {sources.length > 0 && (
          <button type="button" className="text-[12px] text-accent hover:underline" onClick={() => setView('traffic')}>
            View traffic →
          </button>
        )}
      </div>
      {sources.length === 0 ? (
        <p className="px-4 py-6 text-[12.5px] text-faint">
          Nothing launched yet. Browsers and terminals you start from here show up in this list, and TrafficKit
          closes them when it quits.
        </p>
      ) : (
        <ul>
          {sources.map((s) => (
            <li key={s.id} className="flex items-center gap-3 border-b border-line/60 px-4 py-2.5 last:border-0">
              <span className="h-2 w-2 shrink-0 rounded-full bg-ok shadow-[0_0_6px_var(--ok)]" aria-hidden="true" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-[13px]">{s.name}</p>
                <p className="tabular text-[11.5px] text-faint">
                  since {formatClock(s.started).slice(0, 8)}
                  {s.tracked ? ` · pid ${s.pid}` : ' · close it yourself when done'}
                </p>
              </div>
              <IconButton
                label={s.tracked ? `Close ${s.name}` : 'Remove from list'}
                onClick={() => engine.stopSource(s.id).catch(() => {})}
              >
                <CloseIcon size={14} />
              </IconButton>
            </li>
          ))}
        </ul>
      )}
    </aside>
  )
}

export function ConnectView() {
  const proxy = useApp((s) => s.proxy)
  const https = useApp((s) => s.https)
  const connection = useApp((s) => s.connection)
  const setView = useApp((s) => s.setView)
  const [url, setUrl] = useState('')
  const [pending, setPending] = useState(null)
  const [error, setError] = useState(null)

  const targetsQuery = useQuery({
    queryKey: ['targets'],
    queryFn: ({ signal }) => engine.targets(signal),
    enabled: connection === 'live',
    staleTime: 60_000,
  })
  const targets = targetsQuery.data?.items ?? []
  const browsers = targets.filter((t) => t.kind === 'browser')
  const terminals = targets.filter((t) => t.kind === 'terminal')
  const running = proxy?.running

  const launch = async (target) => {
    setPending(target.id)
    setError(null)
    try {
      await engine.launch(target.id, target.kind === 'browser' ? url.trim() : '')
    } catch (e) {
      setError(e)
    } finally {
      setPending(null)
    }
  }

  return (
    <div className="h-full overflow-auto bg-bg">
      <div className="mx-auto grid max-w-[1180px] grid-cols-1 gap-6 px-8 py-8 lg:grid-cols-[minmax(0,1fr)_300px]">
        <div className="min-w-0">
          <h1 className="text-[20px] font-semibold tracking-tight">Connect a source</h1>
          <p className="mt-1.5 max-w-[640px] text-[13px] text-muted">
            Launch something below and its traffic appears in TrafficKit, HTTPS included. Each browser gets its own
            temporary profile that trusts TrafficKit's certificate; nothing on your system is changed.
          </p>

          {!running && connection === 'live' && (
            <Banner>
              The proxy is stopped, so there's nothing to connect to.{' '}
              <button type="button" className="text-accent hover:underline" onClick={() => engine.startProxy().catch(setError)}>
                Start it
              </button>
            </Banner>
          )}
          {running && https && !https.intercept && (
            <Banner>
              HTTPS interception is off: launched apps work, but HTTPS shows up as encrypted tunnels.{' '}
              <button type="button" className="text-accent hover:underline" onClick={() => setView('settings')}>
                Change in Settings
              </button>
            </Banner>
          )}
          {error && (
            <Banner tone="error">
              {error.message}
              {error.hint && <span className="block text-faint">{error.hint}</span>}
            </Banner>
          )}

          <section className="mt-7">
            <SectionTitle note="Chromium-based browsers found on this machine">Browsers</SectionTitle>
            <label className="mb-3 flex h-8 max-w-[520px] items-center gap-2 rounded border border-line-strong bg-panel px-2.5 focus-within:border-select-line">
              <span className="shrink-0 text-[12px] text-faint">Open at</span>
              <input
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://… (optional, defaults to a check page)"
                spellCheck={false}
                className="min-w-0 flex-1 bg-transparent font-mono text-[12px] outline-none placeholder:font-sans placeholder:text-faint"
              />
            </label>
            {targetsQuery.isPending ? (
              <p className="text-[12.5px] text-faint">Looking for browsers…</p>
            ) : browsers.length === 0 ? (
              <p className="text-[12.5px] text-muted">
                No supported browser found. TrafficKit can launch Chrome, Edge, Brave, Chromium and Vivaldi.
              </p>
            ) : (
              <div className="grid grid-cols-1 gap-2.5 md:grid-cols-2 2xl:grid-cols-3">
                {browsers.map((t) => (
                  <LaunchTile key={t.id} target={t} onLaunch={launch} busy={pending === t.id} disabled={!running} />
                ))}
              </div>
            )}
          </section>

          {terminals.length > 0 && (
            <section className="mt-7">
              <SectionTitle>Terminal</SectionTitle>
              <div className="grid grid-cols-1 gap-2.5 md:grid-cols-2 2xl:grid-cols-3">
                {terminals.map((t) => (
                  <LaunchTile key={t.id} target={t} onLaunch={launch} busy={pending === t.id} disabled={!running} />
                ))}
              </div>
            </section>
          )}

          <section className="mt-7">
            <SectionTitle>Manual setup</SectionTitle>
            <ManualSetup />
          </section>
        </div>

        <div className="lg:pt-[76px]">
          <ActiveSources />
        </div>
      </div>
    </div>
  )
}

function Banner({ tone, children }) {
  return (
    <div
      className={`mt-4 flex max-w-[720px] items-start gap-2 rounded border px-3 py-2 text-[12.5px] ${
        tone === 'error' ? 'border-server-err/40 bg-server-err/5' : 'border-line-strong bg-raised'
      }`}
    >
      <AlertIcon className={`mt-0.5 shrink-0 ${tone === 'error' ? 'text-server-err' : 'text-client-err'}`} />
      <div>{children}</div>
    </div>
  )
}
