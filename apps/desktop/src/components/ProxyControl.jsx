import { engine } from '../engine/client.js'
import { useApp } from '../state/app.js'

export async function toggleProxy() {
  const { proxy } = useApp.getState()
  return proxy?.running ? engine.stopProxy() : engine.startProxy()
}
