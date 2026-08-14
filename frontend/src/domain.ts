export type ComponentStatus =
  | 'detecting'
  | 'missing'
  | 'installed'
  | 'update_available'
  | 'conflict'
  | 'unsupported'
  | 'broken'
  | 'installing'
  | 'failed'

export interface SystemInfo {
  distribution: string
  version: string
  architecture: string
  shell: string
  supported: boolean
  unsupportedReason: string
}

export interface Installation {
  path: string
  resolvedPath: string
  version: string
  source: string
  managed: boolean
}

export interface Component {
  id: string
  name: string
  category: string
  status: ComponentStatus
  installations: Installation[]
  message: string
  minimumOS: string
}

export interface EnvironmentSnapshot {
  scannedAt: string
  system: SystemInfo
  components: Component[]
  ready: number
  total: number
  needsAttention: number
}

export type ProxyProtocol = 'http' | 'https-connect' | 'socks5'

export interface ProxyAttempt {
  protocol: ProxyProtocol
  available: boolean
  latencyMillis: number
  message: string
}

export interface ProxyResult {
  port: number
  reachable: boolean
  recommended: ProxyProtocol | ''
  attempts: ProxyAttempt[]
  checkedAt: string
}

export interface InstallChange {
  kind: string
  path: string
  description: string
}

export interface InstallPlan {
  id: string
  componentId: string
  name: string
  command: string
  version: string
  downloadBytes: number
  changes: InstallChange[]
  createdAt: string
  expiresAt: string
}

export type InstallTaskPhase =
  | 'queued'
  | 'downloading'
  | 'verifying'
  | 'committing'
  | 'completed'
  | 'failed'
  | 'canceled'

export interface InstallTask {
  id: string
  planId: string
  componentId: string
  phase: InstallTaskPhase
  progress: number
  message: string
  errorCode: string
  startedAt: string
  finishedAt: string
}
