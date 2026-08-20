import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import type { EnvironmentSnapshot } from './domain'
import type { EnvironmentScanState } from './hooks/useEnvironmentScan'
import App from './App'
import {
  resetHistoryOperationsForTests,
  resetLaunchOperationForTests,
  resetRemovalOperationsForTests,
  setHistoryOperationsForTests,
  setLaunchOperationForTests,
  setRemovalOperationsForTests,
} from './services/osverse'

const mockUseEnvironmentScan = vi.fn<() => EnvironmentScanState>()

vi.mock('./hooks/useEnvironmentScan', () => ({
  useEnvironmentScan: () => mockUseEnvironmentScan(),
}))

const refresh = vi.fn()

const snapshot: EnvironmentSnapshot = {
  scannedAt: '2026-08-13T08:05:06Z',
  system: {
    distribution: 'Ubuntu',
    version: '24.04',
    architecture: 'x86_64',
    shell: '/bin/bash',
    supported: true,
    unsupportedReason: '',
  },
  components: [
    {
      id: 'claude-code',
      name: 'Claude Code',
      category: 'Core CLI',
      status: 'installed',
      installations: [
        {
          path: '/usr/bin/claude',
          resolvedPath: '/opt/claude/bin/claude',
          version: '1.2.3',
          source: 'path',
          managed: false,
        },
      ],
      message: '已安装',
      minimumOS: 'Ubuntu 20.04',
    },
    {
      id: 'codex-cli',
      name: 'Codex CLI',
      category: 'Core CLI',
      status: 'conflict',
      installations: [
        {
          path: '/usr/bin/codex',
          resolvedPath: '/opt/codex/bin/codex',
          version: '2.0.0',
          source: 'path',
          managed: false,
        },
        {
          path: '/home/test/.local/bin/codex',
          resolvedPath: '/home/test/tools/codex',
          version: '1.9.0',
          source: 'path',
          managed: false,
        },
      ],
      message: '检测到多个安装位置',
      minimumOS: 'Ubuntu 20.04',
    },
    {
      id: 'opencode-cli',
      name: 'OpenCode CLI',
      category: 'Core CLI',
      status: 'missing',
      installations: [],
      message: '未检测到安装',
      minimumOS: 'Ubuntu 20.04',
    },
    {
      id: 'github-copilot-cli',
      name: 'GitHub Copilot CLI',
      category: 'Core CLI',
      status: 'missing',
      installations: [],
      message: '未检测到安装',
      minimumOS: 'Ubuntu 20.04',
    },
    {
      id: 'kimi-code',
      name: 'Kimi Code',
      category: 'Core CLI',
      status: 'missing',
      installations: [],
      message: '未检测到安装',
      minimumOS: 'Ubuntu 20.04',
    },
    {
      id: 'claude-desktop',
      name: 'Claude Desktop',
      category: 'Desktop Applications',
      status: 'unsupported',
      installations: [],
      message: '当前系统不支持',
      minimumOS: 'Ubuntu 22.04',
    },
    {
      id: 'chatgpt-desktop',
      name: 'ChatGPT Desktop',
      category: 'Desktop Applications',
      status: 'broken',
      installations: [],
      message: '发现安装记录，但未找到可执行文件',
      minimumOS: 'Ubuntu 24.04',
    },
    {
      id: 'opencode-desktop',
      name: 'OpenCode Desktop',
      category: 'Desktop Applications',
      status: 'failed',
      installations: [],
      message: '组件检测失败',
      minimumOS: 'Ubuntu 20.04',
    },
    {
      id: 'cc-switch',
      name: 'CC Switch',
      category: 'Management Tools',
      status: 'update_available',
      installations: [],
      message: '有可用更新',
      minimumOS: 'Ubuntu 20.04',
    },
    {
      id: 'cockpit-tools',
      name: 'Cockpit Tools',
      category: 'Management Tools',
      status: 'detecting',
      installations: [],
      message: '正在检测',
      minimumOS: 'Ubuntu 20.04',
    },
  ],
  ready: 1,
  total: 10,
  needsAttention: 6,
}

function scanState(
  overrides: Partial<EnvironmentScanState> = {},
): EnvironmentScanState {
  return {
    snapshot,
    phase: 'ready',
    error: null,
    refresh,
    ...overrides,
  }
}

