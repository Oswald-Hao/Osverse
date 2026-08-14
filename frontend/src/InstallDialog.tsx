import type { InstallFlowState } from './hooks/useInstallFlow'

function formatBytes(bytes: number): string {
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

export default function InstallDialog({ flow }: { flow: InstallFlowState }) {
  if (flow.phase === 'idle') return null
  const busy = flow.phase === 'planning' || flow.phase === 'installing'

  return (
    <div className="dialog-backdrop" role="presentation">
      <section className="install-dialog" role="dialog" aria-modal="true" aria-labelledby="install-dialog-title">
        <div className="install-dialog__header">
          <div>
            <p className="card-kicker">VERIFIED INSTALL</p>
            <h2 id="install-dialog-title">
              {flow.phase === 'planning' ? '正在准备安装计划' : flow.plan?.name ?? '安装工具'}
            </h2>
          </div>
          {!busy && (
            <button type="button" className="dialog-close" onClick={flow.dismiss} aria-label="关闭安装窗口">×</button>
          )}
        </div>

        {flow.phase === 'planning' && <div className="install-dialog__loading" role="status">正在读取受信任的软件清单…</div>}

        {flow.plan && flow.phase === 'review' && (
          <>
            <div className="install-plan-summary">
              <span>版本 <strong>{flow.plan.version}</strong></span>
              <span>下载 <strong>{formatBytes(flow.plan.downloadBytes)}</strong></span>
              <span>命令 <strong>{flow.plan.command}</strong></span>
            </div>
            <p className="install-dialog__explain">确认后只会执行以下固定变更。不会运行 shell 安装脚本，也不会覆盖其他程序的命令。</p>
            <ol className="install-changes">
              {flow.plan.changes.map((change) => (
                <li key={`${change.kind}-${change.path}`}>
                  <strong>{change.description}</strong>
                  <code>{change.path}</code>
                </li>
              ))}
            </ol>
            <div className="install-dialog__actions">
              <button type="button" className="secondary-button" onClick={flow.dismiss}>取消</button>
              <button type="button" className="primary-button" onClick={flow.confirm}>确认安装</button>
            </div>
          </>
        )}

        {flow.phase === 'installing' && flow.task && (
          <div className="install-progress" role="status">
            <div><strong>{flow.task.message}</strong><span>{flow.task.progress}%</span></div>
            <progress max="100" value={flow.task.progress}>{flow.task.progress}%</progress>
            <button type="button" className="secondary-button" onClick={flow.cancel}>取消安装</button>
          </div>
        )}

        {flow.phase === 'installing' && !flow.task && <div className="install-dialog__loading" role="status">正在启动安装事务…</div>}

        {flow.phase === 'completed' && (
          <div className="install-result install-result--success" role="status">
            <strong>安装完成</strong>
            <p>环境状态正在刷新。新终端可直接使用该命令。</p>
            <button type="button" className="primary-button" onClick={flow.dismiss}>完成</button>
          </div>
        )}

        {flow.phase === 'error' && (
          <div className="install-result install-result--error" role="alert">
            <strong>安装未完成</strong>
            <p>{flow.error ?? '安装失败，原版本未改变'}</p>
            <button type="button" className="secondary-button" onClick={flow.dismiss}>关闭</button>
          </div>
        )}
      </section>
    </div>
  )
}
