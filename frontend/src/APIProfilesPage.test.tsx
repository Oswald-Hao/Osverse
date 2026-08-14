import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'

import APIProfilesPage from './APIProfilesPage'
import {
  resetProfileOperationsForTests,
  setProfileOperationsForTests,
} from './services/osverse'

const profile = {
  id: 'profile-1', name: '工作网关', keyHint: '••••1234',
  baseUrl: 'https://api.example/v1', model: 'gpt-test',
  allowPrivateNetwork: false, protection: 'local-file',
  createdAt: '2026-08-14T01:00:00Z', updatedAt: '2026-08-14T01:00:00Z',
}

afterEach(() => {
  cleanup()
  resetProfileOperationsForTests()
})

function operations(overrides: Partial<Parameters<typeof setProfileOperationsForTests>[0]> = {}) {
  return {
    save: vi.fn().mockResolvedValue(profile),
    list: vi.fn().mockResolvedValue([]),
    delete: vi.fn().mockResolvedValue(undefined),
    probe: vi.fn().mockResolvedValue({
      profileId: profile.id, reachable: true, authenticated: true,
      protocols: [
        { protocol: 'openai-responses', status: 'compatible', message: '已识别协议路由' },
        { protocol: 'anthropic-messages', status: 'compatible', message: '已识别协议路由' },
      ],
      message: 'API 可访问，凭据已验证', checkedAt: '2026-08-14T01:00:00Z',
    }),
    compatibility: vi.fn().mockResolvedValue([
      { target: 'claude-code', compatible: true, reason: '凭据和协议均已确认' },
      { target: 'codex-cli', compatible: true, reason: '凭据和协议均已确认' },
      { target: 'opencode-cli', compatible: false, reason: 'API 未确认所需协议' },
    ]),
    createPlan: vi.fn().mockResolvedValue({
      id: 'apply-plan', profileId: profile.id, profileName: profile.name, keyHint: profile.keyHint,
      effects: [
        { target: 'claude-code', path: '/home/test/.claude/settings.json', description: '备份并原子更新' },
        { target: 'codex-cli', path: '/home/test/.codex/config.toml', description: '备份并原子更新' },
      ],
      warning: '目标文件和备份均强制为 0600。', createdAt: '', expiresAt: '',
    }),
    apply: vi.fn().mockResolvedValue({
      planId: 'apply-plan', profileId: profile.id,
      results: [], succeeded: 2, failed: 0,
    }),
    ...overrides,
  }
}

describe('APIProfilesPage', () => {
  it('explains Base URL, validates required fields, saves, and clears the key', async () => {
    const list = vi.fn().mockResolvedValueOnce([]).mockResolvedValueOnce([profile])
    const api = operations({ list })
    setProfileOperationsForTests(api)
    render(<APIProfilesPage />)
    await screen.findByText('尚未创建 API 配置档案')

    expect(screen.getByText(/从服务商控制台的 API 文档/)).toBeVisible()
    fireEvent.click(screen.getByRole('button', { name: '保存并加密' }))
    expect(screen.getByRole('alert')).toHaveTextContent('请完整填写')

    fireEvent.change(screen.getByLabelText('档案名称'), { target: { value: '工作网关' } })
    fireEvent.change(screen.getByLabelText('模型名'), { target: { value: 'gpt-test' } })
    fireEvent.change(screen.getByLabelText('API Key'), { target: { value: 'secret-key-1234' } })
    fireEvent.change(screen.getByLabelText('Base URL'), { target: { value: 'https://api.example/v1' } })
    fireEvent.click(screen.getByRole('button', { name: '保存并加密' }))

    await screen.findByText('••••1234')
    expect(api.save).toHaveBeenCalledWith(expect.objectContaining({ apiKey: 'secret-key-1234' }))
    expect(screen.getByLabelText('API Key')).toHaveValue('')
    expect(screen.queryByText('secret-key-1234')).not.toBeInTheDocument()
  })

  it('requires probe and immutable plan confirmation before applying', async () => {
    const api = operations({ list: vi.fn().mockResolvedValue([profile]) })
    setProfileOperationsForTests(api)
    render(<APIProfilesPage />)
    await screen.findByText('工作网关')

    fireEvent.click(screen.getByRole('button', { name: '探测兼容性' }))
    await screen.findByRole('heading', { name: '兼容矩阵' })
    expect(screen.getByText('凭据已验证')).toBeVisible()
    const protocolDetails = screen.getByRole('region', { name: '协议探测详情' })
    expect(protocolDetails).toHaveTextContent('OpenAI Responses')
    expect(protocolDetails).toHaveTextContent('Anthropic Messages')
    expect(protocolDetails).toHaveTextContent('已识别协议路由')
    expect(screen.getByLabelText(/OpenCode CLI/)).toBeDisabled()
    expect(screen.getByLabelText(/Claude Code/)).toBeChecked()

    fireEvent.click(screen.getByRole('button', { name: '预览应用变更' }))
    const dialog = await screen.findByRole('dialog', { name: /确认应用 工作网关/ })
    expect(within(dialog).getByText('/home/test/.claude/settings.json')).toBeVisible()
    expect(within(dialog).getByText(/0600/)).toBeVisible()
    expect(api.apply).not.toHaveBeenCalled()

    fireEvent.click(within(dialog).getByRole('button', { name: '确认写入' }))
    await waitFor(() => expect(api.apply).toHaveBeenCalledWith('apply-plan'))
    expect(await screen.findByRole('status')).toHaveTextContent('已完成 2 个目标')
  })

  it('requires a second click before deleting an encrypted profile', async () => {
    const api = operations({ list: vi.fn().mockResolvedValueOnce([profile]).mockResolvedValueOnce([]) })
    setProfileOperationsForTests(api)
    render(<APIProfilesPage />)
    await screen.findByText('工作网关')

    fireEvent.click(screen.getByRole('button', { name: '删除' }))
    expect(api.delete).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: '确认删除' }))
    await waitFor(() => expect(api.delete).toHaveBeenCalledWith(profile.id))
  })
})
