# Distribution Guide

## Purpose

This guide describes how to build distributable desktop packages. Windows is the primary client target; macOS is a secondary target used during development.

## Primary Release Target: Windows

Output:

- `dist\Ramseyer Finance-windows\` — unpackaged folder ready for use
- `dist\Ramseyer Finance-windows.zip` — zipped bundle for GitHub Release delivery

## Build Commands

### Basic Windows binary (for local testing only)

```powershell
go build -ldflags "-H=windowsgui" -o build\ramseyer-finance.exe main.go
```

### Packaged Windows bundle (for client delivery)

```powershell
.\scripts\build_windows_bundle.ps1
```

### macOS bundle (secondary, for development)

```bash
make macos-app
```

## What the Windows Packager Does

The `build_windows_bundle.ps1` script performs these steps in order:

1. Builds the GUI-subsystem `ramseyer-finance.exe` from source
2. Builds the GUI-subsystem `updater.exe` from `cmd/updater`
3. Creates `dist\Ramseyer Finance-windows\app\`
4. Copies both application executables into the package folder
5. Copies the `docs/` folder into the package
6. Builds a `Ramseyer Finance.exe` GUI launcher for terminal-free double-click startup
7. Writes a short `README-Windows.txt` handoff note with first-run instructions
8. Zips the entire package folder for delivery

## Windows Runtime Requirement

The client machine must have **Microsoft Edge WebView2 Runtime** installed. This is the embedded browser engine the desktop shell depends on. On Windows 10 (version 1809+) and Windows 11, the runtime is included by default. For older Windows 10 builds, it can be downloaded from Microsoft's WebView2 page.

## What to Hand Over for Windows

The minimum deliverable is `Ramseyer Finance-windows.zip`. The extracted folder can also be handed over directly if the client prefers a ready-to-open copy.

Optionally include the documentation files separately:

- `docs/INSTALLATION.md`
- `docs/OPERATIONS.md`
- `docs/WINDOWS.md`

## Recommended Delivery Method

The cleanest handoff avoids asking the client to install Go, clone the repository, or run build commands:

1. Push the source code to GitHub
2. Build the Windows package on a Windows machine
3. Create a GitHub Release
4. Upload `dist\Ramseyer Finance-windows.zip` as the release asset
5. Send the client the GitHub Release download link along with the installation notes

## Optional Code Signing

The Windows bundler does not currently apply code signing. If Windows SmartScreen or organisational policy requires signed binaries, add a post-build signing step on the Windows release machine using `signtool.exe`.

The macOS bundle supports optional signing through an environment variable:

```bash
SIGN_IDENTITY="Developer ID Application: Example Org" make macos-app
```

## Release Checklist

Before publishing a GitHub Release, verify the following on a Windows machine using the packaged `.exe` (not `go run`):

1. [ ] Run tests: `go test ./...`
2. [ ] Build the Windows package: `.\scripts\build_windows_bundle.ps1`
3. [ ] Double-click the root `Ramseyer Finance.exe` launcher and confirm no terminal appears
4. [ ] Confirm first-run PIN setup flow completes
5. [ ] Confirm login with the created PIN works
6. [ ] Confirm transaction entry (income and expenditure) saves correctly
7. [ ] Confirm register edit and delete work, including the duplicate warning
8. [ ] Confirm export saves a file locally (CSV or PDF)
9. [ ] Confirm manual backup download saves a file
10. [ ] Confirm the searchable restore library filters and selects a backup without opening a native picker
11. [ ] Confirm auto-backup settings save and the scheduler logs activity
12. [ ] Confirm `Check for Updates` reaches the latest GitHub Release
13. [ ] Update `internal/appmeta/meta.go` to the new version before tagging
14. [ ] Create the GitHub Release
15. [ ] Upload `dist\Ramseyer Finance-windows.zip` as the release asset
16. [ ] Send the client the release download link and installation notes

## Handoff Contents

Recommended files to include in the release:

- `Ramseyer Finance-windows.zip` (the packaged application)
- `docs/INSTALLATION.md` (first-run setup instructions)
- `docs/OPERATIONS.md` (day-to-day usage guide)
- `docs/WINDOWS.md` (Windows-specific notes)

## Secondary Platform Notes

### macOS

The repository includes a working macOS packaging helper because active development happens on macOS. Output:

- `dist/Ramseyer Finance.app`
- `dist/Ramseyer Finance-macos.zip`

### Linux

Linux packaging is not yet formalised in this repository. The application runs correctly on Linux via `go run main.go`, but no `.desktop` file, AppImage, or distribution package is currently generated.
