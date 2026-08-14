import { useCallback, useEffect, useRef, useState } from 'react'

import type { AppUpdateInfo } from '../domain'
import { checkForAppUpdate, startAppUpdate } from '../services/update'

export type AppUpdatePhase = 'idle' | 'checking' | 'available' | 'installing' | 'complete' | 'error'

export interface AppUpdateController {
  phase: AppUpdatePhase
  info: AppUpdateInfo | null
  error: string | null
  resultMessage: string | null
  visible: boolean
  check: (manual?: boolean) => Promise<void>
  install: () => Promise<void>
  dismiss: () => void
  show: () => void
}

function message(reason: unknown): string {
  return reason instanceof Error && reason.message.trim() ? reason.message : '更新操作未完成，请稍后重试'
}

export function useAppUpdate(): AppUpdateController {
  const generation = useRef(0)
  const mounted = useRef(true)
  const [phase, setPhase] = useState<AppUpdatePhase>('idle')
  const [info, setInfo] = useState<AppUpdateInfo | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [resultMessage, setResultMessage] = useState<string | null>(null)
  const [visible, setVisible] = useState(false)

  const check = useCallback(async (manual = false) => {
    const request = ++generation.current
    setPhase('checking')
    setError(null)
    setResultMessage(null)
    if (manual) setVisible(true)
    try {
      const next = await checkForAppUpdate()
      if (!mounted.current || request !== generation.current) return
      setInfo(next)
      if (next.available) {
        setPhase('available')
        setVisible(true)
      } else {
        setPhase('idle')
      }
    } catch (reason) {
      if (!mounted.current || request !== generation.current) return
      setPhase('error')
      setError(message(reason))
      if (!manual) setVisible(false)
    }
  }, [])

  const install = useCallback(async () => {
    if (!info?.canInstall || !info.planId) return
    const request = ++generation.current
    setPhase('installing')
    setError(null)
    setVisible(true)
    try {
      const result = await startAppUpdate(info.planId)
      if (!mounted.current || request !== generation.current) return
      if (!result.started) throw new Error('更新安装器未能启动')
      setResultMessage(result.message || '更新程序已启动')
      setPhase('complete')
    } catch (reason) {
      if (!mounted.current || request !== generation.current) return
      setError(message(reason))
      setPhase('error')
    }
  }, [info])

  useEffect(() => {
    mounted.current = true
    void check(false)
    return () => {
      mounted.current = false
      generation.current++
    }
  }, [check])

  return {
    phase, info, error, resultMessage, visible, check, install,
    dismiss: () => setVisible(false),
    show: () => setVisible(true),
  }
}
