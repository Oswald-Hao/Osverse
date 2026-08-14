import { useState } from 'react'

import type { ProxyProtocol } from './domain'
import { useProxyProbe } from './hooks/useProxyProbe'

const protocolLabels: Record<ProxyProtocol, string> = {
  http: 'HTTP',
  'https-connect': 'HTTPS CONNECT',
  socks5: 'SOCKS5',
}

export default function ProxyPanel() {
  const [portText, setPortText] = useState('7890')
  const [validation, setValidation] = useState<string | null>(null)
  const { phase, result, error, probe, useDirect } = useProxyProbe()

  const submit = () => {
    const port = Number(portText)
    if (!/^\d+$/.test(portText) || !Number.isInteger(port) || port < 1 || port > 65535) {
      setValidation('请输入 1 到 65535 之间的端口号')
      return
    }
    setValidation(null)
    probe(port)
  }

  const direct = () => {
    setValidation(null)
    useDirect()
  }

  return (
    <section className="proxy-panel" aria-labelledby="proxy-title">
      <div className="proxy-panel__heading">
        <div>
          <p className="card-kicker">NETWORK</p>
          <h3 id="proxy-title">下载连接</h3>
          <p>仅探测本机 127.0.0.1，不会修改系统代理。</p>
        </div>
        <span className={`connection-state connection-state--${phase}`}>
          {phase === 'proxy' ? '代理已启用' : phase === 'probing' ? '探测中' : '直连'}
        </span>
      </div>

      <div className="proxy-panel__controls">
        <label htmlFor="proxy-port">
          本地代理端口
          <span className="proxy-address">127.0.0.1 :</span>
        </label>
        <input
          id="proxy-port"
          inputMode="numeric"
          autoComplete="off"
          value={portText}
          onChange={(event) => setPortText(event.target.value.trim())}
          aria-describedby="proxy-help"
          disabled={phase === 'probing'}
        />
        <button className="primary-button" type="button" onClick={submit} disabled={phase === 'probing'}>
          {phase === 'probing' ? '正在探测' : '探测并使用'}
        </button>
        <button className="secondary-button" type="button" onClick={direct} disabled={phase === 'probing'}>
          使用直连
        </button>
      </div>
      <p id="proxy-help" className="proxy-panel__help">
        常见端口：Clash 7890、sing-box 2080。Osverse 会自动识别协议。
      </p>

      {(validation || error) && (
        <div className="notice notice--error proxy-panel__notice" role="alert">
          {validation ?? error}
        </div>
      )}

      {result && (
        <div className="proxy-attempts" aria-label="代理探测结果">
          {result.attempts.map((attempt) => (
            <article key={attempt.protocol}>
              <div>
                <strong>{protocolLabels[attempt.protocol]}</strong>
                <span>{attempt.available ? '可用' : '不可用'}</span>
              </div>
              <p>{attempt.message}</p>
              {attempt.available && <small>{attempt.latencyMillis} ms</small>}
            </article>
          ))}
        </div>
      )}
    </section>
  )
}
