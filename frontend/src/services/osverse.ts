import {
  ApplyAPIPlan,
  ClearHistory,
  CreateAPIApplyPlan,
  DeleteAPIProfile,
  GetAPICompatibility,
  ListAPIProfiles,
  ListHistory,
  LaunchComponent,
  ProbeAPIProfile,
  ProbeProxy,
  CancelInstallTask,
  CreateInstallPlan,
  CreateRemovalPlan,
  CurrentProxySelection,
  GetInstallTask,
  ScanEnvironment,
  UseDirectConnection,
  StartInstall,
  SaveAPIProfile,
  RemoveComponent,
} from '../../wailsjs/go/main/App'
import type { domain as generated } from '../../wailsjs/go/models'
import type {
  APIApplyBatchResult,
  APIApplyPlan,
  APIProbeResult,
  APIProfile,
  APIProfileInput,
  APITargetCompatibility,
  ComponentStatus,
  EnvironmentSnapshot,
  InstallPlan,
  InstallTask,
  InstallTaskPhase,
  HistoryEntry,
  RemovalAction,
  RemovalPlan,
  RemovalResult,
  ProxyProtocol,
  ProxyResult,
  ProxySelection,
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
type CurrentProxyFunction = () => Promise<{ protocol: string; port: number }>
type PlanFunction = (componentId: string) => Promise<unknown>
type StartFunction = (planId: string) => Promise<unknown>
type TaskFunction = (taskId: string) => Promise<unknown>
type CancelFunction = (taskId: string) => Promise<void>
type ProfileOperation = (...args: never[]) => Promise<unknown>
type HistoryOperation = (...args: never[]) => Promise<unknown>
type LaunchOperation = (componentId: string, installationPath: string) => Promise<void>
type RemovalOperation = (id: string) => Promise<unknown>

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
let currentProxy: CurrentProxyFunction = CurrentProxySelection
let createPlan: PlanFunction = CreateInstallPlan
let startInstall: StartFunction = StartInstall
let getTask: TaskFunction = GetInstallTask
let cancelTask: CancelFunction = CancelInstallTask
let saveProfile = SaveAPIProfile as unknown as ProfileOperation
let listProfiles = ListAPIProfiles as unknown as ProfileOperation
let deleteProfile = DeleteAPIProfile as unknown as ProfileOperation
let probeProfile = ProbeAPIProfile as unknown as ProfileOperation
let getCompatibility = GetAPICompatibility as unknown as ProfileOperation
let createApplyPlan = CreateAPIApplyPlan as unknown as ProfileOperation
let applyAPIPlan = ApplyAPIPlan as unknown as ProfileOperation
let readHistory = ListHistory as unknown as HistoryOperation
let clearHistoryOperation = ClearHistory as unknown as HistoryOperation
let launchOperation: LaunchOperation = LaunchComponent
let createRemovalOperation: RemovalOperation = CreateRemovalPlan as unknown as RemovalOperation
let removeComponentOperation: RemovalOperation = RemoveComponent as unknown as RemovalOperation

const proxyProtocols = new Set<string>(['http', 'https-connect', 'socks5'])
const installTaskPhases = new Set<string>([
  'queued', 'downloading', 'verifying', 'installing', 'committing', 'completed', 'failed', 'canceled',
])
const removalActions = new Set<string>([
  'trash', 'package', 'recover', 'manifest', 'store', 'msi', 'uninstaller',
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

function removalAction(value: unknown): RemovalAction {
  const action = stringValue(value)
  if (!removalActions.has(action)) {
    throw new Error('移除服务返回了无效操作')
  }
  return action as RemovalAction
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

export async function getCurrentProxySelection(): Promise<ProxySelection | null> {
  const selection = await currentProxy()
  if (selection.protocol === '' && selection.port === 0) {
    return null
  }
  if (!Number.isInteger(selection.port) || selection.port < 1 || selection.port > 65535) {
    throw new Error('代理服务返回了无效的端口')
  }
  return { protocol: proxyProtocol(selection.protocol), port: selection.port }
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

function recordValue(value: unknown): Record<string, unknown> {
  if (typeof value !== 'object' || value === null || Array.isArray(value)) {
    throw new Error('API 配置服务返回了无效的数据')
  }
  return value as Record<string, unknown>
}

function booleanValue(value: unknown): boolean {
  if (typeof value !== 'boolean') throw new Error('API 配置服务返回了无效的状态')
  return value
}

function normalizeAPIProfile(raw: unknown): APIProfile {
  const value = recordValue(raw)
  return {
    id: stringValue(value.id), name: stringValue(value.name), keyHint: stringValue(value.keyHint),
    baseUrl: stringValue(value.baseUrl), model: stringValue(value.model),
    allowPrivateNetwork: booleanValue(value.allowPrivateNetwork),
    protection: stringValue(value.protection), createdAt: stringValue(value.createdAt),
    updatedAt: stringValue(value.updatedAt),
  }
}

export async function saveAPIProfile(input: APIProfileInput): Promise<APIProfile> {
  return normalizeAPIProfile(await saveProfile(input as never))
}

export async function listAPIProfiles(): Promise<APIProfile[]> {
  const value = await listProfiles()
  if (!Array.isArray(value)) throw new Error('API 配置服务返回了无效的档案列表')
  return value.map(normalizeAPIProfile)
}

export async function deleteAPIProfile(id: string): Promise<void> {
  await deleteProfile(id as never)
}

export async function probeAPIProfile(id: string): Promise<APIProbeResult> {
  const value = recordValue(await probeProfile(id as never))
  const protocols = Array.isArray(value.protocols) ? value.protocols : []
  return {
    profileId: stringValue(value.profileId), reachable: booleanValue(value.reachable),
    authenticated: booleanValue(value.authenticated),
    protocols: protocols.map((raw) => {
      const protocol = recordValue(raw)
      return {
        protocol: stringValue(protocol.protocol), status: stringValue(protocol.status),
        message: stringValue(protocol.message),
      }
    }),
    message: stringValue(value.message), checkedAt: stringValue(value.checkedAt),
  }
}

export async function getAPICompatibility(id: string): Promise<APITargetCompatibility[]> {
  const value = await getCompatibility(id as never)
  if (!Array.isArray(value)) throw new Error('API 配置服务返回了无效的兼容矩阵')
  return value.map((raw) => {
    const item = recordValue(raw)
    return { target: stringValue(item.target), compatible: booleanValue(item.compatible), reason: stringValue(item.reason) }
  })
}

export async function createAPIApplyPlan(profileId: string, targets: string[]): Promise<APIApplyPlan> {
  const value = recordValue(await createApplyPlan(profileId as never, targets as never))
  const effects = Array.isArray(value.effects) ? value.effects : []
  return {
    id: stringValue(value.id), profileId: stringValue(value.profileId),
    profileName: stringValue(value.profileName), keyHint: stringValue(value.keyHint),
    effects: effects.map((raw) => {
      const effect = recordValue(raw)
      return { target: stringValue(effect.target), path: stringValue(effect.path), description: stringValue(effect.description) }
    }),
    warning: stringValue(value.warning), createdAt: stringValue(value.createdAt), expiresAt: stringValue(value.expiresAt),
  }
}

export async function applyProfilePlan(id: string): Promise<APIApplyBatchResult> {
  const value = recordValue(await applyAPIPlan(id as never))
  const results = Array.isArray(value.results) ? value.results : []
  return {
    planId: stringValue(value.planId), profileId: stringValue(value.profileId),
    results: results.map((raw) => {
      const item = recordValue(raw)
      return {
        target: stringValue(item.target), applied: booleanValue(item.applied), path: stringValue(item.path),
        backupPath: stringValue(item.backupPath), message: stringValue(item.message),
      }
    }),
    succeeded: numberValue(value.succeeded), failed: numberValue(value.failed),
  }
}

export async function launchComponent(componentId: string, installationPath: string): Promise<void> {
	await launchOperation(componentId, installationPath)
}

export async function createRemovalPlan(componentId: string): Promise<RemovalPlan> {
  const value = recordValue(await createRemovalOperation(componentId))
  const effects = Array.isArray(value.effects) ? value.effects : []
  return {
    id: stringValue(value.id), componentId: stringValue(value.componentId), name: stringValue(value.name),
    effects: effects.map((raw) => {
      const effect = recordValue(raw)
      const action = removalAction(effect.action)
      return { action, path: stringValue(effect.path), description: stringValue(effect.description), recoverable: booleanValue(effect.recoverable) }
    }),
    warning: stringValue(value.warning), createdAt: stringValue(value.createdAt), expiresAt: stringValue(value.expiresAt),
  }
}

export async function removeComponent(planId: string): Promise<RemovalResult> {
  const value = recordValue(await removeComponentOperation(planId))
  return {
    planId: stringValue(value.planId), componentId: stringValue(value.componentId),
    removed: booleanValue(value.removed), message: stringValue(value.message),
  }
}

export async function listHistory(): Promise<HistoryEntry[]> {
	const result = await readHistory()
	if (!Array.isArray(result)) throw new Error('历史记录返回了无效结果')
	return result.map((raw) => {
		const value = recordValue(raw)
		const status = stringValue(value.status)
		if (!['completed', 'failed', 'canceled'].includes(status)) throw new Error('历史记录返回了无效状态')
		return {
			id: stringValue(value.id), operationId: stringValue(value.operationId), componentId: stringValue(value.componentId),
			name: stringValue(value.name), action: stringValue(value.action), status: status as HistoryEntry['status'],
			message: stringValue(value.message), createdAt: stringValue(value.createdAt),
		}
	})
}

export async function clearHistory(): Promise<void> {
	await clearHistoryOperation()
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
  testCurrent: CurrentProxyFunction = () => Promise.resolve({ protocol: '', port: 0 }),
): void {
  if (!testSeamEnabled()) {
    throw new Error('The proxy test seam is unavailable outside tests')
  }
  probe = testProbe
  direct = testDirect
  currentProxy = testCurrent
}

export function resetProxyOperationsForTests(): void {
  if (!testSeamEnabled()) {
    throw new Error('The proxy test seam is unavailable outside tests')
  }
  probe = ProbeProxy
  direct = UseDirectConnection
  currentProxy = CurrentProxySelection
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

export function setProfileOperationsForTests(operations: {
  save: (...args: unknown[]) => Promise<unknown>
  list: (...args: unknown[]) => Promise<unknown>
  delete: (...args: unknown[]) => Promise<unknown>
  probe: (...args: unknown[]) => Promise<unknown>
  compatibility: (...args: unknown[]) => Promise<unknown>
  createPlan: (...args: unknown[]) => Promise<unknown>
  apply: (...args: unknown[]) => Promise<unknown>
}): void {
  if (!testSeamEnabled()) throw new Error('The profile test seam is unavailable outside tests')
  saveProfile = operations.save as ProfileOperation
  listProfiles = operations.list as ProfileOperation
  deleteProfile = operations.delete as ProfileOperation
  probeProfile = operations.probe as ProfileOperation
  getCompatibility = operations.compatibility as ProfileOperation
  createApplyPlan = operations.createPlan as ProfileOperation
  applyAPIPlan = operations.apply as ProfileOperation
}

export function resetProfileOperationsForTests(): void {
  if (!testSeamEnabled()) throw new Error('The profile test seam is unavailable outside tests')
  saveProfile = SaveAPIProfile as unknown as ProfileOperation
  listProfiles = ListAPIProfiles as unknown as ProfileOperation
  deleteProfile = DeleteAPIProfile as unknown as ProfileOperation
  probeProfile = ProbeAPIProfile as unknown as ProfileOperation
  getCompatibility = GetAPICompatibility as unknown as ProfileOperation
  createApplyPlan = CreateAPIApplyPlan as unknown as ProfileOperation
  applyAPIPlan = ApplyAPIPlan as unknown as ProfileOperation
}

export function setHistoryOperationsForTests(list: HistoryOperation, clear: HistoryOperation = () => Promise.resolve()): void {
	if (!testSeamEnabled()) throw new Error('The history test seam is unavailable outside tests')
	readHistory = list
	clearHistoryOperation = clear
}

export function resetHistoryOperationsForTests(): void {
	if (!testSeamEnabled()) throw new Error('The history test seam is unavailable outside tests')
	readHistory = ListHistory as unknown as HistoryOperation
	clearHistoryOperation = ClearHistory as unknown as HistoryOperation
}

export function setLaunchOperationForTests(operation: LaunchOperation): void {
	if (!testSeamEnabled()) throw new Error('The launch test seam is unavailable outside tests')
	launchOperation = operation
}

export function resetLaunchOperationForTests(): void {
	if (!testSeamEnabled()) throw new Error('The launch test seam is unavailable outside tests')
	launchOperation = LaunchComponent
}

export function setRemovalOperationsForTests(create: RemovalOperation, remove: RemovalOperation): void {
  if (!testSeamEnabled()) throw new Error('The removal test seam is unavailable outside tests')
  createRemovalOperation = create
  removeComponentOperation = remove
}

export function resetRemovalOperationsForTests(): void {
  if (!testSeamEnabled()) throw new Error('The removal test seam is unavailable outside tests')
  createRemovalOperation = CreateRemovalPlan as unknown as RemovalOperation
  removeComponentOperation = RemoveComponent as unknown as RemovalOperation
}
