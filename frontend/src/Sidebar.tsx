const items = [
  { icon: '⌁', iconName: '概览图标', label: '环境概览' },
  { icon: '⌘', iconName: '工具图标', label: '工具状态' },
  { icon: '◈', iconName: '系统图标', label: '系统信息' },
] as const

function Sidebar() {
  return (
    <aside className="sidebar">
      <div className="brand">
        <span className="brand__mark" aria-hidden="true">
          O
        </span>
        <h1>Osverse</h1>
      </div>
      <nav aria-label="仪表盘栏目">
        <ul>
          {items.map((item, index) => (
            <li key={item.label} className={index === 0 ? 'sidebar__current' : ''}>
              <span
                className="sidebar__icon"
                role="img"
                aria-label={item.iconName}
              >
                {item.icon}
              </span>
              <span className="sidebar__label">{item.label}</span>
            </li>
          ))}
        </ul>
      </nav>
      <p className="sidebar__phase">Phase 1 · 只读扫描</p>
    </aside>
  )
}

export default Sidebar
