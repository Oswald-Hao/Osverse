import { afterEach, describe, expect, it, vi } from 'vitest'

import {
  createRemovalPlan,
  resetRemovalOperationsForTests,
  setRemovalOperationsForTests,
} from './osverse'

const removalActions = [
  'trash',
  'package',
  'recover',
  'manifest',
  'store',
  'msi',
  'uninstaller',
] as const

describe('removal plan bridge contract', () => {
  afterEach(() => resetRemovalOperationsForTests())

  it.each(removalActions)('accepts the backend %s effect', async (action) => {
    setRemovalOperationsForTests(
      vi.fn().mockResolvedValue({
        id: 'remove-plan',
        componentId: 'deepseek-harness',
        name: 'DeepSeek Harness',
        effects: [{ action, path: 'C:\\managed', description: '受信任操作', recoverable: action === 'recover' || action === 'manifest' }],
        warning: '保留用户数据',
        createdAt: '2026-08-20T00:00:00Z',
        expiresAt: '2026-08-20T00:03:00Z',
      }),
      vi.fn(),
    )

    await expect(createRemovalPlan('deepseek-harness')).resolves.toMatchObject({
      effects: [{ action }],
    })
  })

  it('continues to reject unknown removal effects', async () => {
    setRemovalOperationsForTests(
      vi.fn().mockResolvedValue({
        id: 'remove-plan',
        componentId: 'deepseek-harness',
        name: 'DeepSeek Harness',
        effects: [{ action: 'delete-anything', path: 'C:\\outside', description: '未知操作', recoverable: false }],
        warning: '',
        createdAt: '',
        expiresAt: '',
      }),
      vi.fn(),
    )

    await expect(createRemovalPlan('deepseek-harness')).rejects.toThrow('移除服务返回了无效操作')
  })
})
