import { useQuery } from '@tanstack/react-query'
import { engine } from '../engine/client.js'
import { formatClock } from '../lib/format.js'
import { useApp } from '../state/app.js'
import { CopyIcon } from '../components/icons.jsx'
import { useCopy } from '../components/ui.jsx'
import { Section } from './Section.jsx'

function Snippet({ label, text }) {
  const [copied, copy] = useCopy()
  return (
    <div className="mt-3">
      <div className="mb-1 flex items-center justify-between text-[12px] text-muted">
        <span>{label}</span>
        <button
          type="button"
          onClick={() => copy(text)}
          className="flex items-center gap-1 rounded px-1.5 py-0.5 hover:bg-hover hover:text-text"
        >
          <CopyIcon size={13} />
          {copied ? 'Copied' : 'Copy'}
        </button>
      </div>
      <pre className="overflow-x-auto rounded-md border border-line bg-bg px-3 py-2.5 font-mono text-[12px] leading-5 whitespace-pre text-text select-text">
        {text}
      </pre>
    </div>
  )
}

function quoteArg(p) {
  return /[\s"]/.test(p) ? `"${p}"` : p
}

// Setup instructions for connecting AI assistants through `traffickit mcp`.
export function McpSettings() {
  const clients = useApp((s) => s.clients)
  const { data } = useQuery({ queryKey: ['status'], queryFn: ({ signal }) => engine.status(signal) })
  const exe = data?.enginePath || 'traffickit'

  const claudeCode = `claude mcp add traffickit -- ${quoteArg(exe)} mcp`
  const json = JSON.stringify({ mcpServers: { traffickit: { command: exe, args: ['mcp'] } } }, null, 2)

  return (
    <Section
      title="AI assistants (MCP)"
      note="Let Claude or another assistant read captured traffic, search it, and open intercepted browsers, through the Model Context Protocol. Nothing is shared until you add TrafficKit to an assistant."
    >
      <Snippet label="Claude Code" text={claudeCode} />
      <Snippet label="Claude Desktop, Cursor, VS Code and other MCP clients (add to their MCP config)" text={json} />

      <div className="mt-4 space-y-1.5 text-[12px] text-muted">
        <p>
          Whatever the assistant reads is sent to its provider. Passwords, cookies, API keys and tokens are
          redacted before they leave; add <span className="font-mono text-text">--no-redact</span> after{' '}
          <span className="font-mono text-text">mcp</span> only if you need the real values.
        </p>
        <p>
          Captured pages can contain text written to mislead an AI. The assistant is told to treat traffic as
          data, but review what it does with what it reads.
        </p>
      </div>

      <div className="mt-4 rounded-md border border-line bg-raised/50 px-3 py-2.5 text-[12px]">
        <p className="font-medium text-text">Recently connected</p>
        {clients.length === 0 ? (
          <p className="mt-1 text-faint">No assistant has used TrafficKit since it started.</p>
        ) : (
          <ul className="mt-1 space-y-0.5 text-muted">
            {clients.map((c) => (
              <li key={c.name}>
                {c.name} <span className="text-faint">· last request {formatClock(c.lastSeen).slice(0, 8)}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </Section>
  )
}
