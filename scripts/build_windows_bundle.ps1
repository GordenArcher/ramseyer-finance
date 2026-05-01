$ErrorActionPreference = "Stop"

$RootDir = Split-Path -Parent $PSScriptRoot
$AppName = if ($env:APP_NAME) { $env:APP_NAME } else { "Ramseyer Finance" }
$BinaryName = if ($env:BINARY_NAME) { $env:BINARY_NAME } else { "ramseyer-finance.exe" }
$DistDir = Join-Path $RootDir "dist"
$BundleDir = Join-Path $DistDir "$AppName-windows"
$AppDir = Join-Path $BundleDir "app"
$ZipPath = Join-Path $DistDir "$AppName-windows.zip"
$LauncherPath = Join-Path $BundleDir "START-Ramseyer-Finance.bat"
$ReadmePath = Join-Path $BundleDir "README-Windows.txt"

if (Test-Path $BundleDir) {
    Remove-Item -Recurse -Force $BundleDir
}
if (Test-Path $ZipPath) {
    Remove-Item -Force $ZipPath
}

New-Item -ItemType Directory -Force -Path $AppDir | Out-Null

go build -o (Join-Path $AppDir $BinaryName) (Join-Path $RootDir "main.go")

Copy-Item (Join-Path $RootDir "README.md") $BundleDir
Copy-Item (Join-Path $RootDir "docs") (Join-Path $BundleDir "docs") -Recurse

@"
@echo off
setlocal
cd /d "%~dp0app"
start "" "ramseyer-finance.exe"
"@ | Set-Content -Encoding ASCII $LauncherPath

@"
Ramseyer Finance Windows Package

How to start:
1. Open the extracted folder.
2. Double-click START-Ramseyer-Finance.bat

Important:
- The app stores its SQLite database under %%AppData%%\ramseyer-finance by default.
- Backups and exports are saved under the user's Downloads folder by default.
- See docs\INSTALLATION.md and docs\WINDOWS.md for more details.
"@ | Set-Content -Encoding ASCII $ReadmePath

Compress-Archive -Path $BundleDir -DestinationPath $ZipPath -Force

Write-Host "Built Windows bundle: $BundleDir"
Write-Host "Built Windows zip: $ZipPath"
