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
      { target: 'qwen-code', compatible: true, reason: '凭据和协议均已确认' },
      { target: 'kimi-code', compatible: true, reason: '将使用 OpenAI Chat Completions' },
      { target: 'deepseek-harness', compatible: true, reason: '将使用 OpenAI Responses' },
    ]),
    createPlan: vi.fn().mockResolvedValue({
      id: 'apply-plan', profileId: profile.id, profileName: profile.name, keyHint: profile.keyHint,
      effects: [
        { target: 'claude-code', path: '/home/test/.claude/settings.json', description: '备份并原子更新' },
        { target: 'codex-cli', path: '/home/test/.codex/config.toml', description: '备份并原子更新' },
        { target: 'qwen-code', path: '/home/test/.qwen/settings.json', description: '备份并原子更新' },
        { target: 'kimi-code', path: '/home/test/.kimi-code/config.toml', description: '备份并原子更新' },
        { target: 'deepseek-harness', path: '/home/test/.dsh/settings.yaml', description: '备份并原子更新' },
      ],
      warning: '目标文件和备份均强制为 0600。', createdAt: '', expiresAt: '',
    }),
    apply: vi.fn().mockResolvedValue({
      planId: 'apply-plan', profileId: profile.id,
      results: [], succeeded: 5, failed: 0,
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
    expect(screen.getByLabelText(/Qwen Code/)).toBeChecked()
    expect(screen.getByLabelText(/Kimi Code/)).toBeChecked()
    expect(screen.getByLabelText(/DeepSeek Harness/)).toBeChecked()

    fireEvent.click(screen.getByRole('button', { name: '预览应用变更' }))
    const dialog = await screen.findByRole('dialog', { name: /确认应用 工作网关/ })
    expect(within(dialog).getByText('/home/test/.claude/settings.json')).toBeVisible()
    expect(within(dialog).getByText('/home/test/.qwen/settings.json')).toBeVisible()
    expect(within(dialog).getByText('/home/test/.kimi-code/config.toml')).toBeVisible()
    expect(within(dialog).getByText('/home/test/.dsh/settings.yaml')).toBeVisible()
    expect(within(dialog).getByText(/0600/)).toBeVisible()
    expect(api.apply).not.toHaveBeenCalled()

    fireEvent.click(within(dialog).getByRole('button', { name: '确认写入' }))
    await waitFor(() => expect(api.apply).toHaveBeenCalledWith('apply-plan'))
    expect(await screen.findByRole('status')).toHaveTextContent('已完成 5 个目标')
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

  it('edits public fields without revealing the stored key and preserves the profile identity', async () => {
    const updated = {
      ...profile,
      name: '生产网关',
      model: 'deepseek/deepseek-v4-flash',
      updatedAt: '2026-08-14T02:00:00Z',
    }
    const list = vi.fn().mockResolvedValueOnce([profile]).mockResolvedValueOnce([updated])
    const api = operations({ list, save: vi.fn().mockResolvedValue(updated) })
    setProfileOperationsForTests(api)
    render(<APIProfilesPage />)
    const profileHeading = await screen.findByRole('heading', { name: '工作网关' })
    const card = profileHeading.closest('article')
    expect(card).not.toBeNull()

    fireEvent.click(within(card as HTMLElement).getByRole('button', { name: '编辑' }))

    expect(screen.getByRole('heading', { name: '编辑加密档案' })).toBeVisible()
    expect(screen.getByLabelText('档案名称')).toHaveValue(profile.name)
    expect(screen.getByLabelText('模型名')).toHaveValue(profile.model)
    expect(screen.getByLabelText('Base URL')).toHaveValue(profile.baseUrl)
    expect(screen.getByLabelText('API Key')).toHaveValue('')
    expect(screen.queryByDisplayValue(/1234/)).not.toBeInTheDocument()
    expect(screen.getByText(/不会回显已保存的 Key/)).toBeVisible()

    fireEvent.change(screen.getByLabelText('档案名称'), { target: { value: updated.name } })
    fireEvent.change(screen.getByLabelText('模型名'), { target: { value: updated.model } })
    fireEvent.change(screen.getByLabelText('API Key'), { target: { value: 'test-key-5678' } })
    fireEvent.click(screen.getByRole('button', { name: '保存修改' }))

    await waitFor(() => expect(api.save).toHaveBeenCalledWith(expect.objectContaining({
      id: profile.id,
      name: updated.name,
      apiKey: 'test-key-5678',
      model: updated.model,
    })))
    expect(await screen.findByRole('heading', { name: updated.name })).toBeVisible()
    expect(screen.getByRole('heading', { name: '创建加密档案' })).toBeVisible()
    expect(screen.getByLabelText('API Key')).toHaveValue('')
  })
})
