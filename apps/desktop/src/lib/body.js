const TEXT_TYPES = [
  /^text\//,
  /[/+]json$/,
  /[/+]xml$/,
  /^application\/(javascript|ecmascript|x-www-form-urlencoded|graphql|x-ndjson|yaml|x-yaml|toml)$/,
  /^image\/svg\+xml$/,
]

export function isTextType(contentType) {
  if (!contentType) return false
  const t = contentType.split(';')[0].trim().toLowerCase()
  return TEXT_TYPES.some((re) => re.test(t))
}

export function isJsonType(contentType) {
  if (!contentType) return false
  return /[/+]json$/.test(contentType.split(';')[0].trim().toLowerCase())
}

// Rough check for bytes that are readable as UTF-8 text: decodes cleanly and
// has few control characters. Looks at the first 4 KB only.
export function looksLikeText(bytes) {
  const sample = bytes.subarray(0, 4096)
  if (sample.length === 0) return true
  let text
  try {
    text = new TextDecoder('utf-8', { fatal: true }).decode(sample, { stream: true })
  } catch {
    return false
  }
  let control = 0
  for (let i = 0; i < text.length; i++) {
    const c = text.charCodeAt(i)
    if (c === 0) return false
    if (c < 32 && c !== 9 && c !== 10 && c !== 13) control++
  }
  return control / text.length < 0.02
}

export function decodeText(bytes) {
  return new TextDecoder('utf-8').decode(bytes)
}

// Picks the initial viewer: 'json', 'text' or 'hex'.
export function detectMode(contentType, bytes) {
  if (isJsonType(contentType)) return 'json'
  if (isTextType(contentType)) return 'text'
  if (looksLikeText(bytes)) {
    const head = stripXssi(new TextDecoder().decode(bytes.subarray(0, 64))).text.trimStart()
    return head.startsWith('{') || head.startsWith('[') ? 'json' : 'text'
  }
  return 'hex'
}

// Some APIs (Google's among them) prefix JSON with a line that makes it
// unparseable as JavaScript, to stop cross-site script inclusion.
const XSSI_PREFIX = /^\s*(\)\]\}',?|while\s*\(1\);|for\s*\(;;\);)\s*/

export function stripXssi(text) {
  const m = XSSI_PREFIX.exec(text)
  return m ? { text: text.slice(m[0].length), prefix: m[1] } : { text, prefix: null }
}

// Returns pretty-printed JSON, or null if text isn't valid JSON.
export function prettyJson(text) {
  try {
    return JSON.stringify(JSON.parse(text), null, 2)
  } catch {
    return null
  }
}

// Classic 16-bytes-per-line hex dump with an ASCII gutter.
export function hexDump(bytes, limit = 64 * 1024) {
  const n = Math.min(bytes.length, limit)
  const lines = []
  for (let off = 0; off < n; off += 16) {
    let hex = ''
    let ascii = ''
    for (let i = 0; i < 16; i++) {
      if (off + i < n) {
        const b = bytes[off + i]
        hex += b.toString(16).padStart(2, '0') + ' '
        ascii += b >= 0x20 && b < 0x7f ? String.fromCharCode(b) : '.'
      } else {
        hex += '   '
      }
      if (i === 7) hex += ' '
    }
    lines.push(`${off.toString(16).padStart(8, '0')}  ${hex} ${ascii}`)
  }
  return lines.join('\n')
}

// Splits pretty JSON into tokens for highlighting. Output is rendered as
// text nodes, so nothing here needs escaping.
const JSON_TOKEN = /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g

export function jsonTokens(pretty) {
  const out = []
  let last = 0
  for (const m of pretty.matchAll(JSON_TOKEN)) {
    if (m.index > last) out.push({ t: 'punct', v: pretty.slice(last, m.index) })
    if (m[1] !== undefined) {
      out.push({ t: m[2] ? 'key' : 'string', v: m[1] })
      if (m[2]) out.push({ t: 'punct', v: m[2] })
    } else if (m[3] !== undefined) {
      out.push({ t: 'literal', v: m[3] })
    } else {
      out.push({ t: 'number', v: m[4] })
    }
    last = m.index + m[0].length
  }
  if (last < pretty.length) out.push({ t: 'punct', v: pretty.slice(last) })
  return out
}
