import { useState } from 'react'
import { useApp } from '../state/app.js'
import { Logo, PlugIcon, SettingsIcon, TrafficIcon } from './icons.jsx'
import { toggleProxy } from './ProxyControl.jsx'

// Only sections that exist are listed. New ones appear here as they're built.
const NAV = [
  { id: 'connect', label: 'Connect', icon: PlugIcon },
  { id: 'traffic', label: 'Traffic', icon: TrafficIcon },
  { id: 'settings', label: 'Settings', icon: SettingsIcon },
]

function compact(n) {
  return n >= 10_000 ? `${Math.floor(n / 1000)}k` : n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n)
}

function PowerIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 16 16" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" aria-hidden="true">
      <path d="M8 2.5v5" />
      <path d="M4.6 4.6a5 5 0 106.8 0" />
    </svg>
  )
}

function ProxyButton() {
  const proxy = useApp((s) => s.proxy)
  const connection = useApp((s) => s.connection)
  const [busy, setBusy] = useState(false)
  const running = proxy?.running
  const failed = !running && proxy?.error
  const label = running
    ? `Proxy running on ${proxy.address}. Click to stop.`
    : failed
      ? `Proxy stopped: ${proxy.error.message}`
      : 'Proxy stopped. Click to start.'

  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      disabled={busy || connection !== 'live'}
      onClick={async () => {
        setBusy(true)
        try {
          await toggleProxy()
        } catch {
          // the status bar shows the engine's error
        } finally {
          setBusy(false)
        }
      }}
      className="group flex w-[52px] flex-col items-center gap-1 rounded-md py-2 text-[10.5px] text-muted hover:bg-hover disabled:opacity-50"
    >
      <span
        className={`flex h-8 w-8 items-center justify-center rounded-full border ${
          running
            ? 'border-ok/50 bg-ok/10 text-ok'
            : failed
              ? 'border-server-err/50 bg-server-err/10 text-server-err'
              : 'border-line-strong text-faint group-hover:text-text'
        }`}
      >
        <PowerIcon />
      </span>
      <span className="tabular">{running ? `:${proxy.port}` : 'Off'}</span>
    </button>
  )
}

export function Sidebar() {
  const view = useApp((s) => s.view)
  const setView = useApp((s) => s.setView)
  const sources = useApp((s) => s.sources.length)
  const badges = { connect: sources }

  return (
    <nav className="flex min-h-0 w-[68px] flex-col items-center border-r border-line bg-panel py-3" aria-label="Main">
      <div className="mb-4" title="TrafficKit">
        <Logo size={24} />
      </div>
      <ul className="flex flex-1 flex-col items-center gap-1">
        {NAV.map(({ id, label, icon: Icon }) => {
          const active = view === id
          const badge = badges[id]
          return (
            <li key={id}>
              <button
                type="button"
                onClick={() => setView(id)}
                aria-current={active ? 'page' : undefined}
                className={`relative flex w-[56px] flex-col items-center gap-1 rounded-md py-2 text-[10.5px] ${
                  active ? 'bg-hover text-text' : 'text-muted hover:bg-hover/60 hover:text-text'
                }`}
              >
                {active && <span className="absolute top-2 bottom-2 -left-1.5 w-[3px] rounded-full bg-accent" aria-hidden="true" />}
                <Icon size={19} className={active ? 'text-accent' : ''} />
                {label}
                {badge > 0 && (
                  <span className="tabular absolute top-1 right-1 min-w-[16px] rounded-full bg-raised px-1 text-center text-[9.5px] leading-[15px] text-muted ring-1 ring-line-strong">
                    {compact(badge)}
                  </span>
                )}
              </button>
            </li>
          )
        })}
      </ul>
      <ProxyButton />
    </nav>
  )
}
