import { formatDuration } from '../../lib/format.js'
import { Empty } from './tables.jsx'

const PHASES = [
  { key: 'dns', label: 'DNS lookup', color: 'var(--m-connect)' },
  { key: 'connect', label: 'TCP connect', color: 'var(--m-put)' },
  { key: 'tls', label: 'TLS handshake', color: 'var(--m-patch)' },
  { key: 'send', label: 'Request sent', color: 'var(--m-get)' },
  { key: 'wait', label: 'Waiting (TTFB)', color: 'var(--m-post)' },
  { key: 'receive', label: 'Content download', color: 'var(--m-get)' },
]

export function TimingView({ exchange }) {
  const t = exchange.timings
  if (exchange.kind === 'tunnel') {
    return (
      <div className="px-4 py-3 text-[12.5px]">
        <Line label="Connect to upstream" value={t.connect} />
        <Line label="Tunnel open for" value={t.total} />
        <p className="mt-3 text-faint">Detailed phases aren't available for encrypted tunnels.</p>
      </div>
    )
  }
  if (t.total < 0) return <Empty>Timings appear when the exchange finishes.</Empty>

  // Lay phases end to end; skipped ones (-1) take no space.
  let offset = 0
  const bars = PHASES.map((p) => {
    const d = t[p.key]
    const bar = { ...p, d, start: offset }
    if (d > 0) offset += d
    return bar
  })
  const span = Math.max(offset, t.total, 0.001)
  const reused = t.dns < 0 && t.connect < 0

  return (
    <div className="px-4 py-3">
      <div className="grid grid-cols-[140px_1fr_80px] items-center gap-x-3 gap-y-1.5 text-[12px]">
        {bars.map((b) => (
          <div key={b.key} className="contents">
            <span className="text-muted">{b.label}</span>
            <div className="relative h-2.5 rounded-sm bg-raised">
              {b.d > 0 && (
                <div
                  className="absolute top-0 h-full rounded-sm"
                  style={{
                    left: `${(b.start / span) * 100}%`,
                    width: `max(2px, ${(b.d / span) * 100}%)`,
                    background: b.color,
                  }}
                />
              )}
            </div>
            <span className="tabular text-right font-mono text-muted">
              {b.d >= 0 ? formatDuration(b.d) : '—'}
            </span>
          </div>
        ))}
        <span className="mt-1 border-t border-line pt-1.5 font-medium">Total</span>
        <span className="mt-1 border-t border-line pt-1.5" />
        <span className="tabular mt-1 border-t border-line pt-1.5 text-right font-mono font-medium">
          {formatDuration(t.total)}
        </span>
      </div>
      {reused && (
        <p className="mt-4 text-[12px] text-faint">
          No DNS or connect phase: the proxy reused an open connection to this host.
        </p>
      )}
    </div>
  )
}

function Line({ label, value }) {
  return (
    <div className="flex justify-between border-b border-line/60 py-1">
      <span className="text-muted">{label}</span>
      <span className="tabular font-mono">{value >= 0 ? formatDuration(value) : '—'}</span>
    </div>
  )
}
