// Builds the Go engine as a Tauri sidecar:
//   apps/desktop/src-tauri/binaries/traffickit-<target-triple>[.exe]
//
// Tauri requires the target triple in the file name. Pass --target to cross
// compile, e.g. `node scripts/build-engine.mjs --target aarch64-apple-darwin`.
import { spawnSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import path from 'node:path'

const TRIPLES = {
  'x86_64-pc-windows-msvc': ['windows', 'amd64'],
  'aarch64-pc-windows-msvc': ['windows', 'arm64'],
  'x86_64-apple-darwin': ['darwin', 'amd64'],
  'aarch64-apple-darwin': ['darwin', 'arm64'],
  'x86_64-unknown-linux-gnu': ['linux', 'amd64'],
  'aarch64-unknown-linux-gnu': ['linux', 'arm64'],
}

function hostTriple() {
  const rustc = spawnSync('rustc', ['-vV'], { encoding: 'utf8' })
  const m = rustc.stdout?.match(/^host: (\S+)$/m)
  if (m) return m[1]
  const arch = process.arch === 'arm64' ? 'aarch64' : 'x86_64'
  return {
    win32: `${arch}-pc-windows-msvc`,
    darwin: `${arch}-apple-darwin`,
    linux: `${arch}-unknown-linux-gnu`,
  }[process.platform]
}

const i = process.argv.indexOf('--target')
const triple = i > 0 ? process.argv[i + 1] : hostTriple()
if (!TRIPLES[triple]) {
  console.error(`unsupported target ${triple}; expected one of:\n  ${Object.keys(TRIPLES).join('\n  ')}`)
  process.exit(1)
}
const [goos, goarch] = TRIPLES[triple]

const root = path.resolve(import.meta.dirname, '..')
const outDir = path.join(root, 'apps/desktop/src-tauri/binaries')
const out = path.join(outDir, `traffickit-${triple}${goos === 'windows' ? '.exe' : ''}`)
mkdirSync(outDir, { recursive: true })

const version = process.env.TRAFFICKIT_VERSION || '0.1.0'
const res = spawnSync(
  'go',
  ['build', '-trimpath', '-ldflags', `-s -w -X main.version=${version}`, '-o', out, './cmd/traffickit'],
  { cwd: root, stdio: 'inherit', env: { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: '0' } },
)
if (res.status !== 0) process.exit(res.status ?? 1)
console.log(out)
