const protections = [
  { title: 'API 档案', value: 'AES-256-GCM', detail: 'API Key、Base URL 和模型名均加密后写入本机；密钥文件权限为 0600。' },
  { title: '代理作用域', value: '仅 Osverse', detail: '只接受 127.0.0.1 端口，系统终端和其他应用不会被修改。' },
  { title: '安装校验', value: '固定清单 + SHA-256', detail: 'CLI 与 AppImage 必须匹配内置版本、长度和摘要；Claude Desktop 由 APT 签名验证。' },
  { title: '数据收集', value: '无遥测', detail: '环境扫描、档案和脱敏历史记录均保留在本机。' },
]

export default function SettingsPage({ update }: { update: AppUpdateController }) {
  const version = update.info?.currentVersion || '正在读取'
  const updateState = update.phase === 'checking' ? '检查中' : update.info?.available ? `可更新至 ${update.info.latestVersion}` : update.phase === 'error' ? '检查失败' : '已是最新'
  return (
    <>
      <header className="dashboard-header"><div><p className="eyebrow">LOCAL POLICY</p><h2>设置</h2><p>查看当前 Linux 版本采用的本地存储、网络和更新策略。</p></div></header>
      <section className="settings-grid" aria-label="安全与隐私策略">
        {protections.map((item) => <article className="settings-card" key={item.title}>
          <p>{item.title}</p><h3>{item.value}</h3><span>{item.detail}</span>
        </article>)}
      </section>
      <section className="system-card settings-update" aria-labelledby="settings-update-title">
        <div><p className="card-kicker">APPLICATION UPDATE</p><h3 id="settings-update-title">Osverse 更新</h3><p>当前版本 {version}。更新只从 Osverse 官方 GitHub Release 下载，并在安装前校验长度与 SHA-256。</p></div>
        <div className="settings-update__action"><strong>{updateState}</strong><button className="primary-button" type="button" disabled={update.phase === 'checking' || update.phase === 'installing'} onClick={() => { if (update.info?.available) update.show(); else void update.check(true) }}>{update.info?.available ? '查看更新' : '检查更新'}</button></div>
      </section>
      <section className="system-card settings-paths">
        <div><p className="card-kicker">MANAGED PATHS</p><h3>Osverse 管理目录</h3><p>删除 Osverse 不会自动删除外部工具；应用只写入以下自有目录和经过确认的 CLI 配置文件。</p></div>
        <dl>
          <div><dt>工具与应用</dt><dd><code>~/.local/share/osverse</code></dd></div>
          <div><dt>命令入口</dt><dd><code>~/.local/bin</code></dd></div>
          <div><dt>桌面入口</dt><dd><code>~/.local/share/applications</code></dd></div>
        </dl>
      </section>
    </>
  )
}
import type { AppUpdateController } from './hooks/useAppUpdate'
