import type { ComponentStatus } from './domain'

const statusDetails: Record<ComponentStatus, { label: string; tone: string }> = {
  detecting: { label: '检测中', tone: 'neutral' },
  missing: { label: '未安装', tone: 'muted' },
  installed: { label: '已安装', tone: 'success' },
  update_available: { label: '可更新', tone: 'info' },
  conflict: { label: '安装冲突', tone: 'warning' },
  unsupported: { label: '系统不支持', tone: 'danger' },
  broken: { label: '安装异常', tone: 'danger' },
  installing: { label: '安装中', tone: 'info' },
  failed: { label: '检测失败', tone: 'danger' },
}

function StatusBadge({ status }: { status: ComponentStatus }) {
  const detail = statusDetails[status]

  return <span className={`status-badge status-badge--${detail.tone}`}>{detail.label}</span>
}

export default StatusBadge
