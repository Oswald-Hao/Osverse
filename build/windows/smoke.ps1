[CmdletBinding()]
param([Parameter(Mandatory = $true)][string]$Installer)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

$installerPath = (Resolve-Path -LiteralPath $Installer).Path
$installRoot = Join-Path $env:LOCALAPPDATA 'Programs\Osverse'
$application = Join-Path $installRoot 'osverse.exe'
$uninstaller = Join-Path $installRoot 'uninstall.exe'

$installProcess = Start-Process -FilePath $installerPath -ArgumentList '/S' -Wait -PassThru
if ($installProcess.ExitCode -ne 0 -or -not (Test-Path -LiteralPath $application -PathType Leaf) -or -not (Test-Path -LiteralPath $uninstaller -PathType Leaf)) {
    throw "Silent installer smoke failed with exit code $($installProcess.ExitCode)."
}

$applicationProcess = Start-Process -FilePath $application -PassThru
try {
    Start-Sleep -Seconds 8
    if ($applicationProcess.HasExited) {
        throw "Installed Osverse exited during launch smoke with code $($applicationProcess.ExitCode)."
    }
}
finally {
    if (-not $applicationProcess.HasExited) {
        Stop-Process -Id $applicationProcess.Id -Force
        $applicationProcess.WaitForExit()
    }
}

$uninstallProcess = Start-Process -FilePath $uninstaller -ArgumentList '/S' -Wait -PassThru
if ($uninstallProcess.ExitCode -ne 0 -or (Test-Path -LiteralPath $application)) {
    throw "Silent uninstaller smoke failed with exit code $($uninstallProcess.ExitCode)."
}
