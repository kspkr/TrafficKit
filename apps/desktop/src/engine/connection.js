import { useEffect } from 'react'
import { engine } from './client.js'
import { useTraffic } from '../state/traffic.js'
import { useApp } from '../state/app.js'

const FLUSH_MS = 40
const PAGE = 5000

async function fetchAll(signal) {
  const all = []
  let after = 0
  for (;;) {
    const { items, more } = await engine.listExchanges(after, PAGE, signal)
    all.push(...items)
    if (!more || items.length === 0) return all
    after = items[items.length - 1].id
  }
}

// Keeps the traffic store in sync with the engine for as long as the
// component using it is mounted: subscribe, load the list, apply events,
// reconnect with backoff when the stream drops.
export function useEngineSync() {
  useEffect(() => {
    const ctl = new AbortController()
    const { signal } = ctl
    const traffic = useTraffic.getState()
    const app = useApp.getState()

    let queue = []
    let timer = null
    const flush = () => {
      timer = null
      const batch = queue
      queue = []
      traffic.apply(batch)
    }
    const enqueue = (summary) => {
      queue.push(summary)
      timer ??= setTimeout(flush, FLUSH_MS)
    }
    const resync = async () => {
      traffic.replace(await fetchAll(signal))
    }

    async function run() {
      let delay = 500
      let everConnected = false
      while (!signal.aborted) {
        try {
          for await (const ev of engine.events(signal)) {
            switch (ev.type) {
              case 'hello':
                app.setProxy(ev.data.proxy)
                app.setHttps(ev.data.https)
                app.setSources(ev.data.sources)
                app.setClients(ev.data.clients)
                await resync()
                app.setConnection('live')
                everConnected = true
                delay = 500
                break
              case 'exchange':
                enqueue(ev.data)
                break
              case 'removed':
                flush()
                traffic.remove(ev.data.ids)
                break
              case 'cleared':
                queue = []
                traffic.clear()
                break
              case 'proxy':
                app.setProxy(ev.data)
                break
              case 'https':
                app.setHttps(ev.data)
                break
              case 'sources':
                app.setSources(ev.data)
                break
              case 'clients':
                app.setClients(ev.data)
                break
              case 'resync':
                flush()
                await resync()
                break
            }
          }
        } catch (err) {
          if (signal.aborted) return
          app.setConnection(everConnected ? 'reconnecting' : 'offline', err)
        }
        if (signal.aborted) return
        app.setConnection(everConnected ? 'reconnecting' : 'offline', useApp.getState().connectionError)
        await new Promise((r) => setTimeout(r, delay))
        delay = Math.min(delay * 2, 5000)
      }
    }

    run()
    return () => {
      ctl.abort()
      clearTimeout(timer)
    }
  }, [])
}
