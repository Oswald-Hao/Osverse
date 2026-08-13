import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { EnvironmentSnapshot } from '../domain'
import {
  resetScanEnvironmentForTests,
  setScanEnvironmentForTests,
} from '../services/osverse'
import { useEnvironmentScan } from './useEnvironmentScan'

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function snapshot(scannedAt: string): EnvironmentSnapshot {
  return {
    scannedAt,
    system: {
      distribution: 'Ubuntu',
      version: '22.04',
      architecture: 'x86_64',
      shell: '/bin/bash',
      supported: true,
      unsupportedReason: '',
    },
    components: [
      {
        id: 'codex-cli',
        name: 'Codex CLI',
        category: 'developer-tool',
        status: 'installed',
        installations: [
          {
            path: '/usr/bin/codex',
            resolvedPath: '/usr/bin/codex',
            version: '1.2.3',
            source: 'path',
            managed: false,
          },
        ],
        message: '已安装',
        minimumOS: 'Ubuntu 20.04',
      },
    ],
    ready: 1,
    total: 1,
    needsAttention: 0,
  }
}

afterEach(() => {
  resetScanEnvironmentForTests()
})

describe('useEnvironmentScan', () => {
  it('automatically scans on mount and exposes the successful snapshot', async () => {
    const pending = deferred<EnvironmentSnapshot>()
    const scan = vi.fn(() => pending.promise)
    setScanEnvironmentForTests(scan)

    const { result } = renderHook(() => useEnvironmentScan())

    expect(result.current.phase).toBe('scanning')
    expect(result.current.snapshot).toBeNull()
    expect(result.current.error).toBeNull()
    expect(scan).toHaveBeenCalledTimes(1)

    pending.resolve(snapshot('2026-08-13T08:00:00Z'))

    await waitFor(() => expect(result.current.phase).toBe('ready'))
    expect(result.current.snapshot).toEqual(snapshot('2026-08-13T08:00:00Z'))
    expect(Object.getPrototypeOf(result.current.snapshot)).toBe(Object.prototype)
    expect(Object.getPrototypeOf(result.current.snapshot?.system)).toBe(
      Object.prototype,
    )
    expect(Object.getPrototypeOf(result.current.snapshot?.components[0])).toBe(
      Object.prototype,
    )
    expect(result.current.error).toBeNull()
  })

  it('shows only accessible public error text after an initial failure', async () => {
    const publicError = Object.assign(
      new Error('SCAN_FAILED: 环境扫描失败'),
      { cause: new Error('token=secret-value') },
    )
    setScanEnvironmentForTests(() => Promise.reject(publicError))

    const { result } = renderHook(() => useEnvironmentScan())

    await waitFor(() => expect(result.current.phase).toBe('error'))
    expect(result.current.snapshot).toBeNull()
    expect(result.current.error).toBe('SCAN_FAILED: 环境扫描失败')
    expect(result.current.error).not.toContain('secret-value')
  })

  it('preserves the last snapshot while refresh replaces it', async () => {
    const first = snapshot('2026-08-13T08:00:00Z')
    const second = snapshot('2026-08-13T08:05:00Z')
    const refresh = deferred<EnvironmentSnapshot>()
    const scan = vi
      .fn<() => Promise<EnvironmentSnapshot>>()
      .mockResolvedValueOnce(first)
      .mockImplementationOnce(() => refresh.promise)
    setScanEnvironmentForTests(scan)
    const { result } = renderHook(() => useEnvironmentScan())
    await waitFor(() => expect(result.current.phase).toBe('ready'))

    act(() => result.current.refresh())

    expect(result.current.phase).toBe('scanning')
    expect(result.current.snapshot).toEqual(first)
    expect(result.current.error).toBeNull()

    refresh.resolve(second)
    await waitFor(() => expect(result.current.snapshot).toEqual(second))
    expect(result.current.phase).toBe('ready')
  })

  it('preserves the last snapshot when a refresh fails', async () => {
    const first = snapshot('2026-08-13T08:00:00Z')
    const refresh = deferred<EnvironmentSnapshot>()
    const scan = vi
      .fn<() => Promise<EnvironmentSnapshot>>()
      .mockResolvedValueOnce(first)
      .mockImplementationOnce(() => refresh.promise)
    setScanEnvironmentForTests(scan)
    const { result } = renderHook(() => useEnvironmentScan())
    await waitFor(() => expect(result.current.phase).toBe('ready'))

    act(() => result.current.refresh())
    refresh.reject(new Error('SCAN_FAILED: 刷新失败'))

    await waitFor(() => expect(result.current.phase).toBe('error'))
    expect(result.current.snapshot).toEqual(first)
    expect(result.current.error).toBe('SCAN_FAILED: 刷新失败')
  })

  it('ignores an older completion after a newer refresh succeeds', async () => {
    const older = deferred<EnvironmentSnapshot>()
    const newer = deferred<EnvironmentSnapshot>()
    const scan = vi
      .fn<() => Promise<EnvironmentSnapshot>>()
      .mockImplementationOnce(() => older.promise)
      .mockImplementationOnce(() => newer.promise)
    setScanEnvironmentForTests(scan)
    const { result } = renderHook(() => useEnvironmentScan())

    act(() => result.current.refresh())
    newer.resolve(snapshot('2026-08-13T09:00:00Z'))
    await waitFor(() => expect(result.current.phase).toBe('ready'))

    older.resolve(snapshot('2026-08-13T08:00:00Z'))
    await act(async () => {
      await older.promise
    })

    expect(result.current.snapshot?.scannedAt).toBe('2026-08-13T09:00:00Z')
    expect(result.current.error).toBeNull()
  })

  it('ignores an older rejection after a newer refresh succeeds', async () => {
    const older = deferred<EnvironmentSnapshot>()
    const newer = deferred<EnvironmentSnapshot>()
    const scan = vi
      .fn<() => Promise<EnvironmentSnapshot>>()
      .mockImplementationOnce(() => older.promise)
      .mockImplementationOnce(() => newer.promise)
    setScanEnvironmentForTests(scan)
    const { result } = renderHook(() => useEnvironmentScan())

    act(() => result.current.refresh())
    newer.resolve(snapshot('2026-08-13T09:00:00Z'))
    await waitFor(() => expect(result.current.phase).toBe('ready'))

    older.reject(new Error('stale public error'))
    await act(async () => {
      await expect(older.promise).rejects.toThrow('stale public error')
    })

    expect(result.current.phase).toBe('ready')
    expect(result.current.error).toBeNull()
  })

  it('does not update state after unmount', async () => {
    const pending = deferred<EnvironmentSnapshot>()
    setScanEnvironmentForTests(() => pending.promise)
    const { result, unmount } = renderHook(() => useEnvironmentScan())
    const stateAtUnmount = result.current

    unmount()
    pending.resolve(snapshot('2026-08-13T08:00:00Z'))
    await act(async () => {
      await pending.promise
    })

    expect(result.current).toBe(stateAtUnmount)
  })

  it('is StrictMode-safe when the discarded scan rejects', async () => {
    const discarded = deferred<EnvironmentSnapshot>()
    const active = deferred<EnvironmentSnapshot>()
    const scan = vi
      .fn<() => Promise<EnvironmentSnapshot>>()
      .mockImplementationOnce(() => discarded.promise)
      .mockImplementationOnce(() => active.promise)
    setScanEnvironmentForTests(scan)
    const unhandled = vi.fn()
    window.addEventListener('unhandledrejection', unhandled)
    const { result } = renderHook(() => useEnvironmentScan(), {
      reactStrictMode: true,
    })
    expect(scan).toHaveBeenCalledTimes(2)

    active.resolve(snapshot('2026-08-13T10:00:00Z'))
    await waitFor(() => expect(result.current.phase).toBe('ready'))
    discarded.reject(new Error('discarded public error'))
    await act(async () => {
      await expect(discarded.promise).rejects.toThrow('discarded public error')
    })

    expect(result.current.snapshot?.scannedAt).toBe('2026-08-13T10:00:00Z')
    expect(result.current.error).toBeNull()
    expect(unhandled).not.toHaveBeenCalled()
    window.removeEventListener('unhandledrejection', unhandled)
  })
})
