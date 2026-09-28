import { afterAll, beforeAll, describe, expect, it, vi } from 'vitest'
import { fireEvent, render, screen } from '@testing-library/react'
import { KeyValueTable } from './inspector/tables.jsx'
import { TrafficTable } from './TrafficTable.jsx'

describe('KeyValueTable', () => {
  it('renders captured values as text, never as markup', () => {
    const evil = '<img src=x onerror="alert(1)">'
    const { container } = render(<KeyValueTable rows={[{ name: 'X-Evil', value: evil }]} />)
    expect(container.querySelector('img')).toBe(null)
    expect(screen.getByText(evil)).toBeTruthy()
  })

  it('shows the empty message', () => {
    render(<KeyValueTable rows={[]} empty="Nothing here" />)
    expect(screen.getByText('Nothing here')).toBeTruthy()
  })
})

describe('TrafficTable', () => {
  const rows = Array.from({ length: 500 }, (_, i) => ({
    id: i + 1,
    rev: 1,
    state: 'complete',
    kind: 'http',
    method: i % 2 ? 'POST' : 'GET',
    scheme: 'http',
    host: 'api.test',
    path: `/item/${i + 1}`,
    proto: 'HTTP/1.1',
    status: 200,
    statusText: 'OK',
    respSize: 10,
    duration: 3,
    started: '2026-01-01T00:00:00Z',
  }))
  const props = { selectedId: null, onSelect: () => {}, sort: { key: 'id', dir: 'asc' }, onSort: () => {} }

  // jsdom reports every element as 0x0; give the scroll container a size.
  const saved = {}
  beforeAll(() => {
    for (const [prop, value] of [['offsetWidth', 1000], ['offsetHeight', 240]]) {
      saved[prop] = Object.getOwnPropertyDescriptor(HTMLElement.prototype, prop)
      Object.defineProperty(HTMLElement.prototype, prop, { configurable: true, get: () => value })
    }
  })
  afterAll(() => {
    for (const [prop, desc] of Object.entries(saved)) Object.defineProperty(HTMLElement.prototype, prop, desc)
  })

  it('renders only the visible window', () => {
    render(<TrafficTable {...props} rows={rows} />)
    const rendered = screen.getAllByRole('row').length - 1 // minus the header row
    expect(rendered).toBeGreaterThan(5)
    expect(rendered).toBeLessThan(60)
    expect(screen.getByText('/item/1')).toBeTruthy()
    expect(screen.queryByText('/item/400')).toBe(null)
  })

  it('selects on mouse down and sorts on header click', () => {
    const onSelect = vi.fn()
    const onSort = vi.fn()
    render(
      <TrafficTable
        {...props}
        rows={rows.slice(0, 3)}
        selectedId={2}
        onSelect={onSelect}
        sort={{ key: 'status', dir: 'asc' }}
        onSort={onSort}
      />,
    )
    fireEvent.mouseDown(screen.getByText('/item/3'))
    expect(onSelect).toHaveBeenCalledWith(3)
    fireEvent.click(screen.getByRole('columnheader', { name: /status/i }))
    expect(onSort).toHaveBeenCalledWith({ key: 'status', dir: 'desc' })
    const selected = screen.getAllByRole('row').find((r) => r.getAttribute('aria-selected') === 'true')
    expect(selected.textContent).toContain('/item/2')
  })

  it('marks failed and tunneled exchanges', () => {
    render(
      <TrafficTable
        {...props}
        rows={[
          { ...rows[0], id: 1, status: undefined, error: 'refused' },
          { ...rows[0], id: 2, kind: 'tunnel', method: 'CONNECT', host: 'secure.test:443', path: '' },
        ]}
      />,
    )
    expect(screen.getByTitle('refused').textContent).toContain('failed')
    expect(screen.getByText('encrypted')).toBeTruthy()
  })
})
