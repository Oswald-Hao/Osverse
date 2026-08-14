import './App.css'

import { useState } from 'react'

import Sidebar from './Sidebar'
import SummaryCards from './SummaryCards'
import ToolSection from './ToolSection'
import ProxyPanel from './ProxyPanel'
import InstallDialog from './InstallDialog'
import APIProfilesPage from './APIProfilesPage'
import HistoryPage from './HistoryPage'
import SettingsPage from './SettingsPage'
import type { AppView } from './Sidebar'
import type { RemovalPlan } from './domain'
import { useEnvironmentScan } from './hooks/useEnvironmentScan'
import { useInstallFlow } from './hooks/useInstallFlow'
import { createRemovalPlan, launchComponent, removeComponent } from './services/osverse'

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
  const [removalPlan, setRemovalPlan] = useState<RemovalPlan | null>(null)
  const [removalBusy, setRemovalBusy] = useState(false)
  const isScanning = phase === 'scanning'

  if (view === 'api') {
    return <div className="app-shell"><Sidebar active={view} onNavigate={setView} /><main className="dashboard-main"><APIProfilesPage /></main></div>
  }

  if (view === 'history' || view === 'settings') {
    return (
      <div className="app-shell">
        <Sidebar active={view} onNavigate={setView} />
        <main className="dashboard-main">
          {view === 'history' ? <HistoryPage /> : <SettingsPage />}
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
              onLaunch={(id, installationPath) => {
                setLaunchNotice(null)
                void launchComponent(id, installationPath).catch((reason: unknown) => {
                  setLaunchNotice(reason instanceof Error ? reason.message : '无法启动应用')
                })
              }}
              onRemove={(id) => {
                setLaunchNotice(null)
                setRemovalBusy(true)
                void createRemovalPlan(id).then(setRemovalPlan).catch((reason: unknown) => {
                  setLaunchNotice(reason instanceof Error ? reason.message : '无法创建移除计划')
                }).finally(() => setRemovalBusy(false))
              }}
            />
          ))}
        </div>
      </main>
      <InstallDialog flow={install} />
      {removalPlan && (
        <div className="dialog-backdrop" role="presentation">
          <section className="install-dialog" role="dialog" aria-modal="true" aria-labelledby="removal-plan-title">
            <div className="install-dialog__header">
              <div><p className="card-kicker">REMOVAL REVIEW</p><h2 id="removal-plan-title">确认移除 {removalPlan.name}</h2></div>
            </div>
            <div className="notice notice--error"><strong>请检查影响范围</strong><span>{removalPlan.warning}</span></div>
            <ol className="install-changes">
              {removalPlan.effects.map((effect, index) => <li key={`${effect.path}-${index}`}><strong>{effect.description}</strong><code>{effect.path}</code><span>{effect.recoverable ? '可从恢复区恢复' : '由受信任卸载器执行'}</span></li>)}
            </ol>
            <div className="install-dialog__actions">
              <button className="secondary-button" type="button" onClick={() => setRemovalPlan(null)} disabled={removalBusy}>取消</button>
              <button className="danger-button" type="button" disabled={removalBusy} onClick={() => {
                setRemovalBusy(true); setLaunchNotice(null)
                void removeComponent(removalPlan.id).then(() => {
                  setRemovalPlan(null)
                  refresh()
                }).catch((reason: unknown) => {
                  setLaunchNotice(reason instanceof Error ? reason.message : '移除未完成')
                }).finally(() => setRemovalBusy(false))
              }}>确认移除</button>
            </div>
          </section>
        </div>
      )}
    </div>
  )
}

export default App
