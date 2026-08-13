import type { Component } from './domain'
import ToolCard from './ToolCard'

interface ToolSectionProps {
  title: string
  description: string
  components: Component[]
}

function ToolSection({ title, description, components }: ToolSectionProps) {
  const headingId = `section-${title}`

  return (
    <section className="tool-section" aria-labelledby={headingId}>
      <header className="tool-section__header">
        <div>
          <h3 id={headingId}>{title}</h3>
          <p>{description}</p>
        </div>
        <span aria-label={`${components.length} 个工具`}>{components.length}</span>
      </header>
      {components.length > 0 ? (
        <ul className="tool-grid">
          {components.map((component) => (
            <ToolCard key={component.id} component={component} />
          ))}
        </ul>
      ) : (
        <p className="tool-section__empty">本分类暂无扫描项</p>
      )}
    </section>
  )
}

export default ToolSection
