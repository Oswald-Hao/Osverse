import type { AppUpdateController } from './hooks/useAppUpdate'

function bytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '未知大小'
  const units = ['B', 'KB', 'MB', 'GB']
  let amount = value
  let index = 0
  while (amount >= 1024 && index < units.length - 1) { amount /= 1024; index++ }
  return `${amount.toFixed(index === 0 ? 0 : 1)} ${units[index]}`
}

export default function UpdateDialog({ update }: { update: AppUpdateController }) {
  if (!update.visible) return null
  const busy = update.phase === 'checking' || update.phase === 'installing'
  const info = update.info
  return (
    <div className="dialog-backdrop update-backdrop" role="presentation">
      <section className="install-dialog update-dialog" role="dialog" aria-modal="true" aria-labelledby="update-title">
        <div className="install-dialog__header">
          <div>
            <p className="card-kicker">OSVERSE UPDATE</p>
            <h2 id="update-title">{info?.available ? `发现 Osverse ${info.latestVersion}` : '检查 Osverse 更新'}</h2>
          </div>
          {!busy && <button className="dialog-close" type="button" aria-label="稍后更新" onClick={update.dismiss}>×</button>}
        </div>
        {update.phase === 'checking' && <div className="notice notice--progress" role="status"><span className="notice__spinner" /><div><strong>正在检查更新</strong><span>正在读取 Osverse 官方 GitHub Release。</span></div></div>}
        {info?.available && <>
          <div className="update-version-row"><span>{info.currentVersion}</span><b aria-hidden="true">→</b><strong>{info.latestVersion}</strong><em>{bytes(info.downloadBytes)}</em></div>
          <div className="update-notes"><h3>本次更新</h3><pre>{info.releaseNotes || '本次发布未提供详细更新说明。'}</pre></div>
          <div className="notice notice--progress"><div><strong>安全更新</strong><span>{info.message}；安装前会核对固定仓库、文件长度和 SHA-256。</span></div></div>
        </>}
        {update.phase === 'idle' && <div className="install-result install-result--success"><strong>当前已是最新版本</strong><p>{info?.message || '没有发现可用更新。'}</p></div>}
        {update.phase === 'complete' && <div className="install-result install-result--success"><strong>更新程序已启动</strong><p>{update.resultMessage}</p></div>}
        {update.phase === 'error' && <div className="notice notice--error" role="alert"><strong>更新未完成</strong><span>{update.error}</span></div>}
        <div className="install-dialog__actions">
          {!busy && <button className="secondary-button" type="button" onClick={update.dismiss}>{update.phase === 'complete' ? '关闭' : '稍后'}</button>}
          {info?.available && update.phase !== 'complete' && <button className="primary-button" type="button" disabled={busy || !info.canInstall} onClick={() => void update.install()}>{update.phase === 'installing' ? '下载并校验中…' : info.canInstall ? '下载并更新' : '当前平台暂不可安装'}</button>}
          {(update.phase === 'idle' || update.phase === 'error') && <button className="primary-button" type="button" disabled={busy} onClick={() => void update.check(true)}>重新检查</button>}
        </div>
      </section>
    </div>
  )
}
