import { beforeEach, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { useApp } from '../state/app.js'
import { ConnectView } from './ConnectView.jsx'

vi.mock('../engine/client.js', () => ({
  engine: {
    targets: vi.fn(async () => ({
      items: [
        { id: 'chrome', kind: 'browser', name: 'Google Chrome', description: 'fresh window', path: 'x' },
        { id: 'terminal', kind: 'terminal', name: 'New terminal', description: 'shell', path: 'y' },
      ],
    })),
    launch: vi.fn(async () => ({})),
    stopSource: vi.fn(async () => null),
    startProxy: vi.fn(),
    revealCA: vi.fn(),
  },
}))
const { engine } = await import('../engine/client.js')

const https = { intercept: true, passthrough: [], ca: { path: 'C:/ca.crt', sha256: 'AABBCC', spki: 'k', subject: 's' } }

function renderView() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <ConnectView />
    </QueryClientProvider>,
  )
}

describe('ConnectView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useApp.setState({
      connection: 'live',
      proxy: { running: true, address: '127.0.0.1:8877', bind: '127.0.0.1', port: 8877 },
      https,
      sources: [],
    })
  })

  it('lists detected targets and launches a browser at the given URL', async () => {
    renderView()
    const tile = await screen.findByRole('button', { name: /Google Chrome/ })
    fireEvent.change(screen.getByPlaceholderText(/optional/), { target: { value: ' https://example.com/ ' } })
    fireEvent.click(tile)
    await waitFor(() => expect(engine.launch).toHaveBeenCalledWith('chrome', 'https://example.com/'))
  })

  it('does not pass the URL to terminals', async () => {
    renderView()
    fireEvent.change(screen.getByPlaceholderText(/optional/), { target: { value: 'https://example.com/' } })
    fireEvent.click(await screen.findByRole('button', { name: /New terminal/ }))
    await waitFor(() => expect(engine.launch).toHaveBeenCalledWith('terminal', ''))
  })

  it('shows launch errors with their hint', async () => {
    engine.launch.mockRejectedValueOnce(Object.assign(new Error('Could not launch chrome.'), { hint: 'file not found' }))
    renderView()
    fireEvent.click(await screen.findByRole('button', { name: /Google Chrome/ }))
    expect(await screen.findByText('Could not launch chrome.')).toBeTruthy()
    expect(screen.getByText('file not found')).toBeTruthy()
  })

  it('disables launching while the proxy is stopped', async () => {
    useApp.setState({ proxy: { running: false, bind: '127.0.0.1', port: 8877 } })
    renderView()
    expect((await screen.findByRole('button', { name: /Google Chrome/ })).disabled).toBe(true)
    expect(screen.getByText(/proxy is stopped/)).toBeTruthy()
  })

  it('lists active sources and closes them', async () => {
    useApp.setState({
      sources: [{ id: 'a1', name: 'Google Chrome', kind: 'browser', pid: 42, started: '2026-01-01T10:00:00Z', tracked: true }],
    })
    renderView()
    fireEvent.click(screen.getByRole('button', { name: 'Close Google Chrome' }))
    expect(engine.stopSource).toHaveBeenCalledWith('a1')
  })
})
