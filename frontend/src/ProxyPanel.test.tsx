import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  resetProxyOperationsForTests,
  setProxyOperationsForTests,
} from './services/osverse'
import ProxyPanel from './ProxyPanel'

afterEach(() => {
  cleanup()
  resetProxyOperationsForTests()
})

describe('ProxyPanel', () => {
  it('validates the only user-supplied network value locally', () => {
    const probe = vi.fn()
    setProxyOperationsForTests(probe)
    render(<ProxyPanel />)

    fireEvent.change(screen.getByLabelText(/本地代理端口/), { target: { value: 'host:7890' } })
    fireEvent.click(screen.getByRole('button', { name: '探测并使用' }))

    expect(screen.getByRole('alert')).toHaveTextContent('1 到 65535')
    expect(probe).not.toHaveBeenCalled()
    expect(screen.getByText('127.0.0.1 :')).toBeVisible()
  })

  it('renders fixed protocol results and permits returning to direct mode', async () => {
    const direct = vi.fn().mockResolvedValue(undefined)
    setProxyOperationsForTests(() => Promise.resolve({
      port: 7890,
      reachable: true,
      recommended: 'socks5',
      checkedAt: '2026-08-14T01:02:03Z',
      attempts: [
        { protocol: 'http', available: true, latencyMillis: 3, message: 'HTTP 可用' },
        { protocol: 'https-connect', available: false, latencyMillis: 0, message: 'CONNECT 不可用' },
        { protocol: 'socks5', available: true, latencyMillis: 5, message: 'SOCKS5 可用' },
      ],
    }), direct)
    render(<ProxyPanel />)

    fireEvent.click(screen.getByRole('button', { name: '探测并使用' }))
    await waitFor(() => expect(screen.getByText('代理已启用')).toBeVisible())

    const results = screen.getByLabelText('代理探测结果')
    expect(within(results).getByText('HTTP')).toBeVisible()
    expect(within(results).getByText('HTTPS CONNECT')).toBeVisible()
    expect(within(results).getByText('SOCKS5')).toBeVisible()
    expect(within(results).getByText('5 ms')).toBeVisible()

    fireEvent.click(screen.getByRole('button', { name: '使用直连' }))
    await waitFor(() => expect(screen.getByText('直连', { selector: '.connection-state' })).toBeVisible())
    expect(screen.queryByLabelText('代理探测结果')).not.toBeInTheDocument()
    expect(direct).toHaveBeenCalledTimes(1)
  })

  it('restores the backend selection after navigation without probing again', async () => {
    const current = vi.fn().mockResolvedValue({ protocol: 'socks5', port: 7897 })
    const probe = vi.fn()
    setProxyOperationsForTests(probe, () => Promise.resolve(), current)
    const first = render(<ProxyPanel />)

    await waitFor(() => expect(screen.getByText('代理已启用')).toBeVisible())
    expect(screen.getByLabelText(/本地代理端口/)).toHaveValue('7897')
    expect(screen.getByText('SOCKS5 · 127.0.0.1:7897')).toBeVisible()
    expect(probe).not.toHaveBeenCalled()

    first.unmount()
    render(<ProxyPanel />)
    await waitFor(() => expect(screen.getByText('代理已启用')).toBeVisible())
    expect(screen.getByLabelText(/本地代理端口/)).toHaveValue('7897')
    expect(current).toHaveBeenCalledTimes(2)
  })

  it('keeps the active selection visible when persistent clear fails', async () => {
    setProxyOperationsForTests(
      vi.fn(),
      () => Promise.reject(new Error('无法清除已保存的代理选择')),
      () => Promise.resolve({ protocol: 'https-connect', port: 2080 }),
    )
    render(<ProxyPanel />)
    await waitFor(() => expect(screen.getByText('代理已启用')).toBeVisible())

    fireEvent.click(screen.getByRole('button', { name: '使用直连' }))

    await waitFor(() => expect(screen.getByRole('alert')).toHaveTextContent('无法清除'))
    expect(screen.getByText('代理已启用')).toBeVisible()
    expect(screen.getByLabelText(/本地代理端口/)).toHaveValue('2080')
  })

  it('colors latency at the 500ms and 1000ms boundaries', async () => {
    setProxyOperationsForTests(() => Promise.resolve({
      port: 7890,
      reachable: true,
      recommended: 'https-connect',
      checkedAt: '2026-08-17T06:00:00Z',
      attempts: [
        { protocol: 'http', available: true, latencyMillis: 500, message: 'HTTP 可用' },
        { protocol: 'https-connect', available: true, latencyMillis: 1000, message: 'CONNECT 可用' },
        { protocol: 'socks5', available: true, latencyMillis: 1001, message: 'SOCKS5 可用' },
      ],
    }))
    render(<ProxyPanel />)
    fireEvent.click(screen.getByRole('button', { name: '探测并使用' }))

    await waitFor(() => expect(screen.getByText('500 ms')).toBeVisible())
    expect(screen.getByText('500 ms')).toHaveClass('proxy-latency--good')
    expect(screen.getByText('1000 ms')).toHaveClass('proxy-latency--warning')
    expect(screen.getByText('1001 ms')).toHaveClass('proxy-latency--bad')
  })

})
