const items = [
  { icon: '⌁', label: '环境概览' },
  { icon: '⌘', label: '工具状态' },
  { icon: '◈', label: '系统信息' },
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
      <section className="sidebar__overview" aria-labelledby="overview-title">
        <h2 className="visually-hidden" id="overview-title">
          状态概览
        </h2>
        <ul>
          {items.map((item) => (
            <li key={item.label} aria-label={item.label}>
              <span className="sidebar__icon" aria-hidden="true">
                {item.icon}
              </span>
              <span className="sidebar__label">{item.label}</span>
            </li>
          ))}
        </ul>
      </section>
      <p className="sidebar__phase">Phase 1 · 只读扫描</p>
    </aside>
  )
}

export default Sidebar
