import { beforeEach, describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { useApp } from '../state/app.js'
import { StatusBar } from './StatusBar.jsx'

describe('StatusBar', () => {
  beforeEach(() => {
    useApp.setState({
      connection: 'live',
      proxy: { running: true, address: '127.0.0.1:8877', bind: '127.0.0.1', port: 8877 },
      https: { intercept: true },
      clients: [],
    })
  })

  it('says when an AI assistant is reading traffic', () => {
    useApp.setState({ clients: [{ name: 'claude-code', lastSeen: new Date().toISOString() }] })
    render(<StatusBar />)
    expect(screen.getByText(/AI connected: claude-code/)).toBeTruthy()
  })

  it('drops assistants that have gone quiet', () => {
    useApp.setState({ clients: [{ name: 'old-bot', lastSeen: new Date(Date.now() - 10 * 60_000).toISOString() }] })
    render(<StatusBar />)
    expect(screen.queryByText(/AI connected/)).toBe(null)
  })

  it('shows why the proxy stopped', () => {
    useApp.setState({ proxy: { running: false, port: 8877, error: { message: 'Port 8877 is already in use.' } } })
    render(<StatusBar />)
    expect(screen.getByText(/Port 8877 is already in use/)).toBeTruthy()
  })
})
