export type AppView = 'overview' | 'api' | 'history' | 'settings'

const items: Array<{ icon: string; label: string; view: AppView }> = [
  { icon: '⌁', label: '总览', view: 'overview' },
  { icon: '◈', label: 'API 配置', view: 'api' },
  { icon: '↺', label: '安装记录', view: 'history' },
  { icon: '⚙', label: '设置', view: 'settings' },
]

function Sidebar({ active = 'overview', onNavigate }: { active?: AppView; onNavigate?: (view: AppView) => void }) {
  return (
    <aside className="sidebar">
      <div className="brand">
        <span className="brand__mark" aria-hidden="true">O</span>
        <h1>Osverse</h1>
      </div>
      <nav className="sidebar__overview" aria-label="主导航">
        <ul>
          {items.map((item) => (
            <li key={item.view} className={active === item.view ? 'sidebar__item--active' : ''}>
              <button type="button" onClick={() => onNavigate?.(item.view)} aria-current={active === item.view ? 'page' : undefined}>
                <span className="sidebar__icon" aria-hidden="true">{item.icon}</span>
                <span className="sidebar__label">{item.label}</span>
              </button>
            </li>
          ))}
        </ul>
      </nav>
      <p className="sidebar__phase">Linux Beta · 本地优先</p>
    </aside>
  )
}

export default Sidebar
