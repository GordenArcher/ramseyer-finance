# GitHub Release Checklist

## Purpose

This checklist covers publishing a client-ready Windows release through GitHub. The goal is a single download link that requires no cloning, no Go installation, and no build steps on the client's side.

## Recommended Flow

1. Push the latest code to GitHub
2. On a Windows machine, pull or clone the latest version
3. Build the Windows release package:

   ```powershell
   .\scripts\build_windows_bundle.ps1
   ```

4. Confirm the ZIP was produced:

   ```text
   dist\Ramseyer Finance-windows.zip
   ```

5. Launch the packaged `.exe` and run through the full smoke test list below
6. Create a new GitHub Release
7. Upload `dist\Ramseyer Finance-windows.zip` as a release asset
8. Publish the release
9. Send the client the release download link along with the installation notes

## Pre-Publish Smoke Test

Run these checks on Windows using the packaged `.exe` (not `go run`):

- [ ] App starts and shows the PIN setup screen
- [ ] PIN setup flow completes (create, confirm, auto-login)
- [ ] Login works with the created PIN
- [ ] Transaction entry works (income and expenditure)
- [ ] Register loads, filters, paginates, and search works
- [ ] Transaction edit and delete work, including duplicate warning
- [ ] Export saves a file locally (test CSV and at least one other format)
- [ ] Manual backup download saves a `.db` file
- [ ] Restore file picker opens (native dialog on Windows)
- [ ] Auto-backup settings save and display correctly on the Setup screen

## What to Upload

**Required asset:**

- `dist\Ramseyer Finance-windows.zip`

**Optional supporting assets:**

- A PDF or plain-text quick-start guide (can be the installation doc exported to PDF)
- A screenshot of the extracted folder layout if the client needs extra help locating the launcher

## Suggested Release Title

```
Ramseyer Finance v1.0.0
```

## Suggested Release Notes

```
Windows desktop release of Ramseyer Finance.

How to use:
1. Download the ZIP file.
2. Extract it to a folder on your machine.
3. Open the extracted folder.
4. Double-click START-Ramseyer-Finance.bat.

On first launch:
- The app will ask you to create a PIN.
- Choose a 4 to 8 digit PIN and confirm it.
- All data is stored locally on your machine.

Backups and exports:
- Backups and exported files save to your Downloads folder by default.
- Automatic backups can be configured from the Setup screen inside the app.

Need help?
See the docs folder inside the package for installation and operations guides.
```

## Client Handoff Goal

The client should only need to:

1. Open the GitHub Release link
2. Download the Windows ZIP asset
3. Extract it to any folder
4. Double-click `START-Ramseyer-Finance.bat`

No cloning. No Go. No command line. No build steps.
