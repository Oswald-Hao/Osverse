[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$Version,
    [Parameter(Mandatory = $true)][string]$Binary,
    [Parameter(Mandatory = $true)][string]$Installer,
    [Parameter(Mandatory = $true)][string]$OutputDirectory
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest

if ($Version -notmatch '^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?$') {
    throw 'Version must be semantic.'
}

$binaryPath = (Resolve-Path -LiteralPath $Binary).Path
$installerPath = (Resolve-Path -LiteralPath $Installer).Path
if ([IO.Path]::GetExtension($binaryPath) -ne '.exe' -or [IO.Path]::GetExtension($installerPath) -ne '.exe') {
    throw 'Windows package inputs must be executable files.'
}

$outputPath = [IO.Path]::GetFullPath($OutputDirectory)
[IO.Directory]::CreateDirectory($outputPath) | Out-Null
$portableBinary = Join-Path $outputPath "osverse-$Version-windows-amd64.exe"
$setup = Join-Path $outputPath "osverse-$Version-windows-amd64-setup.exe"
$zip = Join-Path $outputPath "osverse-$Version-windows-amd64-portable.zip"
Copy-Item -LiteralPath $binaryPath -Destination $portableBinary
Copy-Item -LiteralPath $installerPath -Destination $setup

$portableRoot = Join-Path ([IO.Path]::GetTempPath()) ("osverse-portable-" + [Guid]::NewGuid().ToString('N'))
$portableDirectory = Join-Path $portableRoot 'Osverse'
[IO.Directory]::CreateDirectory($portableDirectory) | Out-Null
Copy-Item -LiteralPath $binaryPath -Destination (Join-Path $portableDirectory 'osverse.exe')
$portableReadme = @"
Osverse $Version portable for Windows x64

Run osverse.exe as the current user. Microsoft Edge WebView2 Runtime is required.
API profiles are stored for the current Windows user and protected with DPAPI.
"@
[IO.File]::WriteAllText((Join-Path $portableDirectory 'README.txt'), $portableReadme, [Text.UTF8Encoding]::new($false))
Compress-Archive -LiteralPath $portableDirectory -DestinationPath $zip -CompressionLevel Optimal

$subjects = Get-ChildItem -LiteralPath $outputPath -File |
    Where-Object { $_.Extension -eq '.exe' -or $_.Extension -eq '.zip' } |
    Sort-Object -Property Name
$checksums = foreach ($subject in $subjects) {
    $digest = (Get-FileHash -LiteralPath $subject.FullName -Algorithm SHA256).Hash.ToLowerInvariant()
    "$digest  $($subject.Name)"
}
if ($checksums.Count -ne 3) {
    throw "Expected three Windows release artifacts, found $($checksums.Count)."
}
[IO.File]::WriteAllLines((Join-Path $outputPath 'SHA256SUMS-windows-amd64'), $checksums, [Text.UTF8Encoding]::new($false))
