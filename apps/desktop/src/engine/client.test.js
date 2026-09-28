import { afterEach, describe, expect, it, vi } from 'vitest'
import { EngineError, engine, setEngine } from './client.js'

afterEach(() => vi.unstubAllGlobals())

function stubFetch(impl) {
  const fn = vi.fn(impl)
  vi.stubGlobal('fetch', fn)
  return fn
}

describe('engine client', () => {
  it('sends the bearer token and JSON body', async () => {
    setEngine({ base: 'http://127.0.0.1:9', token: 'tok' })
    const fetch = stubFetch(async () => new Response('{"running":true}'))
    const st = await engine.startProxy({ port: 9000 })
    expect(st.running).toBe(true)
    const [url, init] = fetch.mock.calls[0]
    expect(url).toBe('http://127.0.0.1:9/v1/proxy/start')
    expect(init.method).toBe('POST')
    expect(init.headers.Authorization).toBe('Bearer tok')
    expect(JSON.parse(init.body)).toEqual({ port: 9000 })
  })

  it('leaves out Authorization without a token (the dev server adds it)', async () => {
    setEngine({ base: '', token: null })
    const fetch = stubFetch(async () => new Response('{}'))
    await engine.status()
    expect(fetch.mock.calls[0][1].headers.Authorization).toBeUndefined()
  })

  it('turns API errors into EngineError with code and hint', async () => {
    setEngine({ base: '', token: 't' })
    const payload = { error: { code: 'port_in_use', message: 'Port 1 is busy.', hint: 'Pick another.' } }
    stubFetch(async () => new Response(JSON.stringify(payload), { status: 409 }))
    const err = await engine.startProxy({ port: 1 }).catch((e) => e)
    expect(err).toBeInstanceOf(EngineError)
    expect(err).toMatchObject({ code: 'port_in_use', hint: 'Pick another.', status: 409 })
  })

  it('reports network failures as unreachable', async () => {
    setEngine({ base: '', token: 't' })
    stubFetch(async () => {
      throw new TypeError('Failed to fetch')
    })
    const err = await engine.status().catch((e) => e)
    expect(err.code).toBe('unreachable')
    expect(err.cause).toBeInstanceOf(TypeError)
  })

  it('returns body bytes with capture metadata', async () => {
    setEngine({ base: '', token: 't' })
    stubFetch(
      async () =>
        new Response(new Uint8Array([1, 2, 3]), {
          headers: { 'X-TK-Truncated': '1', 'X-TK-Decoded': 'gzip' },
        }),
    )
    const b = await engine.body(4, 'response')
    expect([...b.bytes]).toEqual([1, 2, 3])
    expect(b.truncated).toBe(true)
    expect(b.decoded).toBe('gzip')
  })
})