beforeEach(() => {
  refresh.mockReset()
  mockUseEnvironmentScan.mockReset()
  mockUseEnvironmentScan.mockReturnValue(scanState())
})

afterEach(() => {
  cleanup()
  resetHistoryOperationsForTests()
  resetLaunchOperationForTests()
  resetRemovalOperationsForTests()
})

describe('environment status dashboard', () => {
  it('renders system facts, local scan-time semantics, and summary counts', () => {
    const dateTimeFormat = vi.spyOn(Intl, 'DateTimeFormat')

    try {
      render(<App />)

      expect(screen.getByRole('heading', { name: '环境状态' })).toBeVisible()
      expect(screen.getByText('Ubuntu')).toBeVisible()
      expect(screen.getByText('x86_64')).toBeVisible()
      expect(screen.getByText('/bin/bash')).toBeVisible()
      expect(screen.getByText('支持')).toBeVisible()

      const scanTime = document.querySelector(
        'time[datetime="2026-08-13T08:05:06Z"]',
      )
      expect(scanTime).toBeVisible()
      expect(scanTime).not.toHaveTextContent(/^\s*$/)
      expect(scanTime?.nextElementSibling).toBeVisible()
      expect(scanTime?.nextElementSibling).not.toHaveTextContent(/^\s*$/)

      expect(dateTimeFormat).toHaveBeenCalledTimes(2)
      for (const [, options] of dateTimeFormat.mock.calls) {
        expect(options).not.toHaveProperty('timeZone')
      }

      expect(screen.getByRole('article', { name: '已就绪 1' })).toBeVisible()
      expect(screen.getByRole('article', { name: '工具总数 10' })).toBeVisible()
      expect(screen.getByRole('article', { name: '需要关注 6' })).toBeVisible()
    } finally {
      dateTimeFormat.mockRestore()
    }
  })

  it('renders PRETTY_NAME without duplicating its version', () => {
    mockUseEnvironmentScan.mockReturnValue(
      scanState({
        snapshot: {
          ...snapshot,
          system: {
            ...snapshot.system,
            distribution: 'Ubuntu 22.04.5 LTS',
            version: '22.04',
          },
        },
      }),
    )

    render(<App />)

    expect(
      screen.getByRole('heading', { name: 'Ubuntu 22.04.5 LTS' }),
    ).toBeVisible()
    expect(
      screen.queryByText('Ubuntu 22.04.5 LTS 22.04'),
    ).not.toBeInTheDocument()

    const systemCard = screen
      .getByRole('heading', { name: 'Ubuntu 22.04.5 LTS' })
      .closest('section')
    expect(systemCard).not.toBeNull()
    const versionFact = within(systemCard as HTMLElement)
      .getByText('版本', { selector: 'dt' })
      .closest('div')
    expect(versionFact).not.toBeNull()
    expect(
      within(versionFact as HTMLElement).getByText('22.04', {
        selector: 'dd',
      }),
    ).toBeVisible()
  })

  it('separates the exact backend categories into labelled sections', () => {
    render(<App />)

    const cli = screen.getByRole('region', { name: '命令行工具' })
    const desktop = screen.getByRole('region', { name: '桌面应用' })
    const management = screen.getByRole('region', { name: '管理工具' })

    expect(within(cli).getByText('Claude Code')).toBeVisible()
    expect(within(cli).getByText('Codex CLI')).toBeVisible()
    expect(within(cli).getByText('Kimi Code')).toBeVisible()
    expect(within(cli).queryByText('Claude Desktop')).not.toBeInTheDocument()
    expect(within(desktop).getByText('Claude Desktop')).toBeVisible()
    expect(within(desktop).getByText('ChatGPT Desktop')).toBeVisible()
    expect(within(management).getByText('CC Switch')).toBeVisible()
    expect(within(management).getByText('Cockpit Tools')).toBeVisible()
  })

  it('uses distinct Chinese labels for every relevant status', () => {
    render(<App />)

    for (const label of [
      '已安装',
      '未安装',
      '安装冲突',
      '系统不支持',
      '安装异常',
      '检测失败',
      '可更新',
      '检测中',
    ]) {
      expect(screen.getAllByText(label, { selector: '.status-badge' })[0]).toBeVisible()
    }
  })

  it('labels installing distinctly', () => {
    mockUseEnvironmentScan.mockReturnValue(
      scanState({
        snapshot: {
          ...snapshot,
          components: snapshot.components.map((component) =>
            component.id === 'cockpit-tools'
              ? { ...component, status: 'installing' }
              : component,
          ),
        },
      }),
    )

    render(<App />)

    expect(screen.getByText('安装中')).toBeVisible()
  })

  it('shows the reason when the current system is unsupported', () => {
    mockUseEnvironmentScan.mockReturnValue(
      scanState({
        snapshot: {
          ...snapshot,
          system: {
            ...snapshot.system,
            supported: false,
            unsupportedReason: '仅支持 Ubuntu 22.04 及以上版本',
          },
        },
      }),
    )

    render(<App />)

    expect(screen.getByText('不支持')).toBeVisible()
    expect(screen.getByText('仅支持 Ubuntu 22.04 及以上版本')).toBeVisible()
  })

  it('keeps every category visible when the scan has no components', () => {
    mockUseEnvironmentScan.mockReturnValue(
      scanState({
        snapshot: {
          ...snapshot,
          components: [],
          ready: 0,
          total: 0,
          needsAttention: 0,
        },
      }),
    )

    render(<App />)

    expect(screen.getByRole('region', { name: '命令行工具' })).toBeVisible()
    expect(screen.getByRole('region', { name: '桌面应用' })).toBeVisible()
    expect(screen.getByRole('region', { name: '管理工具' })).toBeVisible()
    expect(screen.getAllByText('本分类暂无扫描项')).toHaveLength(3)
  })

  it('renders invalid scan timestamps as unknown without a dateTime value', () => {
    mockUseEnvironmentScan.mockReturnValue(
      scanState({ snapshot: { ...snapshot, scannedAt: 'not-a-date' } }),
    )

    render(<App />)

    const unknownTime = screen.getByText('扫描时间未知')
    expect(unknownTime).toBeVisible()
    expect(unknownTime).not.toHaveAttribute('datetime')
    expect(screen.queryByText('not-a-date')).not.toBeInTheDocument()
  })

  it('exposes accessible sidebar navigation without decorative images', () => {
    render(<App />)

    const navigation = screen.getByRole('navigation', { name: '主导航' })
    for (const label of ['总览', 'API 配置', '安装记录', '设置']) {
      expect(within(navigation).getByRole('button', { name: label })).toBeVisible()
    }
    expect(within(navigation).getByRole('button', { name: '总览' })).toHaveAttribute('aria-current', 'page')
    expect(within(navigation).queryByRole('img')).not.toBeInTheDocument()
  })

  it('opens real history and settings pages instead of placeholders', async () => {
	setHistoryOperationsForTests(() => Promise.resolve([]))
	render(<App />)
	fireEvent.click(screen.getByRole('button', { name: '安装记录' }))
	expect(await screen.findByText('还没有操作记录')).toBeVisible()
	fireEvent.click(screen.getByRole('button', { name: '设置' }))
	expect(screen.getByRole('heading', { name: 'AES-256-GCM' })).toBeVisible()
	expect(screen.getByText('无遥测')).toBeVisible()
  })

  it('keeps desktop-width sidebar labels in normal layout', () => {
    render(<App />)

    for (const label of ['总览', 'API 配置', '安装记录', '设置']) {
      const visibleLabel = screen.getByRole('button', { name: label }).querySelector('.sidebar__label')
      expect(visibleLabel).toBeVisible()
      expect(visibleLabel).toHaveStyle({ position: '' })
    }
  })

  it('shows every conflict path and its resolved target', () => {
    render(<App />)

    const conflictCard = screen
      .getByRole('heading', { name: 'Codex CLI' })
      .closest('article')
    expect(conflictCard).not.toBeNull()
    const card = within(conflictCard as HTMLElement)
    expect(card.getByText('/usr/bin/codex')).toBeVisible()
    expect(card.getByText('/opt/codex/bin/codex')).toBeVisible()
    expect(card.getByText('/home/test/.local/bin/codex')).toBeVisible()
    expect(card.getByText('/home/test/tools/codex')).toBeVisible()
  })

  it('refreshes through the hook and enables supported verified installs', () => {
    render(<App />)

    fireEvent.click(screen.getByRole('button', { name: '刷新环境状态' }))
    expect(refresh).toHaveBeenCalledTimes(1)

    const openCodeCard = screen.getByRole('heading', { name: 'OpenCode CLI' }).closest('article')
    expect(within(openCodeCard as HTMLElement).getByRole('button', { name: /安装/ })).toBeEnabled()
    const copilotCard = screen.getByRole('heading', { name: 'GitHub Copilot CLI' }).closest('article')
    expect(within(copilotCard as HTMLElement).getByRole('button', { name: /安装/ })).toBeEnabled()
    const kimiCard = screen.getByRole('heading', { name: 'Kimi Code' }).closest('article')
    expect(within(kimiCard as HTMLElement).getByRole('button', { name: /安装/ })).toBeEnabled()
    const claudeCard = screen.getByRole('heading', { name: 'Claude Code' }).closest('article')
    expect(within(claudeCard as HTMLElement).getByRole('button', { name: /启动/ })).toBeEnabled()
    const desktopCard = screen.getByRole('heading', { name: 'Claude Desktop' }).closest('article')
    expect(within(desktopCard as HTMLElement).getByRole('button', { name: /安装/ })).toBeDisabled()
    const openCodeDesktop = screen.getByRole('heading', { name: 'OpenCode Desktop' }).closest('article')
    expect(within(openCodeDesktop as HTMLElement).getByRole('button', { name: /配置/ })).toBeEnabled()
    const ccSwitch = screen.getByRole('heading', { name: 'CC Switch' }).closest('article')
    expect(within(ccSwitch as HTMLElement).getByRole('button', { name: /更新/ })).toBeEnabled()
    const chatGPT = screen.getByRole('heading', { name: 'ChatGPT Desktop' }).closest('article')
    expect(within(chatGPT as HTMLElement).getByRole('button', { name: /配置/ })).toBeEnabled()
    expect(screen.getAllByText('官方校验安装')).toHaveLength(6)
  })

  it('starts detected CLI and external desktop installations through Osverse', () => {
    const launch = vi.fn().mockResolvedValue(undefined)
    setLaunchOperationForTests(launch)
    mockUseEnvironmentScan.mockReturnValue(scanState({
      snapshot: {
        ...snapshot,
        components: snapshot.components.map((component) => component.id === 'opencode-desktop' ? {
          ...component,
          status: 'installed',
          installations: [{
            path: '/opt/opencode/opencode-desktop',
            resolvedPath: '/opt/opencode/opencode-desktop',
            version: '1.18.18',
            source: 'desktop',
            managed: false,
          }],
        } : component),
      },
    }))
    render(<App />)

    const claude = screen.getByRole('heading', { name: 'Claude Code' }).closest('article')
    fireEvent.click(within(claude as HTMLElement).getByRole('button', { name: /启动/ }))
    const openCode = screen.getByRole('heading', { name: 'OpenCode Desktop' }).closest('article')
    fireEvent.click(within(openCode as HTMLElement).getByRole('button', { name: /启动/ }))

    expect(launch).toHaveBeenNthCalledWith(1, 'claude-code', '/usr/bin/claude')
    expect(launch).toHaveBeenNthCalledWith(2, 'opencode-desktop', '/opt/opencode/opencode-desktop')
    expect(within(claude as HTMLElement).getByText('在终端中启动')).toBeVisible()
    expect(within(openCode as HTMLElement).getByText('启动已检测应用')).toBeVisible()

    const codex = screen.getByRole('heading', { name: 'Codex CLI' }).closest('article')
    const locations = within(codex as HTMLElement).getAllByRole('button', { name: '启动此位置' })
    expect(locations).toHaveLength(2)
    fireEvent.click(locations[1])
    expect(launch).toHaveBeenNthCalledWith(3, 'codex-cli', '/home/test/.local/bin/codex')
  })

  it('allows fixed-identity removal when a Store app alias is disabled', () => {
    mockUseEnvironmentScan.mockReturnValue(scanState({ snapshot: {
      ...snapshot,
      components: snapshot.components.map((component) => component.id === 'chatgpt-desktop' ? {
        ...component, status: 'installed', installations: [], message: '已安装（应用执行别名未启用）',
      } : component),
    }}))
    render(<App />)
    const card = screen.getByRole('heading', { name: 'ChatGPT Desktop' }).closest('article')
    expect(within(card as HTMLElement).getByRole('button', { name: /移除/ })).toBeEnabled()
    expect(within(card as HTMLElement).queryByRole('button', { name: /启动/ })).not.toBeInTheDocument()
  })

  it('previews exact recoverable effects before removing a detected installation', async () => {
    const create = vi.fn().mockResolvedValue({
      id: 'remove-plan', componentId: 'claude-code', name: 'Claude Code',
      effects: [{ action: 'trash', path: '/home/test/.local/bin/claude', description: '移至回收站', recoverable: true }],
      warning: '配置、凭据和会话数据不会删除。', createdAt: '', expiresAt: '',
    })
    const remove = vi.fn().mockResolvedValue({
      planId: 'remove-plan', componentId: 'claude-code', removed: true, message: '已移除',
    })
    setRemovalOperationsForTests(create, remove)
    render(<App />)
    const claude = screen.getByRole('heading', { name: 'Claude Code' }).closest('article')

    fireEvent.click(within(claude as HTMLElement).getByRole('button', { name: /移除/ }))
    const dialog = await screen.findByRole('dialog', { name: /确认移除 Claude Code/ })
    expect(within(dialog).getByText('/home/test/.local/bin/claude')).toBeVisible()
    expect(within(dialog).getByText(/配置、凭据和会话数据不会删除/)).toBeVisible()
    expect(remove).not.toHaveBeenCalled()

    fireEvent.click(within(dialog).getByRole('button', { name: '确认移除' }))
    await waitFor(() => expect(remove).toHaveBeenCalledWith('remove-plan'))
    expect(refresh).toHaveBeenCalled()
  })

  it('shows the actionable backend message when a managed component is still running', async () => {
    const create = vi.fn().mockResolvedValue({
      id: 'remove-running', componentId: 'claude-code', name: 'Claude Code',
      effects: [{ action: 'trash', path: '/home/test/.local/bin/claude', description: '移至回收站', recoverable: true }],
      warning: '配置、凭据和会话数据不会删除。', createdAt: '', expiresAt: '',
    })
    const message = 'REMOVAL_IN_USE: 组件正在运行或被占用，请关闭相关终端和应用窗口后重试；原安装保持不变'
    setRemovalOperationsForTests(create, vi.fn().mockRejectedValue(new Error(message)))
    render(<App />)
    const claude = screen.getByRole('heading', { name: 'Claude Code' }).closest('article')

    fireEvent.click(within(claude as HTMLElement).getByRole('button', { name: /移除/ }))
    const dialog = await screen.findByRole('dialog', { name: /确认移除 Claude Code/ })
    fireEvent.click(within(dialog).getByRole('button', { name: '确认移除' }))

    expect(await screen.findByText(message)).toHaveAttribute('role', 'alert')
    expect(refresh).not.toHaveBeenCalled()
  })

  it('announces an initial scan without rendering a stale dashboard', () => {
    mockUseEnvironmentScan.mockReturnValue(
      scanState({ snapshot: null, phase: 'scanning' }),
    )

    render(<App />)

    expect(screen.getByRole('status')).toHaveTextContent('正在扫描环境')
    expect(screen.queryByRole('heading', { name: '命令行工具' })).not.toBeInTheDocument()
  })

  it('announces refresh progress while retaining the last snapshot', () => {
    mockUseEnvironmentScan.mockReturnValue(scanState({ phase: 'scanning' }))

    render(<App />)

    expect(screen.getByRole('status')).toHaveTextContent('正在刷新环境状态')
    expect(screen.getByText('Ubuntu')).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Codex CLI' })).toBeVisible()
    expect(screen.getByRole('button', { name: '刷新环境状态' })).toBeDisabled()
  })

  it('announces errors and retains the last good snapshot', () => {
    mockUseEnvironmentScan.mockReturnValue(
      scanState({ phase: 'error', error: 'SCAN_FAILED: 刷新失败' }),
    )

    render(<App />)

    expect(screen.getByRole('alert')).toHaveTextContent('SCAN_FAILED: 刷新失败')
    expect(screen.getByText('Ubuntu')).toBeVisible()
    expect(screen.getByRole('heading', { name: 'Codex CLI' })).toBeVisible()
  })

  it('renders an accessible retry path after an initial error', () => {
    mockUseEnvironmentScan.mockReturnValue(
      scanState({ snapshot: null, phase: 'error', error: '环境扫描失败' }),
    )

    render(<App />)

    expect(screen.getByRole('alert')).toHaveTextContent('环境扫描失败')
    fireEvent.click(screen.getByRole('button', { name: '重新扫描' }))
    expect(refresh).toHaveBeenCalledTimes(1)
  })
})
