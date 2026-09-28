import { useEngineSync } from './engine/connection.js'
import { useApp } from './state/app.js'
import { Sidebar } from './components/Sidebar.jsx'
import { StatusBar } from './components/StatusBar.jsx'
import { Button } from './components/ui.jsx'
import { AlertIcon, Logo } from './components/icons.jsx'
import { TrafficView } from './views/TrafficView.jsx'
import { SettingsView } from './views/SettingsView.jsx'
import { ConnectView } from './views/ConnectView.jsx'

export default function App() {
  useEngineSync()
  const connection = useApp((s) => s.connection)
  const view = useApp((s) => s.view)

  if (connection === 'offline') return <EngineOffline />

  return (
    <div className="grid h-full grid-cols-[68px_minmax(0,1fr)] grid-rows-[minmax(0,1fr)_auto]">
      <Sidebar />
      <main className="min-h-0 min-w-0">
        {view === 'connect' && <ConnectView />}
        {view === 'traffic' && <TrafficView />}
        {view === 'settings' && <SettingsView />}
      </main>
      <StatusBar />
    </div>
  )
}

// Only shown if we never managed to connect. Losing an established
// connection keeps the UI up and shows a banner in the status bar instead.
function EngineOffline() {
  const error = useApp((s) => s.connectionError)
  const details = [
    `code: ${error?.code ?? 'unknown'}`,
    `message: ${error?.message ?? ''}`,
    error?.cause ? `cause: ${error.cause}` : null,
    `location: ${window.location.origin}`,
  ]
    .filter(Boolean)
    .join('\n')

  return (
    <div className="flex h-full items-center justify-center bg-bg p-8">
      <div className="w-full max-w-[520px] rounded border border-line bg-panel p-6 text-[13px]">
        <div className="flex items-center gap-2">
          <Logo />
          <span className="font-semibold">TrafficKit</span>
        </div>
        <div className="mt-5 flex items-start gap-2">
          <AlertIcon className="mt-0.5 shrink-0 text-server-err" />
          <div>
            <p className="font-medium">{error?.message ?? 'Could not reach the TrafficKit engine.'}</p>
            <p className="mt-1 text-muted">
              {error?.hint ??
                'The UI talks to a local engine process. If you are developing, start everything with `npm run dev` from the repository root.'}
            </p>
          </div>
        </div>
        <pre className="mt-4 rounded border border-line bg-bg p-3 font-mono text-[11.5px] whitespace-pre-wrap text-faint select-text">
          {details}
        </pre>
        <p className="mt-3 text-[12px] text-faint">Retrying automatically.</p>
        <Button className="mt-3" onClick={() => window.location.reload()}>
          Reload now
        </Button>
      </div>
    </div>
  )
}
