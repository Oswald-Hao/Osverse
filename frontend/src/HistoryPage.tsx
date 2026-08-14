import { useEffect, useRef, useState } from 'react'

import type { HistoryEntry } from './domain'
import { clearHistory, listHistory } from './services/osverse'

const actionLabels: Record<string, string> = {
  install: '安装 / 更新', configure: '应用 API 配置',
  'profile-save': '保存 API 档案', 'profile-delete': '删除 API 档案',
}
const statusLabels: Record<HistoryEntry['status'], string> = {
  completed: '已完成', failed: '未完成', canceled: '已取消',
}

function formatTime(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '时间未知'
  return new Intl.DateTimeFormat('zh-CN', { dateStyle: 'medium', timeStyle: 'medium', hourCycle: 'h23' }).format(date)
}

function publicMessage(reason: unknown) {
  return reason instanceof Error && reason.message ? reason.message : '无法读取历史记录'
}

export default function HistoryPage() {
  const [entries, setEntries] = useState<HistoryEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [confirmClear, setConfirmClear] = useState(false)
  const mounted = useRef(false)
  const generation = useRef(0)

  const load = async () => {
    const request = ++generation.current
    setLoading(true)
    try {
      const next = await listHistory()
      if (mounted.current && request === generation.current) { setEntries(next); setError(null) }
    } catch (reason) {
      if (mounted.current && request === generation.current) setError(publicMessage(reason))
    } finally {
      if (mounted.current && request === generation.current) setLoading(false)
    }
  }

  useEffect(() => {
    mounted.current = true
    void load()
    return () => { mounted.current = false; ++generation.current }
  }, [])

  const clear = async () => {
    if (!confirmClear) { setConfirmClear(true); return }
    try {
      await clearHistory()
      setEntries([]); setConfirmClear(false); setError(null)
    } catch (reason) { setError(publicMessage(reason)) }
  }

  return (
    <>
      <header className="dashboard-header">
        <div><p className="eyebrow">LOCAL LEDGER</p><h2>安装记录</h2><p>只记录脱敏后的安装和配置结果，最多保留 200 条。</p></div>
        {entries.length > 0 && <div className="history-actions">
          {confirmClear && <button className="secondary-button" type="button" onClick={() => setConfirmClear(false)}>取消</button>}
          <button className={confirmClear ? 'danger-button' : 'secondary-button'} type="button" onClick={() => void clear()}>{confirmClear ? '确认清除' : '清除记录'}</button>
        </div>}
      </header>
      {error && <div className="notice notice--error" role="alert">{error}</div>}
      {loading ? <div className="notice notice--progress" role="status">正在读取本地记录…</div> : entries.length === 0 ? (
        <section className="system-card history-empty"><h3>还没有操作记录</h3><p>安装工具或应用 API 配置后，脱敏结果会显示在这里。</p></section>
      ) : (
        <ol className="history-list">
          {entries.map((entry) => <li key={entry.id} className={`history-entry history-entry--${entry.status}`}>
            <div><span className="history-entry__status">{statusLabels[entry.status]}</span><time dateTime={entry.createdAt}>{formatTime(entry.createdAt)}</time></div>
            <h3>{entry.name}</h3>
            <p>{actionLabels[entry.action] ?? entry.action} · {entry.message}</p>
            <code>{entry.componentId}</code>
          </li>)}
        </ol>
      )}
    </>
  )
}
