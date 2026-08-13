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
