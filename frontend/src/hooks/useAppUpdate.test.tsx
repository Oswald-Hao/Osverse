import { act, renderHook, waitFor } from '@testing-library/react'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import type { AppUpdateInfo } from '../domain'
import { checkForAppUpdate, startAppUpdate } from '../services/update'
import { useAppUpdate } from './useAppUpdate'

vi.mock('../services/update', () => ({
  checkForAppUpdate: vi.fn(),
  startAppUpdate: vi.fn(),
}))

const available: AppUpdateInfo = {
  available: true,
  canInstall: true,
  planId: 'opaque-plan',
  currentVersion: '0.3.0-beta.2',
  latestVersion: '0.4.0-beta.1',
  releaseName: 'Osverse v0.4.0-beta.1',
  releaseNotes: '新增应用内更新',
  publishedAt: '2026-08-14T00:00:00Z',
  downloadBytes: 1024,
  platform: 'windows',
  format: 'nsis',
  message: '安装程序将启动',
}

beforeEach(() => {
  vi.mocked(checkForAppUpdate).mockReset()
  vi.mocked(startAppUpdate).mockReset()
})

describe('useAppUpdate', () => {
  it('checks on startup and prompts when an update is available', async () => {
    vi.mocked(checkForAppUpdate).mockResolvedValue(available)
    const { result } = renderHook(() => useAppUpdate())
    await waitFor(() => expect(result.current.phase).toBe('available'))
    expect(result.current.visible).toBe(true)
    expect(result.current.info?.latestVersion).toBe('0.4.0-beta.1')
  })

  it('installs only the opaque backend plan selected by the check', async () => {
    vi.mocked(checkForAppUpdate).mockResolvedValue(available)
    vi.mocked(startAppUpdate).mockResolvedValue({ started: true, message: '正在重启' })
    const { result } = renderHook(() => useAppUpdate())
    await waitFor(() => expect(result.current.phase).toBe('available'))
    await act(async () => { await result.current.install() })
    expect(startAppUpdate).toHaveBeenCalledWith('opaque-plan')
    expect(result.current.phase).toBe('complete')
    expect(result.current.resultMessage).toBe('正在重启')
  })

  it('keeps automatic network failures silent until the user opens updates', async () => {
    vi.mocked(checkForAppUpdate).mockRejectedValue(new Error('网络不可用'))
    const { result } = renderHook(() => useAppUpdate())
    await waitFor(() => expect(result.current.phase).toBe('error'))
    expect(result.current.visible).toBe(false)
    expect(result.current.error).toBe('网络不可用')
  })
})
