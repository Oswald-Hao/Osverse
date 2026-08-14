import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'

import type { InstallFlowState } from './hooks/useInstallFlow'
import InstallDialog from './InstallDialog'

afterEach(cleanup)

function reviewFlow(): InstallFlowState {
  return {
    phase: 'review',
    plan: {
      id: 'plan', componentId: 'codex-cli', name: 'Codex CLI', command: 'codex',
      version: '0.147.0', downloadBytes: 122020574,
      changes: [
        { kind: 'download', path: 'registry.npmjs.org', description: '下载并校验 Codex CLI' },
        { kind: 'command', path: '/home/test/.local/bin/codex', description: '创建命令入口' },
      ],
      createdAt: '', expiresAt: '',
    },
    task: null,
    error: null,
    prepare: vi.fn(), confirm: vi.fn(), cancel: vi.fn(), dismiss: vi.fn(),
  }
}

it('shows the immutable change preview before explicit confirmation', () => {
  const flow = reviewFlow()
  render(<InstallDialog flow={flow} />)

  expect(screen.getByRole('dialog', { name: 'Codex CLI' })).toBeVisible()
  expect(screen.getByText('0.147.0')).toBeVisible()
  expect(screen.getByText('116.4 MB')).toBeVisible()
  expect(screen.getByText('/home/test/.local/bin/codex')).toBeVisible()
  expect(screen.getByText(/不会运行 shell 安装脚本/)).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: '确认安装' }))
  expect(flow.confirm).toHaveBeenCalledTimes(1)
})

it('shows progress and prevents dismissing an active transaction', () => {
  const flow: InstallFlowState = {
    ...reviewFlow(), phase: 'installing',
    task: {
      id: 'task', planId: 'plan', componentId: 'codex-cli', phase: 'downloading',
      progress: 42, message: '正在下载官方安装包', errorCode: '', startedAt: '', finishedAt: '',
    },
  }
  render(<InstallDialog flow={flow} />)

  expect(screen.getByRole('progressbar')).toHaveValue(42)
  expect(screen.queryByRole('button', { name: '关闭安装窗口' })).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: '取消安装' }))
  expect(flow.cancel).toHaveBeenCalledTimes(1)
})

it('identifies an exact Microsoft Store install without claiming APT', () => {
  const flow = reviewFlow()
  flow.plan = {
    ...flow.plan!, componentId: 'codex-desktop', name: 'Codex Desktop', command: 'codex-desktop',
    version: 'Store latest', downloadBytes: 0,
    changes: [{ kind: 'store', path: '9PLM9XGG6VKS', description: '通过精确 Microsoft Store 产品 ID 安装' }],
  }
  render(<InstallDialog flow={flow} />)

  expect(screen.getByText('Microsoft Store')).toBeVisible()
  expect(screen.getByText('9PLM9XGG6VKS')).toBeVisible()
  expect(screen.queryByText('官方 APT 稳定版')).not.toBeInTheDocument()
})
