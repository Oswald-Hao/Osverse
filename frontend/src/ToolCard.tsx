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
  'claude-desktop', 'opencode-desktop', 'cc-switch', 'cockpit-tools',
])

function ToolCard({ component, onInstall, onLaunch }: {
  component: Component
  onInstall?: (id: string) => void
  onLaunch?: (id: string) => void
}) {
  const action = actionLabels[component.status]
  const canInstall = installableComponents.has(component.id) &&
    ['missing', 'broken', 'failed', 'update_available'].includes(component.status)
  const canLaunch = ['installed', 'update_available'].includes(component.status) &&
    component.installations.length === 1

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
          <div className="tool-card__actions">
            {canInstall && <button type="button" onClick={() => onInstall?.(component.id)}>{action}<span>官方校验安装</span></button>}
            {canLaunch && <button type="button" onClick={() => onLaunch?.(component.id)}>启动<span>{component.category === 'Core CLI' ? '在终端中启动' : component.installations[0].managed ? '校验后启动' : '启动已检测应用'}</span></button>}
            {!canInstall && !canLaunch && <button type="button" disabled>{action}<span>{component.status === 'installed' ? '存在多个安装位置' : '暂不可用'}</span></button>}
          </div>
        </div>
      </article>
    </li>
  )
}

export default ToolCard
