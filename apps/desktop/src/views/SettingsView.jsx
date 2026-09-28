import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { engine } from '../engine/client.js'
import { formatBytes, formatCount } from '../lib/format.js'
import { SHORTCUTS } from '../lib/hotkeys.js'
import { useApp } from '../state/app.js'
import { Button, Kbd, Segmented } from '../components/ui.jsx'
import { McpSettings } from './McpSettings.jsx'
import { Section } from './Section.jsx'


function Field({ label, children, hint }) {
  return (
    <label className="grid grid-cols-[140px_1fr] items-center gap-4 py-1.5 text-[12.5px]">
      <span className="text-muted">{label}</span>
      <span>
        {children}
        {hint && <span className="ml-3 text-[11.5px] text-faint">{hint}</span>}
      </span>
    </label>
  )
}

const inputClass =
  'h-7 rounded border border-line-strong bg-bg px-2 font-mono text-[12.5px] outline-none focus:border-select-line'

function ProxySettings() {
  const proxy = useApp((s) => s.proxy)
  const [bind, setBind] = useState('')
  const [port, setPort] = useState('')
  const [state, setState] = useState({ busy: false, error: null, saved: false })

  // Follow the engine when the proxy moves (e.g. restarted from elsewhere).
  const engineBind = proxy?.bind
  const enginePort = proxy?.port
  useEffect(() => {
    if (engineBind == null) return
    setBind(engineBind)
    setPort(String(enginePort))
  }, [engineBind, enginePort])

  const portNum = Number(port)
  const valid = Number.isInteger(portNum) && portNum >= 1 && portNum <= 65535 && bind.trim() !== ''
  const dirty = proxy && (bind !== proxy.bind || portNum !== proxy.port || !proxy.running)

  const apply = async (e) => {
    e.preventDefault()
    setState({ busy: true, error: null, saved: false })
    try {
      await engine.startProxy({ bind: bind.trim(), port: portNum })
      setState({ busy: false, error: null, saved: true })
    } catch (err) {
      setState({ busy: false, error: err, saved: false })
    }
  }

  return (
    <Section
      title="Proxy"
      note="Changes apply immediately and last until TrafficKit quits. To change the defaults, set proxy.bind and proxy.port in the config file."
    >
      <form onSubmit={apply}>
        <Field label="Listen address" hint="127.0.0.1 keeps the proxy private to this machine">
          <input className={`${inputClass} w-44`} value={bind} onChange={(e) => setBind(e.target.value)} spellCheck={false} />
        </Field>
        <Field label="Port">
          <input
            className={`${inputClass} w-24`}
            value={port}
            onChange={(e) => setPort(e.target.value.replace(/\D/g, ''))}
            inputMode="numeric"
          />
        </Field>
        <div className="mt-3 flex items-center gap-3 pl-[156px]">
          <Button type="submit" variant="primary" disabled={!valid || !dirty || state.busy}>
            {proxy?.running ? 'Restart on this address' : 'Start proxy'}
          </Button>
          {state.saved && <span className="text-[12px] text-ok">Proxy listening on {proxy?.address}</span>}
        </div>
        {state.error && (
          <div className="mt-3 ml-[156px] text-[12px]">
            <p className="text-server-err">{state.error.message}</p>
            {state.error.hint && <p className="mt-0.5 text-faint">{state.error.hint}</p>}
          </div>
        )}
      </form>
    </Section>
  )
}

