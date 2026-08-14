import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { ProxyResult } from '../domain'
import {
  resetProxyOperationsForTests,
  setProxyOperationsForTests,
} from '../services/osverse'
import { useProxyProbe } from './useProxyProbe'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

const usable: ProxyResult = {
  port: 7890,
  reachable: true,
  recommended: 'https-connect',
  checkedAt: '2026-08-14T01:02:03Z',
  attempts: [
    { protocol: 'http', available: true, latencyMillis: 4, message: 'HTTP 可用' },
    { protocol: 'https-connect', available: true, latencyMillis: 6, message: 'CONNECT 可用' },
    { protocol: 'socks5', available: false, latencyMillis: 0, message: 'SOCKS5 不可用' },
  ],
}

afterEach(() => resetProxyOperationsForTests())

describe('useProxyProbe', () => {
  it('starts in direct mode and enables a verified proxy', async () => {
    const probe = vi.fn().mockResolvedValue(usable)
    setProxyOperationsForTests(probe)
    const { result } = renderHook(() => useProxyProbe())

    expect(result.current.phase).toBe('direct')
    act(() => result.current.probe(7890))
    expect(result.current.phase).toBe('probing')

    await waitFor(() => expect(result.current.phase).toBe('proxy'))
    expect(probe).toHaveBeenCalledWith(7890)
    expect(result.current.result).toEqual(usable)
    expect(result.current.error).toBeNull()
  })

  it('keeps direct mode when no HTTPS-capable protocol is available', async () => {
    setProxyOperationsForTests(() => Promise.resolve({
      ...usable,
      reachable: false,
      recommended: '',
    }))
    const { result } = renderHook(() => useProxyProbe())

    act(() => result.current.probe(7890))

    await waitFor(() => expect(result.current.phase).toBe('error'))
    expect(result.current.error).toContain('HTTPS')
    expect(result.current.result?.reachable).toBe(false)
  })

  it('shows only the public rejection message', async () => {
    const failure = Object.assign(new Error('PROXY_PROBE_FAILED: 代理探测失败'), {
      cause: new Error('password=secret'),
    })
    setProxyOperationsForTests(() => Promise.reject(failure))
    const { result } = renderHook(() => useProxyProbe())

    act(() => result.current.probe(7890))

    await waitFor(() => expect(result.current.phase).toBe('error'))
    expect(result.current.error).toBe('PROXY_PROBE_FAILED: 代理探测失败')
    expect(result.current.error).not.toContain('secret')
  })

  it('direct mode invalidates an older pending probe', async () => {
    const pending = deferred<ProxyResult>()
    const direct = vi.fn().mockResolvedValue(undefined)
    setProxyOperationsForTests(() => pending.promise, direct)
    const { result } = renderHook(() => useProxyProbe())

    act(() => result.current.probe(7890))
    act(() => result.current.useDirect())
    pending.resolve(usable)
    await act(async () => pending.promise)

    expect(direct).toHaveBeenCalledTimes(1)
    expect(result.current.phase).toBe('direct')
    expect(result.current.result).toBeNull()
  })

  it('ignores a completion after unmount', async () => {
    const pending = deferred<ProxyResult>()
    setProxyOperationsForTests(() => pending.promise)
    const { result, unmount } = renderHook(() => useProxyProbe())
    act(() => result.current.probe(7890))
    unmount()

    pending.resolve(usable)
    await expect(pending.promise).resolves.toEqual(usable)
  })
})
