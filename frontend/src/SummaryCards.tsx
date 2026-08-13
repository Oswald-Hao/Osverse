interface SummaryCardsProps {
  ready: number
  total: number
  needsAttention: number
}

const summaries = [
  { key: 'ready', label: '已就绪', note: '可以直接使用', tone: 'success' },
  { key: 'total', label: '工具总数', note: '已纳入本次扫描', tone: 'neutral' },
  { key: 'needsAttention', label: '需要关注', note: '建议查看详情', tone: 'warning' },
] as const

function SummaryCards(props: SummaryCardsProps) {
  return (
    <section className="summary-grid" aria-label="扫描摘要">
      {summaries.map((summary) => (
        <article
          className={`summary-card summary-card--${summary.tone}`}
          key={summary.key}
          aria-label={`${summary.label} ${props[summary.key]}`}
        >
          <div>
            <p>{summary.label}</p>
            <span>{summary.note}</span>
          </div>
          <strong>{props[summary.key]}</strong>
        </article>
      ))}
    </section>
  )
}

export default SummaryCards
