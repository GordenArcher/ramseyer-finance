# Installs the current release as a normal user command. It detects the CPU architecture,
# configures PATH, and opens the app when done. Run from PowerShell:
# irm "https://raw.githubusercontent.com/GordenArcher/ramseyer-finance/main/scripts/install.ps1?cb=$(Get-Random)" | iex
$ErrorActionPreference = 'Stop'
$repo = 'GordenArcher/ramseyer-finance'
# Printed so a support session can confirm which copy actually executed. The raw URL is CDN
# cached, and a stale copy was previously mistaken for an unfixed script.
$installerRevision = '2026-10-25-native-stderr'
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

# A running executable is locked on Windows, so replacing it fails with a sharing violation.
# End the logon task first, then kill whatever still owns the port by PID. The installed
# binary's own stop command is deliberately not used: the version being replaced may be the
# one with the broken stop path this installer exists to repair.
Write-Host "Stopping any running Ramseyer Finance instance..."
# cmd owns the redirection on purpose. In Windows PowerShell 5.1, redirecting a native
# command's stderr with 2>$null turns each stderr line into an error record, and the Stop
# preference at the top of this script then aborts the installer. A missing task is the normal
# case on a fresh or broken account, so this call must not be able to fail the run.
& cmd /c "schtasks /End /TN ramseyer-finance >nul 2>&1"
# Get-NetTCPConnection is absent on old Windows builds, so the image-name kill below is the
# fallback rather than letting a missing cmdlet abort the install.
if ($null -ne (Get-Command Get-NetTCPConnection -ErrorAction SilentlyContinue)) {
    Get-NetTCPConnection -LocalPort 47823 -State Listen -ErrorAction SilentlyContinue |
        Select-Object -ExpandProperty OwningProcess -Unique |
        ForEach-Object { Stop-Process -Id $_ -Force -ErrorAction SilentlyContinue }
}
& cmd /c "taskkill /IM ramseyer-finance.exe /T /F >nul 2>&1"
Start-Sleep -Milliseconds 500

try {
    $archive = Join-Path $staging $asset
    Invoke-WebRequest -Uri $download.browser_download_url -OutFile $archive
    # GitHub publishes a sha256 digest per asset. Verifying it stops a truncated or swapped
    # archive from being expanded and installed. Older releases have no digest, and those are
    # accepted as before to avoid breaking an upgrade path that cannot be verified.
    if (-not [string]::IsNullOrWhiteSpace("$($download.digest)")) {
        $digestParts = "$($download.digest)".Split(':', 2)
        if ($digestParts.Count -eq 2 -and $digestParts[0] -eq 'sha256') {
            $actualDigest = (Get-FileHash -Algorithm SHA256 -Path $archive).Hash.ToLowerInvariant()
            if ($actualDigest -ne $digestParts[1].ToLowerInvariant()) {
                throw "The downloaded archive failed checksum verification. Check your connection and run the installer again."
            }
        }
    }
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
# The packaged binary is GUI subsystem, so PowerShell does not wait for a GUI process.
# Start-Process -Wait keeps task registration from racing the health check and browser launch
# that follow, and the new file logging makes any failure diagnosable.
Start-Process -FilePath $appPath -ArgumentList 'enable' -Wait

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

Write-Host "A Ramseyer Finance shortcut was added to your Desktop. Starting the app..."
Start-Process -FilePath $appPath -ArgumentList 'open' -Wait

# A GUI-subsystem start can return before the HTTP listener is ready. Poll the health endpoint
# directly because the process exit code is not observable in this host and the terminal must not
# report success while http://127.0.0.1:47823 is unreachable.
$healthy = $false
$healthUri = 'http://127.0.0.1:47823/healthz'
for ($attempt = 0; $attempt -lt 40; $attempt++) {
    try {
        $response = Invoke-WebRequest -UseBasicParsing -Uri $healthUri -TimeoutSec 2 -ErrorAction Stop
        if ($null -ne $response -and $response.StatusCode -eq 200) {
            $healthy = $true
            break
        }
    } catch {
        # Ignore connection errors while the server is still starting.
    }
    Start-Sleep -Milliseconds 500
}

if (-not $healthy) {
    $logPath = $null
    if (-not [string]::IsNullOrWhiteSpace($env:APPDATA)) {
        $logPath = Join-Path $env:APPDATA 'ramseyer-financial-manager\logs\server.log'
    }
    if (-not [string]::IsNullOrWhiteSpace($logPath)) {
        throw "Ramseyer Finance did not respond on http://127.0.0.1:47823. Start it with `"$appPath`" serve and check the log at $logPath. If the log shows a scheduled task error the app should still start via the new direct-launch fallback."
    } else {
        throw "Ramseyer Finance did not respond on http://127.0.0.1:47823. Start it with `"$appPath`" serve and check logs in your user config directory (ramseyer-financial-manager/logs/server.log). If the log shows a scheduled task error the app should still start via the new direct-launch fallback."
    }
}

Write-Host "Ramseyer Finance is running at http://127.0.0.1:47823"

