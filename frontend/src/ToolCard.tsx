import type { Component } from './domain'
import StatusBadge from './StatusBadge'

const actionLabels: Record<Component['status'], string> = {
  detecting: '配置',
  missing: '安装',
  installed: '配置',
  update_available: '更新',
  conflict: '配置',
  unsupported: '安装',
  broken: '配置',
  installing: '安装',
  failed: '配置',
}

const installableComponents = new Set([
  'claude-code', 'codex-cli', 'opencode-cli',
  'opencode-desktop', 'cc-switch', 'cockpit-tools',
])

function ToolCard({ component, onInstall, onLaunch }: {
  component: Component
  onInstall?: (id: string) => void
  onLaunch?: (id: string) => void
}) {
  const action = actionLabels[component.status]
  const canInstall = installableComponents.has(component.id) &&
    ['missing', 'broken', 'failed', 'update_available'].includes(component.status)
  const canLaunch = component.category !== 'Core CLI' && component.status === 'installed' &&
    component.installations.some((installation) => installation.managed)

  return (
    <li>
      <article className="tool-card">
        <div className="tool-card__topline">
          <div>
            <p className="tool-card__id">{component.id}</p>
            <h4>{component.name}</h4>
          </div>
          <StatusBadge status={component.status} />
        </div>

        <p className="tool-card__message">{component.message || '暂无附加信息'}</p>

        {component.installations.length > 0 && (
          <ul className="installations" aria-label={`${component.name} 安装位置`}>
            {component.installations.map((installation, index) => (
              <li key={`${installation.path}-${installation.resolvedPath}-${index}`}>
                <dl>
                  <div>
                    <dt>安装路径</dt>
                    <dd>
                      <code>{installation.path}</code>
                    </dd>
                  </div>
                  <div>
                    <dt>解析目标</dt>
                    <dd>
                      <code>{installation.resolvedPath || installation.path}</code>
                    </dd>
                  </div>
                  {installation.version && (
                    <div>
                      <dt>版本</dt>
                      <dd>{installation.version}</dd>
                    </div>
                  )}
                </dl>
              </li>
            ))}
          </ul>
        )}

        <div className="tool-card__footer">
          <span>最低要求：{component.minimumOS}</span>
          <button
            type="button"
            disabled={!canInstall && !canLaunch}
            onClick={() => canInstall ? onInstall?.(component.id) : canLaunch ? onLaunch?.(component.id) : undefined}
          >
            {canLaunch ? '启动' : action}
            <span>{canInstall ? '官方校验安装' : canLaunch ? 'Osverse 管理的应用' : component.status === 'installed' ? '当前已可用' : '暂不可用'}</span>
          </button>
        </div>
      </article>
    </li>
  )
}

export default ToolCard
