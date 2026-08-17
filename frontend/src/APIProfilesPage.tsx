import { useEffect, useRef, useState } from 'react'

import type {
  APIApplyBatchResult,
  APIApplyPlan,
  APIProfile,
  APIProfileInput,
  APIProbeResult,
  APITargetCompatibility,
} from './domain'
import {
  applyProfilePlan,
  createAPIApplyPlan,
  deleteAPIProfile,
  getAPICompatibility,
  listAPIProfiles,
  probeAPIProfile,
  saveAPIProfile,
} from './services/osverse'

const targetNames: Record<string, string> = {
  'claude-code': 'Claude Code',
  'codex-cli': 'Codex CLI',
  'opencode-cli': 'OpenCode CLI',
  'qwen-code': 'Qwen Code',
}

const protocolNames: Record<string, string> = {
  'openai-responses': 'OpenAI Responses',
  'openai-chat': 'OpenAI Chat Completions',
  'anthropic-messages': 'Anthropic Messages',
}

const protocolStatusNames: Record<string, string> = {
  compatible: '已确认', unavailable: '不可用', unconfirmed: '未确认',
}

const emptyInput: APIProfileInput = {
  id: '', name: '', apiKey: '', baseUrl: '', model: '', allowPrivateNetwork: false,
}

function publicMessage(reason: unknown): string {
  if (reason instanceof Error && reason.message.trim()) return reason.message
  if (typeof reason === 'string' && reason.trim()) return reason
  return 'API 配置操作失败，请重试'
}

