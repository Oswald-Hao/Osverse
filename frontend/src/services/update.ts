import { CheckForAppUpdate, StartAppUpdate } from '../../wailsjs/go/main/App'

import type { AppUpdateInfo, AppUpdateResult } from '../domain'

function text(value: unknown): string {
  return typeof value === 'string' ? value : String(value ?? '')
}

function finiteNumber(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0
}

export async function checkForAppUpdate(): Promise<AppUpdateInfo> {
  const raw = await CheckForAppUpdate() as unknown as Record<string, unknown>
  return {
    available: raw.available === true,
    canInstall: raw.canInstall === true,
    planId: text(raw.planId),
    currentVersion: text(raw.currentVersion),
    latestVersion: text(raw.latestVersion),
    releaseName: text(raw.releaseName),
    releaseNotes: text(raw.releaseNotes),
    publishedAt: text(raw.publishedAt),
    downloadBytes: finiteNumber(raw.downloadBytes),
    platform: text(raw.platform),
    format: text(raw.format),
    message: text(raw.message),
  }
}

export async function startAppUpdate(planId: string): Promise<AppUpdateResult> {
  if (!planId) throw new Error('更新计划无效，请重新检查')
  const raw = await StartAppUpdate(planId) as unknown as Record<string, unknown>
  return { started: raw.started === true, message: text(raw.message) }
}