function HttpsSettings() {
  const https = useApp((s) => s.https)
  const [passthrough, setPassthrough] = useState('')
  const [state, setState] = useState({ busy: false, error: null })
  const [confirmRegen, setConfirmRegen] = useState(false)

  const current = https?.passthrough?.join('\n') ?? ''
  useEffect(() => setPassthrough(current), [current])
  if (!https) return null

  const run = async (fn) => {
    setState({ busy: true, error: null })
    try {
      await fn()
      setState({ busy: false, error: null })
      return true
    } catch (err) {
      setState({ busy: false, error: err })
      return false
    }
  }
  const hosts = passthrough.split(/[\s,]+/).filter(Boolean)
  const ca = https.ca

  return (
    <Section
      title="HTTPS"
      note="TrafficKit decrypts HTTPS with its own certificate authority, created on this machine. Its private key stays in your data folder, and it is never added to your system's trusted certificates."
    >
      <Field label="Interception">
        <Segmented
          value={https.intercept ? 'on' : 'off'}
          onChange={(v) => run(() => engine.setHTTPS({ intercept: v === 'on' }))}
          options={[
            { id: 'on', label: 'Decrypt HTTPS' },
            { id: 'off', label: 'Tunnel only' },
          ]}
        />
      </Field>
      <div className="grid grid-cols-[140px_1fr] items-start gap-4 py-1.5 text-[12.5px]">
        <span className="pt-1 text-muted">Passthrough hosts</span>
        <div>
          <textarea
            aria-label="Passthrough hosts"
            className="h-20 w-full max-w-[420px] rounded border border-line-strong bg-bg px-2 py-1.5 font-mono text-[12px] outline-none focus:border-select-line"
            value={passthrough}
            onChange={(e) => setPassthrough(e.target.value)}
            placeholder={'bank.example.com\n*.apple.com'}
            spellCheck={false}
          />
          <span className="mt-1 block text-[11.5px] text-faint">
            Never decrypted. One per line; *.example.com covers subdomains. Use it for apps that pin certificates.
          </span>
          <Button
            className="mt-2"
            disabled={state.busy || hosts.join('\n') === current}
            onClick={() => run(() => engine.setHTTPS({ passthrough: hosts }))}
          >
            Save hosts
          </Button>
        </div>
      </div>

      <div className="mt-4 rounded border border-line bg-raised/50 p-3 text-[12px]">
        <p className="font-medium text-text">{ca.subject}</p>
        <p className="mt-1 text-muted">
          Valid until {new Date(ca.notAfter).toLocaleDateString()} · SHA-256{' '}
          <span className="font-mono">{ca.sha256.slice(0, 16)}…</span>
        </p>
        <div className="mt-2 flex flex-wrap items-center gap-2">
          <Button onClick={() => run(() => engine.revealCA())}>Show certificate file</Button>
          {!confirmRegen ? (
            <Button variant="danger" onClick={() => setConfirmRegen(true)}>
              Regenerate…
            </Button>
          ) : (
            <>
              <span className="text-client-err">
                Anything that trusts the current certificate stops working, and launched browsers are closed.
              </span>
              <Button
                variant="danger"
                disabled={state.busy}
                onClick={async () => {
                  if (await run(() => engine.regenerateCA())) setConfirmRegen(false)
                }}
              >
                Regenerate now
              </Button>
              <Button variant="ghost" onClick={() => setConfirmRegen(false)}>
                Cancel
              </Button>
            </>
          )}
        </div>
      </div>
      {state.error && (
        <p className="mt-2 text-[12px] text-server-err">
          {state.error.message} {state.error.hint && <span className="text-faint">{state.error.hint}</span>}
        </p>
      )}
    </Section>
  )
}

function CaptureSettings() {
  const { data } = useQuery({ queryKey: ['status'], queryFn: ({ signal }) => engine.status(signal) })
  if (!data) return null
  return (
    <Section title="Capture" note="Set in the config file (capture.maxBodyBytes, capture.maxExchanges).">
      <Field label="Body limit">
        <span className="font-mono">{formatBytes(data.capture.maxBodyBytes)}</span>
        <span className="ml-3 text-[11.5px] text-faint">per request and per response; the rest is forwarded but not kept</span>
      </Field>
      <Field label="History limit">
        <span className="font-mono">{formatCount(data.capture.maxExchanges)}</span>
        <span className="ml-3 text-[11.5px] text-faint">oldest exchanges are dropped past this</span>
      </Field>
      <Field label="Engine">
        <span className="font-mono">{data.version}</span>
      </Field>
    </Section>
  )
}

function AppearanceSettings() {
  const theme = useApp((s) => s.theme)
  const setTheme = useApp((s) => s.setTheme)
  return (
    <Section title="Appearance">
      <Field label="Theme">
        <Segmented
          value={theme}
          onChange={setTheme}
          options={[
            { id: 'dark', label: 'Dark' },
            { id: 'light', label: 'Light' },
            { id: 'system', label: 'System' },
          ]}
        />
      </Field>
    </Section>
  )
}

function ShortcutList() {
  const [q, setQ] = useState('')
  const needle = q.trim().toLowerCase()
  const shown = SHORTCUTS.filter(
    (s) => !needle || s.action.toLowerCase().includes(needle) || s.keys.join(' ').toLowerCase().includes(needle),
  )
  return (
    <Section title="Keyboard shortcuts">
      <input
        className={`${inputClass} mb-3 w-64 font-sans`}
        placeholder="Search shortcuts"
        value={q}
        onChange={(e) => setQ(e.target.value)}
      />
      <table className="text-[12.5px]">
        <tbody>
          {shown.map((s) => (
            <tr key={s.action}>
              <td className="py-1 pr-8 whitespace-nowrap">
                <span className="inline-flex gap-1">
                  {s.keys.map((k) => (
                    <Kbd key={k}>{k}</Kbd>
                  ))}
                  {s.alt && <span className="px-1 text-faint">or</span>}
                  {s.alt?.map((k) => (
                    <Kbd key={k}>{k}</Kbd>
                  ))}
                </span>
              </td>
              <td className="py-1 text-muted">{s.action}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </Section>
  )
}

export function SettingsView() {
  return (
    <div className="h-full overflow-auto bg-panel">
      <div className="mx-auto max-w-[720px] px-8 pb-16">
        <h1 className="pt-8 text-[17px] font-semibold tracking-tight">Settings</h1>
        <ProxySettings />
        <HttpsSettings />
        <McpSettings />
        <CaptureSettings />
        <AppearanceSettings />
        <ShortcutList />
        <section className="py-6 text-[12.5px] text-muted">
          <h2 className="text-[13px] font-semibold text-text">Privacy</h2>
          <p className="mt-2">
            TrafficKit makes no network requests of its own: no telemetry, no update checks, no crash reports.
            Captured traffic is held in memory by the local engine and is never written to disk or sent
            anywhere. Decrypted HTTPS often contains passwords and session cookies; treat captures accordingly.
          </p>
        </section>
      </div>
    </div>
  )
}
