# Installs the current release as a normal user command. It detects the CPU architecture,
# configures PATH, and opens the app when done. Run from PowerShell:
# irm "https://raw.githubusercontent.com/GordenArcher/ramseyer-finance/main/scripts/install.ps1?cb=$(Get-Random)" | iex
$ErrorActionPreference = 'Stop'
$repo = 'GordenArcher/ramseyer-finance'
# Printed so a support session can confirm which copy actually executed. The raw URL is CDN
# cached, and a stale copy was previously mistaken for an unfixed script.
$installerRevision = '2026-10-24-arch-null'
Write-Host "Ramseyer Finance installer ($installerRevision)"

# Architecture comes from the environment rather than RuntimeInformation.OSArchitecture because
# that .NET property is absent on older Windows PowerShell runtimes. When absent it evaluates to
# $null and the ToString() on it threw "cannot call a method on a null-valued expression".
# Interpolation below always produces a string, so this path cannot hit that failure.
$architecture = "$env:PROCESSOR_ARCHITECTURE".ToLowerInvariant()
# A 32-bit shell on 64-bit Windows reports x86 above and keeps the real architecture here.
# Preferring this value prevents serving the 32-bit archive to a 64-bit machine.
if (-not [string]::IsNullOrWhiteSpace($env:PROCESSOR_ARCHITEW6432)) {
    $architecture = "$env:PROCESSOR_ARCHITEW6432".ToLowerInvariant()
}
switch ($architecture) {
    'amd64' { $asset = 'ramseyer-finance-windows-amd64.zip' }
    default { throw "This Windows architecture ($architecture) is not supported by the current release." }
}
$release = Invoke-RestMethod -Headers @{ Accept = 'application/vnd.github+json' } "https://api.github.com/repos/$repo/releases/latest"
$download = $release.assets | Where-Object { $_.name -eq $asset } | Select-Object -First 1
if ($null -eq $download) { throw "The latest release does not include $asset." }

$installDir = Join-Path $env:LOCALAPPDATA 'Ramseyer Finance\bin'
$staging = Join-Path ([System.IO.Path]::GetTempPath()) ("ramseyer-finance-" + [guid]::NewGuid())
New-Item -ItemType Directory -Force -Path $installDir, $staging | Out-Null
try {
    $archive = Join-Path $staging $asset
    Invoke-WebRequest -Uri $download.browser_download_url -OutFile $archive
    Expand-Archive -Path $archive -DestinationPath $staging -Force
    Copy-Item (Join-Path $staging 'ramseyer-finance.exe') (Join-Path $installDir 'ramseyer-finance.exe') -Force
} finally {
    Remove-Item -Recurse -Force $staging -ErrorAction SilentlyContinue
}

# A user with no persisted Path returns $null here, and calling TrimEnd on $null throws.
# Normalising to an empty string keeps the append logic below safe on a fresh Windows account.
$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if ([string]::IsNullOrWhiteSpace($userPath)) {
    $userPath = ''
}

# Blank entries (from a trailing or doubled semicolon) are dropped so the joined value never
# carries empty segments into the user's Path, and the containment check does not miss a match.
$pathEntries = @(
    $userPath -split ';' |
        Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
)

if ($pathEntries -notcontains $installDir) {
    $newUserPath = (@($pathEntries) + $installDir) -join ';'
    [Environment]::SetEnvironmentVariable('Path', $newUserPath, 'User')
    $env:Path += ";$installDir"
}
$appPath = Join-Path $installDir 'ramseyer-finance.exe'
Write-Host "Installed. Enabling automatic start..."
& $appPath enable

# The shortcut uses the explicit open command, so signing in never launches a browser while a
# normal double-click always starts the service if needed and opens the app for the client.
$desktop = [Environment]::GetFolderPath('Desktop')
$shortcutPath = Join-Path $desktop 'Ramseyer Finance.lnk'
$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut($shortcutPath)
$shortcut.TargetPath = $appPath
$shortcut.Arguments = 'open'
$shortcut.WorkingDirectory = $installDir
$shortcut.IconLocation = "$appPath,0"
$shortcut.Description = 'Open Ramseyer Finance'
$shortcut.Save()

Write-Host "A Ramseyer Finance shortcut was added to your Desktop. Opening the app for the first time..."
& $appPath open
