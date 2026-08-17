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
  }
  expect(container.querySelector('[data-tool-icon="codex-cli"]')).toHaveAttribute('data-icon-family', 'codex')
  expect(container.querySelector('[data-tool-icon="codex-desktop"]')).toHaveAttribute('data-icon-family', 'codex')
  expect(container.querySelector('[data-tool-icon="chatgpt-desktop"]')).toHaveAttribute('data-icon-family', 'chatgpt')
  expect(container.querySelector('[data-tool-icon="codex-desktop"] svg')?.innerHTML)
    .not.toBe(container.querySelector('[data-tool-icon="chatgpt-desktop"] svg')?.innerHTML)

  for (const id of ['cc-switch', 'cockpit-tools']) {
    const image = container.querySelector(`[data-tool-icon="${id}"] img`)
    expect(image).toHaveAttribute('src')
    expect(image?.getAttribute('src')).not.toMatch(/^https?:/)
  }
  expect(container.querySelectorAll('img')).toHaveLength(2)
  expect(container.querySelectorAll('svg path').length).toBeGreaterThanOrEqual(ids.length - 2)
})
