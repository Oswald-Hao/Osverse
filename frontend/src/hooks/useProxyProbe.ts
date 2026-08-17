import { useCallback, useEffect, useRef, useState } from 'react'

import type { ProxyResult, ProxySelection } from '../domain'
import { getCurrentProxySelection, probeProxy, useDirectConnection } from '../services/osverse'

export type ProxyPhase = 'direct' | 'probing' | 'proxy' | 'error'

export interface ProxyProbeState {
  phase: ProxyPhase
  result: ProxyResult | null
  selection: ProxySelection | null
  error: string | null
  probe: (port: number) => void
  useDirect: () => void
}

function publicErrorMessage(reason: unknown): string {
  if (typeof reason === 'string' && reason.trim() !== '') {
    return reason
  }
  if (reason instanceof Error && reason.message.trim() !== '') {
    return reason.message
  }
  return '代理探测失败，请重试'
}

export function useProxyProbe(): ProxyProbeState {
  const [phase, setPhase] = useState<ProxyPhase>('direct')
  const [result, setResult] = useState<ProxyResult | null>(null)
  const [selection, setSelection] = useState<ProxySelection | null>(null)
  const [error, setError] = useState<string | null>(null)
  const mounted = useRef(false)
  const generation = useRef(0)

  const probe = useCallback((port: number) => {
    if (!mounted.current) return

    const requestGeneration = ++generation.current
    setPhase('probing')
    setResult(null)
    setSelection(null)
    setError(null)

    let request: Promise<ProxyResult>
    try {
      request = probeProxy(port)
    } catch (reason) {
      request = Promise.reject(reason)
    }

    void request.then(
      (nextResult) => {
        if (!mounted.current || generation.current !== requestGeneration) return
        setResult(nextResult)
        setSelection(nextResult.reachable && nextResult.recommended !== ''
          ? { protocol: nextResult.recommended, port: nextResult.port }
          : null)
        setPhase(nextResult.reachable ? 'proxy' : 'error')
        setError(nextResult.reachable ? null : '未发现可安全承载 HTTPS 下载的本地代理')
      },
      (reason: unknown) => {
        if (!mounted.current || generation.current !== requestGeneration) return
        void getCurrentProxySelection().then(
          (current) => {
            if (!mounted.current || generation.current !== requestGeneration) return
            setPhase(current ? 'proxy' : 'error')
            setResult(null)
            setSelection(current)
            setError(publicErrorMessage(reason))
          },
          () => {
            if (!mounted.current || generation.current !== requestGeneration) return
            setPhase('error')
            setResult(null)
            setSelection(null)
            setError(publicErrorMessage(reason))
          },
        )
      },
    )
  }, [])

  const useDirect = useCallback(() => {
    if (!mounted.current) return
    const requestGeneration = ++generation.current
    setPhase('probing')
    setResult(null)
    setError(null)
    void useDirectConnection().then(
      () => {
        if (!mounted.current || generation.current !== requestGeneration) return
        setPhase('direct')
        setSelection(null)
      },
      (reason: unknown) => {
        if (!mounted.current || generation.current !== requestGeneration) return
        setPhase(selection ? 'proxy' : 'error')
        setError(publicErrorMessage(reason))
      },
    )
  }, [selection])

  useEffect(() => {
    mounted.current = true
    const requestGeneration = ++generation.current
    void getCurrentProxySelection().then(
      (current) => {
        if (!mounted.current || generation.current !== requestGeneration) return
        setSelection(current)
        setPhase(current ? 'proxy' : 'direct')
      },
      (reason: unknown) => {
        if (!mounted.current || generation.current !== requestGeneration) return
        setPhase('error')
        setError(publicErrorMessage(reason))
      },
    )
    return () => {
      mounted.current = false
      ++generation.current
    }
  }, [])

  return { phase, result, selection, error, probe, useDirect }
}
