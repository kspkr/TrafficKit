import { useEffect, useState } from 'react'
import { useApp } from '../state/app.js'

const LABELS = {
  connecting: ['bg-faint', 'Connecting to engine…'],
  live: ['bg-ok', 'Engine connected'],
  reconnecting: ['bg-client-err', 'Engine connection lost, retrying…'],
  offline: ['bg-server-err', 'Engine unreachable'],
}

const ACTIVE_FOR = 5 * 60_000

// Assistants that used the engine recently. Re-checked every 30 s so the
// indicator goes away on its own when they stop.
function useActiveClients() {
  const clients = useApp((s) => s.clients)
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 30_000)
    return () => clearInterval(t)
  }, [])
  return clients.filter((c) => now - new Date(c.lastSeen).getTime() < ACTIVE_FOR)
}

function Sep() {
  return <span className="h-3 w-px bg-line-strong" aria-hidden="true" />
}

export function StatusBar() {
  const connection = useApp((s) => s.connection)
  const proxy = useApp((s) => s.proxy)
  const https = useApp((s) => s.https)
  const setView = useApp((s) => s.setView)
  const [dot, label] = LABELS[connection]
  const proxyError = !proxy?.running && proxy?.error
  const assistants = useActiveClients()

  return (
    <footer className="col-span-2 flex h-[26px] items-center gap-3 border-t border-line bg-panel px-3 text-[11.5px] text-faint">
      <span className="flex items-center gap-1.5">
        <span className={`h-1.5 w-1.5 rounded-full ${dot}`} aria-hidden="true" />
        {label}
      </span>
      <Sep />
      {proxyError ? (
        <button type="button" className="text-server-err hover:underline" onClick={() => setView('settings')}>
          Proxy stopped: {proxy.error.message}
        </button>
      ) : proxy?.running ? (
        <span>
          Proxy <span className="tabular text-muted">{proxy.address}</span>
        </span>
      ) : (
        <span>Proxy stopped</span>
      )}
      {https && (
        <>
          <Sep />
          <span>HTTPS {https.intercept ? 'decrypted' : 'tunneled only'}</span>
        </>
      )}
      {assistants.length > 0 && (
        <>
          <Sep />
          <button
            type="button"
            onClick={() => setView('settings')}
            title="An AI assistant is reading captured traffic through MCP"
            className="flex items-center gap-1.5 text-accent hover:underline"
          >
            <span className="h-1.5 w-1.5 rounded-full bg-accent" aria-hidden="true" />
            AI connected: {assistants.map((c) => c.name).join(', ')}
          </button>
        </>
      )}
      <span className="ml-auto">Captures stay in memory on this machine</span>
    </footer>
  )
}