export default function APIProfilesPage() {
  const [profiles, setProfiles] = useState<APIProfile[]>([])
  const [input, setInput] = useState<APIProfileInput>(emptyInput)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [probe, setProbe] = useState<APIProbeResult | null>(null)
  const [matrix, setMatrix] = useState<APITargetCompatibility[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [plan, setPlan] = useState<APIApplyPlan | null>(null)
  const [applied, setApplied] = useState<APIApplyBatchResult | null>(null)
  const [deleteCandidate, setDeleteCandidate] = useState<string | null>(null)
  const mounted = useRef(false)
  const generation = useRef(0)
  const nameInput = useRef<HTMLInputElement>(null)
  const editing = input.id !== ''

  const load = async () => {
    const request = ++generation.current
    try {
      const next = await listAPIProfiles()
      if (mounted.current && request === generation.current) setProfiles(next)
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

  useEffect(() => {
    if (editing) nameInput.current?.focus()
  }, [editing])

  const save = async () => {
    if (!input.name.trim() || !input.apiKey.trim() || !input.baseUrl.trim() || !input.model.trim()) {
      setError('请完整填写档案名称、API Key、Base URL 和模型名')
      return
    }
    setBusy(true); setError(null); setApplied(null)
    try {
      await saveAPIProfile(input)
      setInput(emptyInput)
      await load()
    } catch (reason) {
      setError(publicMessage(reason))
    } finally { if (mounted.current) setBusy(false) }
  }

  const edit = (profile: APIProfile) => {
    setInput({
      id: profile.id,
      name: profile.name,
      apiKey: '',
      baseUrl: profile.baseUrl,
      model: profile.model,
      allowPrivateNetwork: profile.allowPrivateNetwork,
    })
    setDeleteCandidate(null); setError(null); setProbe(null); setMatrix([])
    setSelected([]); setPlan(null); setApplied(null)
  }

  const cancelEdit = () => {
    setInput(emptyInput)
    setError(null)
  }

  const inspect = async (profile: APIProfile) => {
    setBusy(true); setError(null); setProbe(null); setMatrix([]); setSelected([]); setApplied(null)
    try {
      const nextProbe = await probeAPIProfile(profile.id)
      const compatibility = await getAPICompatibility(profile.id)
      setProbe(nextProbe)
      setMatrix(compatibility)
      setSelected(compatibility.filter((item) => item.compatible).map((item) => item.target))
    } catch (reason) { setError(publicMessage(reason)) }
    finally { if (mounted.current) setBusy(false) }
  }

  const preview = async () => {
    if (!probe || selected.length === 0) return
    setBusy(true); setError(null)
    try { setPlan(await createAPIApplyPlan(probe.profileId, selected)) }
    catch (reason) { setError(publicMessage(reason)) }
    finally { if (mounted.current) setBusy(false) }
  }

  const apply = async () => {
    if (!plan) return
    setBusy(true); setError(null)
    try { setApplied(await applyProfilePlan(plan.id)); setPlan(null) }
    catch (reason) { setError(publicMessage(reason)) }
    finally { if (mounted.current) setBusy(false) }
  }

  const remove = async (id: string) => {
    setBusy(true); setError(null)
    try {
      await deleteAPIProfile(id)
      setDeleteCandidate(null); setProbe(null); setMatrix([]); setSelected([])
      if (input.id === id) setInput(emptyInput)
      await load()
    } catch (reason) { setError(publicMessage(reason)) }
    finally { if (mounted.current) setBusy(false) }
  }

  return (
    <div className="api-page">
      <header className="dashboard-header">
        <div><p className="eyebrow">ENCRYPTED PROFILES</p><h2>API 配置</h2><p>保存一次，探测兼容性后再明确选择要更新的 CLI。</p></div>
      </header>

      {error && <div className="notice notice--error" role="alert">{error}</div>}
      {applied && (
        <div className={applied.failed ? 'notice notice--error' : 'notice notice--progress'} role="status">
          已完成 {applied.succeeded} 个目标，失败 {applied.failed} 个目标。
        </div>
      )}

      <section className="api-form-card" aria-labelledby="profile-form-title">
        <div className="api-form-card__heading">
          <div><p className="card-kicker">{editing ? 'EDIT PROFILE' : 'NEW PROFILE'}</p><h3 id="profile-form-title">{editing ? '编辑加密档案' : '创建加密档案'}</h3></div>
          <span>本地 AES-256-GCM</span>
        </div>
        <div className="api-form-grid">
          <label>档案名称<input ref={nameInput} value={input.name} onChange={(event) => setInput({ ...input, name: event.target.value })} autoComplete="off" /></label>
          <label>模型名<input value={input.model} onChange={(event) => setInput({ ...input, model: event.target.value })} autoComplete="off" placeholder="例如 gpt-5.2-codex" /></label>
          <label className="api-form-grid__wide">API Key<input type="password" value={input.apiKey} onChange={(event) => setInput({ ...input, apiKey: event.target.value })} autoComplete="new-password" /></label>
          <label className="api-form-grid__wide">Base URL<input value={input.baseUrl} onChange={(event) => setInput({ ...input, baseUrl: event.target.value })} autoComplete="url" placeholder="https://api.example.com/v1" /></label>
        </div>
        {editing && <p className="base-url-help"><strong>保护凭据：</strong> Osverse 不会回显已保存的 Key。请输入当前 Key 或新 Key 后保存修改。</p>}
        <p className="base-url-help"><strong>在哪里获取？</strong> 从服务商控制台的 API 文档或“接入信息”复制，不要填写普通聊天网页地址。常见格式以 <code>/v1</code> 结尾。</p>
        <label className="private-network-confirm"><input type="checkbox" checked={input.allowPrivateNetwork} onChange={(event) => setInput({ ...input, allowPrivateNetwork: event.target.checked })} /> 我确认该地址是自己信任的本机或私有网络服务</label>
        <div className="profile-card__actions">
          <button className="primary-button" type="button" onClick={() => void save()} disabled={busy}>{editing ? '保存修改' : '保存并加密'}</button>
          {editing && <button className="secondary-button" type="button" onClick={cancelEdit} disabled={busy}>取消编辑</button>}
        </div>
      </section>

      <section className="profile-list" aria-labelledby="profile-list-title">
        <div className="tool-section__header"><div><h3 id="profile-list-title">已保存档案</h3><p>Key 仅显示末四位；更新 CLI 前必须重新探测。</p></div><span>{profiles.length}</span></div>
        {loading ? <p role="status">正在读取档案…</p> : profiles.length === 0 ? <p className="tool-section__empty">尚未创建 API 配置档案</p> : (
          <div className="profile-grid">
            {profiles.map((profile) => (
              <article className="profile-card" key={profile.id}>
                <div><h4>{profile.name}</h4><span>{profile.keyHint}</span></div>
                <dl><div><dt>Base URL</dt><dd><code>{profile.baseUrl}</code></dd></div><div><dt>模型</dt><dd>{profile.model}</dd></div></dl>
                <div className="profile-card__actions">
                  <button type="button" className="primary-button" onClick={() => void inspect(profile)} disabled={busy}>探测兼容性</button>
                  <button type="button" className="secondary-button" onClick={() => edit(profile)} disabled={busy}>编辑</button>
                  {deleteCandidate === profile.id ? (
                    <><button type="button" className="danger-button" onClick={() => void remove(profile.id)} disabled={busy}>确认删除</button><button type="button" className="secondary-button" onClick={() => setDeleteCandidate(null)}>取消</button></>
                  ) : <button type="button" className="secondary-button" onClick={() => setDeleteCandidate(profile.id)}>删除</button>}
                </div>
              </article>
            ))}
          </div>
        )}
      </section>

      {probe && (
        <section className="compatibility-card" aria-labelledby="compatibility-title">
          <div><h3 id="compatibility-title">兼容矩阵</h3><span className={probe.authenticated ? 'support-ok' : 'support-bad'}>{probe.authenticated ? '凭据已验证' : '凭据未验证'}</span></div>
          <p>{probe.message}</p>
          <div className="protocol-details" role="region" aria-label="协议探测详情">
            {probe.protocols.map((protocol) => (
              <article key={protocol.protocol} className={`protocol-details__item protocol-details__item--${protocol.status}`}>
                <div><strong>{protocolNames[protocol.protocol] ?? protocol.protocol}</strong><span>{protocolStatusNames[protocol.status] ?? protocol.status}</span></div>
                <small>{protocol.message}</small>
              </article>
            ))}
          </div>
          <div className="compatibility-grid">
            {matrix.map((item) => (
              <label key={item.target} className={item.compatible ? '' : 'compatibility-disabled'}>
                <input type="checkbox" disabled={!item.compatible} checked={selected.includes(item.target)} onChange={(event) => setSelected(event.target.checked ? [...selected, item.target] : selected.filter((target) => target !== item.target))} />
                <span><strong>{targetNames[item.target] ?? item.target}</strong><small>{item.reason}</small></span>
              </label>
            ))}
          </div>
          <button type="button" className="primary-button" onClick={() => void preview()} disabled={busy || selected.length === 0}>预览应用变更</button>
        </section>
      )}

      {plan && (
        <div className="dialog-backdrop" role="presentation">
          <section className="install-dialog" role="dialog" aria-modal="true" aria-labelledby="api-plan-title">
            <div className="install-dialog__header"><div><p className="card-kicker">CONFIG REVIEW</p><h2 id="api-plan-title">确认应用 {plan.profileName}</h2></div></div>
            <div className="notice notice--error"><strong>凭据写入提示</strong><span>{plan.warning}</span></div>
            <ol className="install-changes">{plan.effects.map((effect) => <li key={effect.target}><strong>{targetNames[effect.target]}：{effect.description}</strong><code>{effect.path}</code></li>)}</ol>
            <div className="install-dialog__actions"><button className="secondary-button" type="button" onClick={() => setPlan(null)}>取消</button><button className="primary-button" type="button" onClick={() => void apply()} disabled={busy}>确认写入</button></div>
          </section>
        </div>
      )}
    </div>
  )
}
