import './App.css'

import { useState } from 'react'

import Sidebar from './Sidebar'
import SummaryCards from './SummaryCards'
import ToolSection from './ToolSection'
import ProxyPanel from './ProxyPanel'
import InstallDialog from './InstallDialog'
import APIProfilesPage from './APIProfilesPage'
import type { AppView } from './Sidebar'
import { useEnvironmentScan } from './hooks/useEnvironmentScan'
import { useInstallFlow } from './hooks/useInstallFlow'
import { launchManagedApp } from './services/osverse'

const sections = [
  {
    category: 'Core CLI',
    title: '命令行工具',
    description: '终端中的核心 AI 开发工具',
  },
  {
    category: 'Desktop Applications',
    title: '桌面应用',
    description: '本机安装的图形化客户端',
  },
  {
    category: 'Management Tools',
    title: '管理工具',
    description: '用于切换和管理开发环境的工具',
  },
] as const

function ScanNotice({ hasSnapshot }: { hasSnapshot: boolean }) {
  return (
    <div className="notice notice--progress" role="status">
      <span className="notice__spinner" aria-hidden="true" />
      <div>
        <strong>{hasSnapshot ? '正在刷新环境状态' : '正在扫描环境'}</strong>
        <span>
          {hasSnapshot
            ? '当前结果会保留到刷新完成。'
            : '正在读取系统和工具信息，请稍候。'}
        </span>
      </div>
    </div>
  )
}

function EmptyState({
  error,
  onRetry,
}: {
  error: string
  onRetry: () => void
}) {
  return (
    <section className="empty-state" aria-labelledby="scan-error-title">
      <div className="empty-state__mark" aria-hidden="true">
        !
      </div>
      <h2 id="scan-error-title">暂时无法读取环境状态</h2>
      <div className="notice notice--error" role="alert">{error}</div>
      <button className="primary-button" type="button" onClick={onRetry}>
        重新扫描
      </button>
    </section>
  )
}

function formatScanTime(scannedAt: string) {
  const instant = new Date(scannedAt)
  if (Number.isNaN(instant.getTime())) {
    return {
      date: '扫描时间未知',
      time: '',
      dateTime: undefined,
    }
  }

  return {
    date: new Intl.DateTimeFormat('zh-CN', {
      year: 'numeric',
      month: 'long',
      day: 'numeric',
    }).format(instant),
    time: new Intl.DateTimeFormat('zh-CN', {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
      hourCycle: 'h23',
    }).format(instant),
    dateTime: scannedAt,
  }
}

function App() {
  const [view, setView] = useState<AppView>('overview')
  const { snapshot, phase, error, refresh } = useEnvironmentScan()
  const install = useInstallFlow(refresh)
  const [launchNotice, setLaunchNotice] = useState<string | null>(null)
  const isScanning = phase === 'scanning'

  if (view === 'api') {
    return <div className="app-shell"><Sidebar active={view} onNavigate={setView} /><main className="dashboard-main"><APIProfilesPage /></main></div>
  }

  if (view === 'history' || view === 'settings') {
    const isHistory = view === 'history'
    return (
      <div className="app-shell">
        <Sidebar active={view} onNavigate={setView} />
        <main className="dashboard-main">
          <header className="dashboard-header"><div><p className="eyebrow">OSVERSE</p><h2>{isHistory ? '安装记录' : '设置'}</h2><p>{isHistory ? '安装和配置事务的脱敏结果。' : '本地存储、网络和更新策略。'}</p></div></header>
          <section className="system-card placeholder-page"><div><h3>{isHistory ? '记录功能正在接入持久化任务日志' : '所有设置均保持本地优先'}</h3><p>{isHistory ? '当前会话的安装进度已可追踪；持久记录将在 Linux 发布门禁前完成。' : '代理仅作用于 Osverse；API 档案使用本地 AES-256-GCM 加密。'}</p></div></section>
        </main>
      </div>
    )
  }

  if (!snapshot && phase === 'error') {
    return (
      <div className="app-shell">
        <Sidebar active={view} onNavigate={setView} />
        <main className="dashboard-main">
          <EmptyState error={error ?? '环境扫描失败，请重试'} onRetry={refresh} />
        </main>
      </div>
    )
  }

  if (!snapshot) {
    return (
      <div className="app-shell">
        <Sidebar active={view} onNavigate={setView} />
        <main className="dashboard-main dashboard-main--centered">
          <ScanNotice hasSnapshot={false} />
        </main>
      </div>
    )
  }

  const scanned = formatScanTime(snapshot.scannedAt)
  const systemSupport = snapshot.system.supported ? '支持' : '不支持'

  return (
    <div className="app-shell">
      <Sidebar active={view} onNavigate={setView} />
      <main className="dashboard-main">
        <header className="dashboard-header">
          <div>
            <p className="eyebrow" lang="en">SYSTEM OVERVIEW</p>
            <h2>环境状态</h2>
            <p>掌握本机 AI 开发工具的就绪情况。</p>
          </div>
          <button
            className="refresh-button"
            type="button"
            onClick={refresh}
            disabled={isScanning}
            aria-label="刷新环境状态"
          >
            <span aria-hidden="true">↻</span>
            {isScanning ? '刷新中' : '刷新'}
          </button>
        </header>

        {isScanning && <ScanNotice hasSnapshot />}
        {phase === 'error' && (
          <div className="notice notice--error" role="alert">
            <strong>刷新未完成</strong>
            <span>{error ?? '环境扫描失败，请重试'}</span>
          </div>
        )}

        <section className="system-card" aria-labelledby="system-title">
          <div className="system-card__intro">
            <span className="system-card__orb" aria-hidden="true">
              ◎
            </span>
            <div>
              <p className="card-kicker">当前系统</p>
              <h3 id="system-title">{snapshot.system.distribution}</h3>
              <p className="scan-time">
                上次扫描：
                <time dateTime={scanned.dateTime}>{scanned.date}</time>
                {scanned.time && <span>{scanned.time}</span>}
              </p>
            </div>
          </div>
          <dl className="system-facts">
            <div>
              <dt>版本</dt>
              <dd>{snapshot.system.version}</dd>
            </div>
            <div>
              <dt>架构</dt>
              <dd>{snapshot.system.architecture}</dd>
            </div>
            <div>
              <dt lang="en">Shell</dt>
              <dd>{snapshot.system.shell}</dd>
            </div>
            <div>
              <dt>兼容性</dt>
              <dd
                className={
                  snapshot.system.supported ? 'support-ok' : 'support-bad'
                }
              >
                {systemSupport}
              </dd>
            </div>
          </dl>
          {!snapshot.system.supported && snapshot.system.unsupportedReason && (
            <p className="support-reason">{snapshot.system.unsupportedReason}</p>
          )}
        </section>

        <SummaryCards
          ready={snapshot.ready}
          total={snapshot.total}
          needsAttention={snapshot.needsAttention}
        />

        <ProxyPanel />

        {launchNotice && <div className="notice notice--error" role="alert">{launchNotice}</div>}

        <div className="tool-sections">
          {sections.map((section) => (
            <ToolSection
              key={section.category}
              title={section.title}
              description={section.description}
              components={snapshot.components.filter(
                (component) => component.category === section.category,
              )}
              onInstall={install.prepare}
              onLaunch={(id) => {
                setLaunchNotice(null)
                void launchManagedApp(id).catch((reason: unknown) => {
                  setLaunchNotice(reason instanceof Error ? reason.message : '无法启动应用')
                })
              }}
            />
          ))}
        </div>
      </main>
      <InstallDialog flow={install} />
    </div>
  )
}

export default App
