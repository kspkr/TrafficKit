import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MessagesView } from './MessagesView.jsx'

vi.mock('../../engine/client.js', () => ({
  engine: {
    messages: vi.fn(async () => ({
      items: [
        { seq: 1, time: '2026-01-01T10:00:00Z', dir: 'send', type: 'text', size: 26, text: '{"op":"subscribe","ch":"a"}' },
        { seq: 2, time: '2026-01-01T10:00:01Z', dir: 'receive', type: 'text', size: 13, text: '{"ok":true}' },
        { seq: 3, time: '2026-01-01T10:00:02Z', dir: 'receive', type: 'binary', size: 3, base64: 'AAEC' },
        { seq: 4, time: '2026-01-01T10:00:03Z', dir: 'send', type: 'close', size: 2, closeCode: 1000, text: '' },
      ],
      more: false,
      total: 4,
      dropped: 0,
    })),
  },
}))

function renderView() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <div style={{ height: 600 }}>
        <MessagesView exchangeId={7} count={4} />
      </div>
    </QueryClientProvider>,
  )
}

describe('MessagesView', () => {
  const saved = {}
  beforeAll(() => {
    for (const [prop, value] of [['offsetWidth', 600], ['offsetHeight', 500]]) {
      saved[prop] = Object.getOwnPropertyDescriptor(HTMLElement.prototype, prop)
      Object.defineProperty(HTMLElement.prototype, prop, { configurable: true, get: () => value })
    }
  })
  afterAll(() => {
    for (const [prop, desc] of Object.entries(saved)) Object.defineProperty(HTMLElement.prototype, prop, desc)
  })

  it('lists messages with direction and filters them', async () => {
    renderView()
    expect(await screen.findByText('{"op":"subscribe","ch":"a"}')).toBeTruthy()
    expect(screen.getAllByLabelText('Received')).toHaveLength(2)

    fireEvent.click(screen.getByRole('button', { name: 'Sent' }))
    expect(screen.queryByText('{"ok":true}')).toBe(null)
    expect(screen.getByText('2 of 4')).toBeTruthy()

    fireEvent.click(screen.getByRole('button', { name: 'All' }))
    fireEvent.change(screen.getByLabelText('Search messages'), { target: { value: 'OK' } })
    expect(screen.getByText('{"ok":true}')).toBeTruthy()
    expect(screen.queryByText('{"op":"subscribe","ch":"a"}')).toBe(null)
  })

  it('shows a formatted payload when a message is selected', async () => {
    renderView()
    fireEvent.click(await screen.findByText('{"op":"subscribe","ch":"a"}'))
    expect(screen.getByText(/#1 · text/)).toBeTruthy()
    expect(screen.getAllByText('"subscribe"').length).toBeGreaterThan(0)
  })
})
