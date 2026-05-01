# Windows Deployment Notes

## Purpose

This document covers Windows-specific requirements, file locations, package contents, and client handoff guidance. It supplements the Installation and Operations guides with details relevant only to Windows deployments.

## Runtime Requirement

The application uses an embedded desktop webview via `github.com/webview/webview_go`. On Windows, the required runtime is **Microsoft Edge WebView2 Runtime**.

| Windows Version | WebView2 Status |
|----------------|-----------------|
| Windows 11 | Included by default |
| Windows 10 (version 1809+) | Included by default |
| Windows 10 (older builds) | Must be installed separately |
| Windows Server | Must be installed separately |

If the app starts normally, the runtime is already present. If the app window does not open or crashes immediately, install the WebView2 Runtime from Microsoft's official download page.

## Package Contents

The Windows release package (`Ramseyer Finance-windows.zip`) contains:

```
Ramseyer Finance-windows/
├── app/
│   └── ramseyer-finance.exe      (main application)
├── docs/
│   ├── INSTALLATION.md
│   ├── OPERATIONS.md
│   └── WINDOWS.md
├── START-Ramseyer-Finance.bat    (double-click launcher)
└── README-Windows.txt            (quick-start note)
```

## Starting the Application

**Preferred method (double-click):**

1. Extract the delivered `.zip` file to a stable location (Desktop or Documents recommended).
2. Open the extracted folder.
3. Double-click `START-Ramseyer-Finance.bat`.

**Direct method:**

1. Open the `app` folder inside the extracted package.
2. Double-click `ramseyer-finance.exe`.

Both methods produce the same result. The `.bat` launcher is provided for operators who are more comfortable with a clearly labelled entry point.

## Windows File Locations

### Database

The SQLite database is stored in the user's AppData directory, **not** inside the extracted application folder:

```text
%AppData%\ramseyer-finance\ramseyer-finance.db
```

Full expanded path example: `C:\Users\OperatorName\AppData\Roaming\ramseyer-finance\ramseyer-finance.db`

### Backups and Exports

All backups and exported files save to the user's Downloads folder:

```text
%USERPROFILE%\Downloads
```

Additional backup subdirectories are created automatically:

```text
%USERPROFILE%\Downloads\Ramseyer Finance Backups\Auto\      (automatic backups)
%USERPROFILE%\Downloads\Ramseyer Finance Backups\Safety\    (pre-restore safety snapshots)
```

## Environment Overrides (Windows PowerShell)

### Custom database path

```powershell
$env:RAMSEYER_FINANCE_DB_PATH = "C:\RamseyerData\ramseyer-finance.db"
.\ramseyer-finance.exe
```

### Custom backup root

```powershell
$env:RAMSEYER_FINANCE_BACKUP_DIR = "C:\RamseyerBackups"
.\ramseyer-finance.exe
```

## Building the Windows Release

On a Windows machine with Go installed:

```powershell
.\scripts\build_windows_bundle.ps1
```

Expected output:

- `dist\Ramseyer Finance-windows\` — unpackaged folder
- `dist\Ramseyer Finance-windows.zip` — deliverable ZIP

The build script handles compilation, folder structure, documentation copying, launcher creation, and zipping in one step.

## Recommended Client Handoff

In order of preference:

1. **GitHub Release link** — send the client the release download URL and let them download the ZIP directly. This is the cleanest method and avoids email attachment size limits.
2. **Direct ZIP delivery** — provide the ZIP file on a USB drive or via file sharing if the client cannot access GitHub.
3. **Pre-extracted folder** — hand over the extracted folder if the client is not comfortable with ZIP files.

In all cases, instruct the client to place the extracted folder in a stable location such as their **Desktop** or **Documents** folder. Moving or deleting the folder later does not affect the database, which lives in AppData.

## What the Client Should Not Need to Do

The client should never need to:

- Clone the repository
- Install Go or any other development tools
- Run terminal or PowerShell build commands
- Package the application manually
- Edit configuration files

The packaged `.exe` and `.bat` launcher are self-contained. No setup wizard, installer, or administrator permissions are required.

## Important Clarifications

### The application folder is not the database location

Deleting, moving, or renaming the extracted application folder does **not** delete the SQLite database. The database lives under `%AppData%\ramseyer-finance\` and persists independently of the application folder.

This means:

- Upgrading to a new version is as simple as replacing the application folder. The existing database is picked up automatically.
- Uninstalling the application requires manually deleting both the application folder and the `%AppData%\ramseyer-finance\` directory if complete data removal is desired.

### The database survives application restarts

The app always uses the same database path. Restarting the application, rebooting the machine, or extracting a new version of the package does not reset or overwrite the data.

### Keeping the application folder stable

The client should keep the extracted folder in a consistent location. If the folder is moved after the first run, the app continues to work—only the executable location changes, not the data.
