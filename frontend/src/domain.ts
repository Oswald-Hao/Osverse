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

export interface HistoryEntry {
  id: string
  operationId: string
  componentId: string
  name: string
  action: string
  status: 'completed' | 'failed' | 'canceled'
  message: string
  createdAt: string
}

export interface APIProfileInput {
  id: string
  name: string
  apiKey: string
  baseUrl: string
  model: string
  allowPrivateNetwork: boolean
}

export interface APIProfile {
  id: string
  name: string
  keyHint: string
  baseUrl: string
  model: string
  allowPrivateNetwork: boolean
  protection: string
  createdAt: string
  updatedAt: string
}

export interface APIProtocolResult {
  protocol: string
  status: string
  message: string
}

export interface APIProbeResult {
  profileId: string
  reachable: boolean
  authenticated: boolean
  protocols: APIProtocolResult[]
  message: string
  checkedAt: string
}

export interface APITargetCompatibility {
  target: string
  compatible: boolean
  reason: string
}

export interface APIApplyPlan {
  id: string
  profileId: string
  profileName: string
  keyHint: string
  effects: Array<{ target: string; path: string; description: string }>
  warning: string
  createdAt: string
  expiresAt: string
}

export interface APIApplyResult {
  target: string
  applied: boolean
  path: string
  backupPath: string
  message: string
}

export interface RemovalEffect {
  action: 'trash' | 'package'
  path: string
  description: string
  recoverable: boolean
}

export interface RemovalPlan {
  id: string
  componentId: string
  name: string
  effects: RemovalEffect[]
  warning: string
  createdAt: string
  expiresAt: string
}

export interface RemovalResult {
  planId: string
  componentId: string
  removed: boolean
  message: string
}

export interface APIApplyBatchResult {
  planId: string
  profileId: string
  results: APIApplyResult[]
  succeeded: number
  failed: number
}
