import { cleanup, render } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'

import ToolIcon from './ToolIcon'

afterEach(cleanup)

it('provides a local icon for every supported component', () => {
  const ids = [
    'claude-code', 'codex-cli', 'opencode-cli', 'deepseek-harness', 'qwen-code', 'github-copilot-cli',
    'claude-desktop', 'chatgpt-desktop', 'codex-desktop', 'opencode-desktop', 'cc-switch', 'cockpit-tools',
  ]
  const { container } = render(<>{ids.map((id) => <ToolIcon key={id} id={id} />)}</>)
  expect(container.querySelectorAll('.tool-icon')).toHaveLength(ids.length)
  for (const id of ids) {
    const icon = container.querySelector(`[data-tool-icon="${id}"]`)
    expect(icon).toBeTruthy()
    expect(icon?.querySelector('svg path')).toHaveAttribute('d')
  }
  expect(container.querySelector('img')).toBeNull()
})
