# Installs the current release as a normal user command. It detects the CPU architecture,
# configures PATH, and opens the app when done. Run from PowerShell:
# irm https://raw.githubusercontent.com/GordenArcher/ramseyer-finance/main/scripts/install.ps1 | iex
$ErrorActionPreference = 'Stop'
$repo = 'GordenArcher/ramseyer-finance'
$architecture = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
switch ($architecture) {
    'x64' { $asset = 'ramseyer-finance-windows-amd64.zip' }
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

$userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($userPath -split ';') -notcontains $installDir) {
    [Environment]::SetEnvironmentVariable('Path', (($userPath.TrimEnd(';') + ';' + $installDir).TrimStart(';')), 'User')
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
