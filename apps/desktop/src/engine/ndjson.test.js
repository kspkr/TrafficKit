import { describe, expect, it } from 'vitest'
import { readNdjson } from './ndjson.js'

function streamOf(chunks) {
  return new ReadableStream({
    start(ctl) {
      for (const c of chunks) ctl.enqueue(typeof c === 'string' ? new TextEncoder().encode(c) : c)
      ctl.close()
    },
  })
}

async function collect(stream) {
  const out = []
  for await (const v of readNdjson(stream)) out.push(v)
  return out
}

describe('readNdjson', () => {
  it('splits lines that span chunks', async () => {
    const got = await collect(streamOf(['{"a":', '1}\n{"b"', ':2}\n']))
    expect(got).toEqual([{ a: 1 }, { b: 2 }])
  })

  it('handles a last line without newline and skips blank lines', async () => {
    expect(await collect(streamOf(['\n{"a":1}\n\n{"b":2}']))).toEqual([{ a: 1 }, { b: 2 }])
  })

  it('reassembles multi-byte characters split across chunks', async () => {
    const bytes = new TextEncoder().encode('{"s":"héllo 🚦"}\n')
    const cut = bytes.indexOf(0xc3) + 1 // inside the é
    expect(await collect(streamOf([bytes.slice(0, cut), bytes.slice(cut)]))).toEqual([{ s: 'héllo 🚦' }])
  })
})
