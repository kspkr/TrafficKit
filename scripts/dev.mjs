// Starts the engine and the Vite dev server together.
//
//   npm run dev
//
// The engine is built to .dev/ and run with a fixed API port that the Vite
// config proxies to. Ctrl+C stops both.
import { spawn, spawnSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import path from 'node:path'

const root = path.resolve(import.meta.dirname, '..')
const devDir = path.join(root, '.dev')
const exe = path.join(devDir, process.platform === 'win32' ? 'traffickit.exe' : 'traffickit')
const apiPort = process.env.TK_DEV_API_PORT || '47821'

mkdirSync(devDir, { recursive: true })

console.log('building engine…')
const build = spawnSync('go', ['build', '-o', exe, './cmd/traffickit'], { cwd: root, stdio: 'inherit' })
if (build.status !== 0) process.exit(build.status ?? 1)

const children = []

function prefixed(name, stream, out) {
  let carry = ''
  stream.on('data', (chunk) => {
    carry += chunk
    const lines = carry.split('\n')
    carry = lines.pop()
    for (const line of lines) out.write(`${name} ${redact(line)}\n`)
  })
}

// The engine's ready line carries the API token. Terminal scrollback gets
// pasted into bug reports, so mask it.
function redact(line) {
  return line.replace(/("token":")[^"]+/, '$1…')
}

function start(name, cmd, args, opts = {}) {
  const child = spawn(cmd, args, {
    cwd: root,
    stdio: ['pipe', 'pipe', 'pipe'],
    env: { ...process.env, ...opts.env },
  })
  prefixed(name, child.stdout, process.stdout)
  prefixed(name, child.stderr, process.stderr)
  child.on('exit', (code) => {
    console.log(`${name} exited (${code})`)
    shutdown(code ?? 1)
  })
  children.push(child)
  return child
}

start(
  '[engine]',
  exe,
  [
    'serve',
    '--api-port', apiPort,
    '--dev-state', path.join(devDir, 'engine.json'),
    '--allow-origin', 'http://localhost:5173',
    '--exit-on-stdin-close',
  ],
  { env: { TRAFFICKIT_LOG_FORMAT: 'text' } },
)
// Run Vite through the current Node binary; spawning npm.cmd on Windows
// would need a shell.
const vite = path.join(root, 'node_modules/vite/bin/vite.js')
start('[ui]    ', process.execPath, [vite, '--config', 'apps/desktop/vite.config.js', 'apps/desktop'], {
  env: { TK_DEV_API_PORT: apiPort },
})

let stopping = false
function shutdown(code) {
  if (stopping) return
  stopping = true
  for (const c of children) {
    c.stdin.end() // the engine exits when stdin closes
    if (c.exitCode === null) c.kill()
  }
  setTimeout(() => process.exit(code), 300)
}

process.on('SIGINT', () => shutdown(0))
process.on('SIGTERM', () => shutdown(0))
