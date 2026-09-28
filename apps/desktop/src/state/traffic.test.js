import { beforeEach, describe, expect, it } from 'vitest'
import { useTraffic } from './traffic.js'

const row = (id, rev, extra = {}) => ({ id, rev, method: 'GET', host: 'h', path: '/', ...extra })

describe('traffic store', () => {
  beforeEach(() => useTraffic.getState().clear())

  it('keeps the newest revision', () => {
    const t = useTraffic.getState()
    t.apply([row(1, 2, { status: 200 })])
    t.apply([row(1, 1)])
    expect(useTraffic.getState().rows.get(1).status).toBe(200)
  })

  it('only bumps version on real changes', () => {
    const t = useTraffic.getState()
    t.apply([row(1, 1)])
    const v = useTraffic.getState().version
    t.apply([row(1, 1)])
    expect(useTraffic.getState().version).toBe(v)
  })

  it('keeps newer rows from the stream when the list is replaced', () => {
    const t = useTraffic.getState()
    t.apply([row(1, 3, { status: 201 })])
    t.replace([row(1, 2), row(2, 1)])
    const { rows } = useTraffic.getState()
    expect(rows.get(1).rev).toBe(3)
    expect(rows.size).toBe(2)
  })

  it('drops the selection when its row goes away', () => {
    const t = useTraffic.getState()
    t.apply([row(1, 1), row(2, 1)])
    t.select(2)
    t.remove([2])
    expect(useTraffic.getState().selectedId).toBe(null)
    t.select(1)
    t.replace([row(3, 1)])
    expect(useTraffic.getState().selectedId).toBe(null)
  })
})
