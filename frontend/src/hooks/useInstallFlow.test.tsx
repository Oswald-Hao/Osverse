import { act, renderHook, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import type { InstallPlan, InstallTask } from '../domain'
import {
  resetInstallOperationsForTests,
  setInstallOperationsForTests,
} from '../services/osverse'
import { useInstallFlow } from './useInstallFlow'

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

const plan: InstallPlan = {
  id: 'plan-1', componentId: 'codex-cli', name: 'Codex CLI', command: 'codex',
  version: '0.147.0', downloadBytes: 122020574,
  changes: [{ kind: 'download', path: 'registry.npmjs.org', description: '下载并校验' }],
  createdAt: '2026-08-14T00:00:00Z', expiresAt: '2026-08-14T00:10:00Z',
}

function task(phase: InstallTask['phase'], progress: number): InstallTask {
  return {
    id: 'task-1', planId: plan.id, componentId: plan.componentId, phase, progress,
    message: phase === 'completed' ? '安装完成' : '正在安装', errorCode: '',
    startedAt: '2026-08-14T00:00:01Z', finishedAt: '',
  }
}

afterEach(() => {
  vi.useRealTimers()
  resetInstallOperationsForTests()
})

describe('useInstallFlow', () => {
  it('requires plan review before starting and reports completion', async () => {
    const start = vi.fn().mockResolvedValue(task('completed', 100))
    const completed = vi.fn()
    setInstallOperationsForTests({
      createPlan: vi.fn().mockResolvedValue(plan),
      startInstall: start,
      getTask: vi.fn(),
    })
    const { result } = renderHook(() => useInstallFlow(completed))

    act(() => result.current.prepare('codex-cli'))
    expect(result.current.phase).toBe('planning')
    await waitFor(() => expect(result.current.phase).toBe('review'))
    expect(start).not.toHaveBeenCalled()

    act(() => result.current.confirm())
    await waitFor(() => expect(result.current.phase).toBe('completed'))
    expect(start).toHaveBeenCalledWith('plan-1')
    expect(completed).toHaveBeenCalledTimes(1)
  })

  it('polls non-terminal tasks and can request cancellation', async () => {
    vi.useFakeTimers()
    const cancel = vi.fn().mockResolvedValue(undefined)
    const getTask = vi.fn()
      .mockResolvedValueOnce(task('downloading', 50))
      .mockResolvedValueOnce(task('canceled', 50))
    setInstallOperationsForTests({
      createPlan: vi.fn().mockResolvedValue(plan),
      startInstall: vi.fn().mockResolvedValue(task('queued', 0)),
      getTask,
      cancelTask: cancel,
    })
    const { result } = renderHook(() => useInstallFlow(vi.fn()))

    await act(async () => {
      result.current.prepare('codex-cli')
      await Promise.resolve()
    })
    await act(async () => {
      result.current.confirm()
      await Promise.resolve()
    })
    expect(result.current.phase).toBe('installing')
    act(() => result.current.cancel())
    expect(cancel).toHaveBeenCalledWith('task-1')

    await act(async () => {
      await vi.advanceTimersByTimeAsync(400)
      await vi.advanceTimersByTimeAsync(400)
    })
    expect(getTask).toHaveBeenCalledTimes(2)
    expect(result.current.phase).toBe('error')
    expect(result.current.error).toContain('安装')
  })

  it('accepts the Windows desktop installing task phase', async () => {
    const installingTask = { ...task('queued', 0), phase: 'installing', progress: 85 }
    setInstallOperationsForTests({
      createPlan: vi.fn().mockResolvedValue(plan),
      startInstall: vi.fn().mockResolvedValue(installingTask),
      getTask: vi.fn().mockResolvedValue(task('completed', 100)),
    })
    const { result } = renderHook(() => useInstallFlow(vi.fn()))

    act(() => result.current.prepare('codex-cli'))
    await waitFor(() => expect(result.current.phase).toBe('review'))
    act(() => result.current.confirm())

    await waitFor(() => expect(result.current.task?.phase).toBe('installing'))
    expect(result.current.phase).toBe('installing')
    expect(result.current.error).toBeNull()
  })

  it('ignores an older plan after a newer request completes', async () => {
    const older = deferred<InstallPlan>()
    const newer = { ...plan, id: 'new-plan', componentId: 'opencode-cli', name: 'OpenCode CLI' }
    const create = vi.fn()
      .mockImplementationOnce(() => older.promise)
      .mockResolvedValueOnce(newer)
    setInstallOperationsForTests({ createPlan: create, startInstall: vi.fn(), getTask: vi.fn() })
    const { result } = renderHook(() => useInstallFlow(vi.fn()))

    act(() => result.current.prepare('codex-cli'))
    act(() => result.current.prepare('opencode-cli'))
    await waitFor(() => expect(result.current.plan?.id).toBe('new-plan'))
    older.resolve(plan)
    await act(async () => older.promise)

    expect(result.current.plan?.id).toBe('new-plan')
  })

  it('surfaces only a public planning error and dismisses it', async () => {
    const error = Object.assign(new Error('INSTALL_PLAN_FAILED: 无法创建安装计划'), {
      cause: new Error('secret URL'),
    })
    setInstallOperationsForTests({
      createPlan: vi.fn().mockRejectedValue(error), startInstall: vi.fn(), getTask: vi.fn(),
    })
    const { result } = renderHook(() => useInstallFlow(vi.fn()))
    act(() => result.current.prepare('codex-cli'))

    await waitFor(() => expect(result.current.phase).toBe('error'))
    expect(result.current.error).not.toContain('secret')
    act(() => result.current.dismiss())
    expect(result.current.phase).toBe('idle')
  })
})
