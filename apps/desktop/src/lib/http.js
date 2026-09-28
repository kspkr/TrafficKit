export function headerValues(headers, name) {
  const lower = name.toLowerCase()
  return (headers ?? []).filter((h) => h.name.toLowerCase() === lower).map((h) => h.value)
}

export function headerValue(headers, name) {
  return headerValues(headers, name)[0]
}

// Query parameters of a path like "/search?q=a%20b&tag=x&tag=y", decoded.
export function parseQuery(path) {
  const i = path?.indexOf('?') ?? -1
  if (i < 0) return []
  const out = []
  for (const [name, value] of new URLSearchParams(path.slice(i + 1))) {
    out.push({ name, value })
  }
  return out
}

// Cookie request header(s) -> [{name, value}]
export function parseCookieHeader(values) {
  const out = []
  for (const v of values) {
    for (const part of v.split(';')) {
      const s = part.trim()
      if (!s) continue
      const eq = s.indexOf('=')
      out.push(eq < 0 ? { name: '', value: s } : { name: s.slice(0, eq), value: s.slice(eq + 1) })
    }
  }
  return out
}

// One Set-Cookie value -> {name, value, attrs: {domain, path, ...}}
export function parseSetCookie(line) {
  const [pair, ...rest] = line.split(';')
  const eq = pair.indexOf('=')
  const cookie = {
    name: eq < 0 ? '' : pair.slice(0, eq).trim(),
    value: eq < 0 ? pair.trim() : pair.slice(eq + 1).trim(),
    attrs: {},
  }
  for (const a of rest) {
    const s = a.trim()
    if (!s) continue
    const i = s.indexOf('=')
    const key = (i < 0 ? s : s.slice(0, i)).toLowerCase()
    cookie.attrs[key] = i < 0 ? true : s.slice(i + 1)
  }
  return cookie
}

export function statusClass(status) {
  if (!status) return 'none'
  if (status < 200) return 'info'
  if (status < 300) return 'ok'
  if (status < 400) return 'redirect'
  if (status < 500) return 'client-err'
  return 'server-err'
}

const METHOD_COLORS = {
  GET: 'var(--m-get)',
  POST: 'var(--m-post)',
  PUT: 'var(--m-put)',
  PATCH: 'var(--m-patch)',
  DELETE: 'var(--m-delete)',
  CONNECT: 'var(--m-connect)',
}

export function methodColor(method) {
  return METHOD_COLORS[method] ?? 'var(--m-other)'
}

// Rebuilds an HTTP/1.1-style message for the Raw view. Header order is the
// engine's (sorted), not the wire order.
export function rawMessage(startLine, headers, bodyText) {
  const lines = [startLine, ...(headers ?? []).map((h) => `${h.name}: ${h.value}`)]
  return lines.join('\r\n') + '\r\n\r\n' + (bodyText ?? '')
}
