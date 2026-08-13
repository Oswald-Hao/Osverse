import { useCallback, useEffect, useRef, useState } from 'react'

import type { EnvironmentSnapshot } from '../domain'
import { scanEnvironment } from '../services/osverse'

export type ScanPhase = 'idle' | 'scanning' | 'ready' | 'error'

export interface EnvironmentScanState {
  snapshot: EnvironmentSnapshot | null
  phase: ScanPhase
  error: string | null
  refresh: () => void
}

const fallbackError = '环境扫描失败，请重试'

function publicErrorMessage(reason: unknown): string {
  if (typeof reason === 'string' && reason.trim() !== '') {
    return reason
  }
  if (reason instanceof Error && reason.message.trim() !== '') {
    return reason.message
  }
  return fallbackError
}

export function useEnvironmentScan(): EnvironmentScanState {
  const [snapshot, setSnapshot] = useState<EnvironmentSnapshot | null>(null)
  const [phase, setPhase] = useState<ScanPhase>('idle')
  const [error, setError] = useState<string | null>(null)
  const mounted = useRef(false)
  const generation = useRef(0)

  const refresh = useCallback(() => {
    if (!mounted.current) {
      return
    }

    const requestGeneration = ++generation.current
    setPhase('scanning')
    setError(null)

    let request: Promise<EnvironmentSnapshot>
    try {
      request = scanEnvironment()
    } catch (reason) {
      request = Promise.reject(reason)
    }

    void request.then(
      (nextSnapshot) => {
        if (!mounted.current || generation.current !== requestGeneration) {
          return
        }
        setSnapshot(nextSnapshot)
        setPhase('ready')
        setError(null)
      },
      (reason: unknown) => {
        if (!mounted.current || generation.current !== requestGeneration) {
          return
        }
        setPhase('error')
        setError(publicErrorMessage(reason))
      },
    )
  }, [])

  useEffect(() => {
    mounted.current = true
    refresh()

    return () => {
      mounted.current = false
      generation.current++
    }
  }, [refresh])

  return { snapshot, phase, error, refresh }
}
