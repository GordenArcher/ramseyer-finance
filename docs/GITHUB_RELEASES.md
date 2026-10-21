# GitHub Release Checklist

## Purpose

This checklist covers publishing a client-ready release through GitHub. The goal is a normal
`ramseyer-finance` command that requires no cloning, no Go installation, and no client build steps.

## Recommended Flow

1. Set `internal/appmeta/meta.go`'s `CurrentVersion` to the new tag (for example `v1.2.3`).
2. Push the latest code and tag it: `git tag v1.2.3 && git push origin v1.2.3`.
3. The **Release binaries** GitHub Actions workflow builds and publishes the release assets.
4. Confirm the workflow and the GitHub Release both completed successfully.
5. Install the release on a clean client machine and run the smoke test below.

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
- [ ] Searchable restore library selects managed backups without opening a native dialog
- [ ] Auto-backup settings save and display correctly on the Setup screen

## What to Upload

**Required Windows asset:**

- `ramseyer-finance-windows-amd64.zip`

The workflow also publishes matching macOS and Linux archives. Do not rename the assets:
the installer and `ramseyer-finance update` choose them by exact platform name.

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
4. Double-click Ramseyer Finance.exe.

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

1. Run the one-line installer from the README once.
2. Run `ramseyer-finance` whenever the app is needed; it opens the default browser.
3. Run `ramseyer-finance update` when a new release is available.

No cloning. No Go. No client build steps.
