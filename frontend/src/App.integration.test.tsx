import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import App from './App'
import type { EnvironmentSnapshot } from './domain'
import {
  resetScanEnvironmentForTests,
  setScanEnvironmentForTests,
} from './services/osverse'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise
  })
  return { promise, resolve }
}

function snapshot(
  distribution: string,
  scannedAt: string,
): EnvironmentSnapshot {
  return {
    scannedAt,
    system: {
      distribution,
      version: '24.04',
      architecture: 'x86_64',
      shell: '/bin/bash',
      supported: true,
      unsupportedReason: '',
    },
    components: [
      {
        id: 'codex-cli',
        name: 'Codex CLI',
        category: 'Core CLI',
        status: 'installed',
        installations: [],
        message: 'Installed',
        minimumOS: 'Ubuntu 20.04',
      },
    ],
    ready: 1,
    total: 1,
    needsAttention: 0,
  }
}

afterEach(() => {
  cleanup()
  resetScanEnvironmentForTests()
})

describe('App scan integration', () => {
  it('renders an initial snapshot through the real scan hook', async () => {
    const first = snapshot('Ubuntu Initial', '2026-08-13T08:00:00Z')
    setScanEnvironmentForTests(vi.fn().mockResolvedValue(first))

    render(<App />)

    expect(screen.getByRole('status')).toHaveTextContent('正在扫描环境')
    expect(await screen.findByText('Ubuntu Initial 24.04')).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Codex CLI' })).toBeVisible()
  })

  it('retains the prior snapshot during refresh and then replaces it', async () => {
    const first = snapshot('Ubuntu Before', '2026-08-13T08:00:00Z')
    const second = snapshot('Ubuntu After', '2026-08-13T08:05:00Z')
    const pendingRefresh = deferred<EnvironmentSnapshot>()
    const scan = vi
      .fn<() => Promise<EnvironmentSnapshot>>()
      .mockResolvedValueOnce(first)
      .mockImplementationOnce(() => pendingRefresh.promise)
    setScanEnvironmentForTests(scan)
    render(<App />)
    expect(await screen.findByText('Ubuntu Before 24.04')).toBeVisible()

    fireEvent.click(screen.getByRole('button', { name: '刷新环境状态' }))

    expect(screen.getByRole('status')).toHaveTextContent('正在刷新环境状态')
    expect(screen.getByText('Ubuntu Before 24.04')).toBeVisible()

    await act(async () => pendingRefresh.resolve(second))

    expect(await screen.findByText('Ubuntu After 24.04')).toBeVisible()
    expect(screen.queryByText('Ubuntu Before 24.04')).not.toBeInTheDocument()
    expect(scan).toHaveBeenCalledTimes(2)
  })
})
