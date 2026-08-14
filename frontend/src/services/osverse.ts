import {
  ProbeProxy,
  CancelInstallTask,
  CreateInstallPlan,
  GetInstallTask,
  ScanEnvironment,
  UseDirectConnection,
  StartInstall,
} from '../../wailsjs/go/main/App'
import type { domain as generated } from '../../wailsjs/go/models'
import type {
  ComponentStatus,
  EnvironmentSnapshot,
  InstallPlan,
  InstallTask,
  InstallTaskPhase,
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
type PlanFunction = (componentId: string) => Promise<unknown>
type StartFunction = (planId: string) => Promise<unknown>
type TaskFunction = (taskId: string) => Promise<unknown>
type CancelFunction = (taskId: string) => Promise<void>

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
let createPlan: PlanFunction = CreateInstallPlan
let startInstall: StartFunction = StartInstall
let getTask: TaskFunction = GetInstallTask
let cancelTask: CancelFunction = CancelInstallTask

const proxyProtocols = new Set<string>(['http', 'https-connect', 'socks5'])
const installTaskPhases = new Set<string>([
  'queued', 'downloading', 'verifying', 'committing', 'completed', 'failed', 'canceled',
])

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

function stringValue(value: unknown): string {
  return typeof value === 'string' ? value : String(value ?? '')
}

function numberValue(value: unknown): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) {
    throw new Error('安装服务返回了无效的数值')
  }
  return value
}

function installTaskPhase(value: unknown): InstallTaskPhase {
  const phase = stringValue(value)
  if (!installTaskPhases.has(phase)) {
    throw new Error('安装服务返回了无效的任务状态')
  }
  return phase as InstallTaskPhase
}

export async function createInstallPlan(componentId: string): Promise<InstallPlan> {
  const result = await createPlan(componentId) as Record<string, unknown>
  const changes = Array.isArray(result.changes) ? result.changes : []
  return {
    id: stringValue(result.id),
    componentId: stringValue(result.componentId),
    name: stringValue(result.name),
    command: stringValue(result.command),
    version: stringValue(result.version),
    downloadBytes: numberValue(result.downloadBytes),
    changes: changes.map((raw) => {
      const change = raw as Record<string, unknown>
      return {
        kind: stringValue(change.kind),
        path: stringValue(change.path),
        description: stringValue(change.description),
      }
    }),
    createdAt: stringValue(result.createdAt),
    expiresAt: stringValue(result.expiresAt),
  }
}

function normalizeInstallTask(raw: unknown): InstallTask {
  const result = raw as Record<string, unknown>
  const progress = numberValue(result.progress)
  if (!Number.isInteger(progress) || progress < 0 || progress > 100) {
    throw new Error('安装服务返回了无效的进度')
  }
  return {
    id: stringValue(result.id),
    planId: stringValue(result.planId),
    componentId: stringValue(result.componentId),
    phase: installTaskPhase(result.phase),
    progress,
    message: stringValue(result.message),
    errorCode: stringValue(result.errorCode),
    startedAt: stringValue(result.startedAt),
    finishedAt: stringValue(result.finishedAt),
  }
}

export async function beginInstall(planId: string): Promise<InstallTask> {
  return normalizeInstallTask(await startInstall(planId))
}

export async function readInstallTask(taskId: string): Promise<InstallTask> {
  return normalizeInstallTask(await getTask(taskId))
}

export async function cancelInstall(taskId: string): Promise<void> {
  await cancelTask(taskId)
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

export function setInstallOperationsForTests(operations: {
  createPlan: PlanFunction
  startInstall: StartFunction
  getTask: TaskFunction
  cancelTask?: CancelFunction
}): void {
  if (!testSeamEnabled()) {
    throw new Error('The install test seam is unavailable outside tests')
  }
  createPlan = operations.createPlan
  startInstall = operations.startInstall
  getTask = operations.getTask
  cancelTask = operations.cancelTask ?? (() => Promise.resolve())
}

export function resetInstallOperationsForTests(): void {
  if (!testSeamEnabled()) {
    throw new Error('The install test seam is unavailable outside tests')
  }
  createPlan = CreateInstallPlan
  startInstall = StartInstall
  getTask = GetInstallTask
  cancelTask = CancelInstallTask
}
