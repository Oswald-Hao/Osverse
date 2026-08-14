import {
  ProbeProxy,
  ScanEnvironment,
  UseDirectConnection,
} from '../../wailsjs/go/main/App'
import type { domain as generated } from '../../wailsjs/go/models'
import type {
  ComponentStatus,
  EnvironmentSnapshot,
  ProxyProtocol,
  ProxyResult,
} from '../domain'

type ScanResult = generated.EnvironmentSnapshot | EnvironmentSnapshot
type ScanFunction = () => Promise<ScanResult>
type ProbeFunction = (port: number) => Promise<{
  port: number
  reachable: boolean
  recommended: string
  attempts: Array<{
    protocol: string
    available: boolean
    latencyMillis: number
    message: string
  }> | null
  checkedAt: unknown
}>
type DirectFunction = () => Promise<void>

const componentStatuses = new Set<string>([
  'detecting',
  'missing',
  'installed',
  'update_available',
  'conflict',
  'unsupported',
  'broken',
  'installing',
  'failed',
])

let scan: ScanFunction = ScanEnvironment
let probe: ProbeFunction = ProbeProxy
let direct: DirectFunction = UseDirectConnection

const proxyProtocols = new Set<string>(['http', 'https-connect', 'socks5'])

function testSeamEnabled(): boolean {
  const meta = import.meta as ImportMeta & { env: { MODE?: string } }
  return meta.env.MODE === 'test'
}

function componentStatus(value: string): ComponentStatus {
  if (!componentStatuses.has(value)) {
    throw new Error('环境扫描返回了无效的组件状态')
  }
  return value as ComponentStatus
}

function proxyProtocol(value: string): ProxyProtocol {
  if (!proxyProtocols.has(value)) {
    throw new Error('代理探测返回了无效的协议')
  }
  return value as ProxyProtocol
}

export async function scanEnvironment(): Promise<EnvironmentSnapshot> {
  const result = await scan()

  return {
    scannedAt: result.scannedAt as string,
    system: {
      distribution: result.system.distribution,
      version: result.system.version,
      architecture: result.system.architecture,
      shell: result.system.shell,
      supported: result.system.supported,
      unsupportedReason: result.system.unsupportedReason,
    },
    components: (result.components ?? []).map((component) => ({
      id: component.id,
      name: component.name,
      category: component.category,
      status: componentStatus(component.status),
      installations: (component.installations ?? []).map((installation) => ({
        path: installation.path,
        resolvedPath: installation.resolvedPath,
        version: installation.version,
        source: installation.source,
        managed: installation.managed,
      })),
      message: component.message,
      minimumOS: component.minimumOS,
    })),
    ready: result.ready,
    total: result.total,
    needsAttention: result.needsAttention,
  }
}

export async function probeProxy(port: number): Promise<ProxyResult> {
  const result = await probe(port)
  const recommended = result.recommended === ''
    ? ''
    : proxyProtocol(result.recommended)

  return {
    port: result.port,
    reachable: result.reachable,
    recommended,
    attempts: (result.attempts ?? []).map((attempt) => ({
      protocol: proxyProtocol(attempt.protocol),
      available: attempt.available,
      latencyMillis: attempt.latencyMillis,
      message: attempt.message,
    })),
    checkedAt: String(result.checkedAt ?? ''),
  }
}

export async function useDirectConnection(): Promise<void> {
  await direct()
}

// These guarded exports provide a resettable Vitest seam without making a
// runtime bridge or replaceable scanner available in production builds.
export function setScanEnvironmentForTests(testScan: ScanFunction): void {
  if (!testSeamEnabled()) {
    throw new Error('The scan test seam is unavailable outside tests')
  }
  scan = testScan
}

export function resetScanEnvironmentForTests(): void {
  if (!testSeamEnabled()) {
    throw new Error('The scan test seam is unavailable outside tests')
  }
  scan = ScanEnvironment
}

export function setProxyOperationsForTests(
  testProbe: ProbeFunction,
  testDirect: DirectFunction = () => Promise.resolve(),
): void {
  if (!testSeamEnabled()) {
    throw new Error('The proxy test seam is unavailable outside tests')
  }
  probe = testProbe
  direct = testDirect
}

export function resetProxyOperationsForTests(): void {
  if (!testSeamEnabled()) {
    throw new Error('The proxy test seam is unavailable outside tests')
  }
  probe = ProbeProxy
  direct = UseDirectConnection
}
