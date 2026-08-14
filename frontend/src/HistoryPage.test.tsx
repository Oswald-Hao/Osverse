import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import HistoryPage from './HistoryPage'
import { resetHistoryOperationsForTests, setHistoryOperationsForTests } from './services/osverse'

describe('HistoryPage', () => {
  beforeEach(() => {
    setHistoryOperationsForTests(() => Promise.resolve([
      { id: 'entry-1', operationId: 'task-1', componentId: 'codex-cli', name: 'Codex CLI', action: 'install', status: 'completed', message: '安装完成', createdAt: '2026-08-14T03:00:00Z' },
      { id: 'entry-2', operationId: 'task-2', componentId: 'api-profile', name: 'API 配置', action: 'configure', status: 'failed', message: '部分目标失败', createdAt: '2026-08-14T02:00:00Z' },
    ]))
  })
  afterEach(() => resetHistoryOperationsForTests())

  it('renders redacted records and requires a second click to clear', async () => {
    const clear = vi.fn(() => Promise.resolve())
    setHistoryOperationsForTests(() => Promise.resolve([
      { id: 'entry-1', operationId: 'task-1', componentId: 'codex-cli', name: 'Codex CLI', action: 'install', status: 'completed', message: '安装完成', createdAt: '2026-08-14T03:00:00Z' },
    ]), clear)
    render(<HistoryPage />)
    expect(await screen.findByText('Codex CLI')).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: '清除记录' }))
    expect(clear).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '确认清除' }))
    await waitFor(() => expect(clear).toHaveBeenCalledTimes(1))
    expect(screen.getByText('还没有操作记录')).toBeVisible()
  })

  it('shows a safe error without discarding navigation content', async () => {
    setHistoryOperationsForTests(() => Promise.reject(new Error('无法读取历史记录')))
    render(<HistoryPage />)
    expect(await screen.findByRole('alert')).toHaveTextContent('无法读取历史记录')
  })
})
