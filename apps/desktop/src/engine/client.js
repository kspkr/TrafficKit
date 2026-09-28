import { readNdjson } from './ndjson.js'

// Where the engine lives. Inside the desktop app the Tauri shell hands us the
// address and token; in the browser during development, requests go to the
// same origin and the Vite dev server adds the token.
let conn = null

export function isTauri() {
  return typeof window !== 'undefined' && '__TAURI_INTERNALS__' in window
}

export async function resolveEngine() {
  if (conn) return conn
  if (isTauri()) {
    const { invoke } = await import('@tauri-apps/api/core')
    let info
    try {
      info = await invoke('engine_info')
    } catch (reason) {
      // The shell couldn't start the engine; reason is its explanation.
      throw new EngineError({
        code: 'engine_start_failed',
        message: 'The TrafficKit engine failed to start.',
        hint: String(reason),
        status: 0,
      })
    }
    conn = { base: info.apiUrl, token: info.token }
  } else {
    conn = { base: '', token: null }
  }
  return conn
}

// For tests.
export function setEngine(c) {
  conn = c
}

export class EngineError extends Error {
  constructor({ code, message, hint, status, cause }) {
    super(message, { cause })
    this.name = 'EngineError'
    this.code = code
    this.hint = hint
    this.status = status
  }

  static async from(res) {
    let body = null
    try {
      body = await res.json()
    } catch {
      // not JSON; e.g. the dev server's own error page
    }
    const e = body?.error
    return new EngineError({
      code: e?.code ?? 'http_' + res.status,
      message: e?.message ?? `Engine responded with ${res.status} ${res.statusText}`,
      hint: e?.hint,
      status: res.status,
    })
  }

  static unreachable(cause) {
    return new EngineError({
      code: 'unreachable',
      message: 'Could not reach the TrafficKit engine.',
      hint: 'The engine process may have exited. Check the terminal or restart the app.',
      status: 0,
      cause,
    })
  }
}

function headers(c, extra) {
  const h = { ...extra }
  if (c.token) h.Authorization = `Bearer ${c.token}`
  return h
}

async function send(path, { method = 'GET', json, signal } = {}) {
  const c = await resolveEngine()
  let res
  try {
    res = await fetch(c.base + path, {
      method,
      signal,
      headers: headers(c, json !== undefined ? { 'Content-Type': 'application/json' } : {}),
      body: json !== undefined ? JSON.stringify(json) : undefined,
    })
  } catch (err) {
    if (err.name === 'AbortError') throw err
    throw EngineError.unreachable(err)
  }
  if (!res.ok) throw await EngineError.from(res)
  return res
}

async function sendJSON(path, opts) {
  const res = await send(path, opts)
  return res.status === 204 ? null : res.json()
}

export const engine = {
  status: (signal) => sendJSON('/v1/status', { signal }),
  startProxy: (opts = {}) => sendJSON('/v1/proxy/start', { method: 'POST', json: opts }),
  stopProxy: () => sendJSON('/v1/proxy/stop', { method: 'POST', json: {} }),

  listExchanges: (after = 0, limit = 5000, signal) =>
    sendJSON(`/v1/exchanges?after=${after}&limit=${limit}`, { signal }),
  exchange: (id, signal) => sendJSON(`/v1/exchanges/${id}`, { signal }),
  deleteExchange: (id) => sendJSON(`/v1/exchanges/${id}`, { method: 'DELETE' }),
  clearExchanges: () => sendJSON('/v1/exchanges', { method: 'DELETE' }),

  // Returns the body bytes plus what the engine said about them.
  async body(id, side, signal) {
    const res = await send(`/v1/exchanges/${id}/body/${side}?decode=1`, { signal })
    return {
      bytes: new Uint8Array(await res.arrayBuffer()),
      truncated: res.headers.get('X-TK-Truncated') === '1',
      decoded: res.headers.get('X-TK-Decoded'),
      decodeError: res.headers.get('X-TK-Decode-Error'),
    }
  },

  https: (signal) => sendJSON('/v1/https', { signal }),
  setHTTPS: (settings) => sendJSON('/v1/https', { method: 'POST', json: settings }),
  regenerateCA: () => sendJSON('/v1/https/ca/regenerate', { method: 'POST', json: {} }),
  revealCA: () => sendJSON('/v1/https/ca/reveal', { method: 'POST', json: {} }),
  async caCertificate() {
    const res = await send('/v1/https/ca.pem')
    return res.text()
  },

  targets: (signal) => sendJSON('/v1/targets', { signal }),
  launch: (target, url) => sendJSON('/v1/sources', { method: 'POST', json: { target, url: url || '' } }),
  stopSource: (id) => sendJSON(`/v1/sources/${id}`, { method: 'DELETE' }),

  async *events(signal) {
    const res = await send('/v1/events', { signal })
    yield* readNdjson(res.body)
  },
}
