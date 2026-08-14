import { useCallback, useEffect, useRef, useState } from 'react'

import type { InstallPlan, InstallTask } from '../domain'
import {
  beginInstall,
  cancelInstall,
  createInstallPlan,
  readInstallTask,
} from '../services/osverse'

export type InstallFlowPhase = 'idle' | 'planning' | 'review' | 'installing' | 'completed' | 'error'

export interface InstallFlowState {
  phase: InstallFlowPhase
  plan: InstallPlan | null
  task: InstallTask | null
  error: string | null
  prepare: (componentId: string) => void
  confirm: () => void
  cancel: () => void
  dismiss: () => void
}

const pollInterval = 400

function publicMessage(reason: unknown): string {
  if (typeof reason === 'string' && reason.trim()) return reason
  if (reason instanceof Error && reason.message.trim()) return reason.message
  return '安装操作失败，请重试'
}

export function useInstallFlow(onCompleted: () => void): InstallFlowState {
  const [phase, setPhase] = useState<InstallFlowPhase>('idle')
  const [plan, setPlan] = useState<InstallPlan | null>(null)
  const [task, setTask] = useState<InstallTask | null>(null)
  const [error, setError] = useState<string | null>(null)
  const mounted = useRef(false)
  const generation = useRef(0)
  const timer = useRef<number | null>(null)
  const completedCallback = useRef(onCompleted)
  completedCallback.current = onCompleted

  const clearPoll = useCallback(() => {
    if (timer.current !== null) window.clearTimeout(timer.current)
    timer.current = null
  }, [])

  const acceptTask = useCallback((next: InstallTask, requestGeneration: number) => {
    if (!mounted.current || generation.current !== requestGeneration) return
    setTask(next)
    if (next.phase === 'completed') {
      clearPoll()
      setPhase('completed')
      setError(null)
      completedCallback.current()
      return
    }
    if (next.phase === 'failed' || next.phase === 'canceled') {
      clearPoll()
      setPhase('error')
      setError(next.message || (next.phase === 'canceled' ? '安装已取消' : '安装失败'))
      return
    }
    setPhase('installing')
    timer.current = window.setTimeout(() => {
      void readInstallTask(next.id).then(
        (latest) => acceptTask(latest, requestGeneration),
        (reason: unknown) => {
          if (!mounted.current || generation.current !== requestGeneration) return
          setPhase('error')
          setError(publicMessage(reason))
        },
      )
    }, pollInterval)
  }, [clearPoll])

  const prepare = useCallback((componentId: string) => {
    if (!mounted.current) return
    clearPoll()
    const requestGeneration = ++generation.current
    setPhase('planning')
    setPlan(null)
    setTask(null)
    setError(null)
    void createInstallPlan(componentId).then(
      (nextPlan) => {
        if (!mounted.current || generation.current !== requestGeneration) return
        setPlan(nextPlan)
        setPhase('review')
      },
      (reason: unknown) => {
        if (!mounted.current || generation.current !== requestGeneration) return
        setPhase('error')
        setError(publicMessage(reason))
      },
    )
  }, [clearPoll])

  const confirm = useCallback(() => {
    if (!mounted.current || !plan || phase !== 'review') return
    const requestGeneration = ++generation.current
    setPhase('installing')
    setError(null)
    void beginInstall(plan.id).then(
      (nextTask) => acceptTask(nextTask, requestGeneration),
      (reason: unknown) => {
        if (!mounted.current || generation.current !== requestGeneration) return
        setPhase('error')
        setError(publicMessage(reason))
      },
    )
  }, [acceptTask, phase, plan])

  const cancel = useCallback(() => {
    if (!mounted.current || !task || phase !== 'installing') return
    void cancelInstall(task.id).catch(() => undefined)
  }, [phase, task])

  const dismiss = useCallback(() => {
    if (!mounted.current || phase === 'installing') return
    clearPoll()
    ++generation.current
    setPhase('idle')
    setPlan(null)
    setTask(null)
    setError(null)
  }, [clearPoll, phase])

  useEffect(() => {
    mounted.current = true
    return () => {
      mounted.current = false
      ++generation.current
      clearPoll()
    }
  }, [clearPoll])

  return { phase, plan, task, error, prepare, confirm, cancel, dismiss }
}
