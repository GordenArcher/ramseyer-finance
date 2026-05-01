# Installation Guide

## Purpose

This guide is for the operator setting up the application on their local machine. It covers first-run behaviour, data storage locations, environment configuration, and common troubleshooting. The primary target machine is a Windows laptop.

## Prerequisites

### Windows

The application requires **Microsoft Edge WebView2 Runtime**. This is included by default on:

- Windows 11 (all versions)
- Windows 10 version 1809 and later

If the app does not start on an older Windows 10 build, download the WebView2 Runtime from Microsoft's official download page.

### macOS

No additional runtime is required. The packaged `.app` bundle includes all necessary dependencies.

## First Run

1. Start the application (see platform-specific instructions below).
2. The first launch presents a PIN setup screen.
3. Choose a PIN between 4 and 8 digits.
4. Re-enter the same PIN to confirm.
5. After the PIN is saved, the app opens into the main dashboard.

The PIN is a local lock for this installation only. It is not an online account and cannot be recovered if forgotten—keep a record of it. If the PIN is lost, the database file can still be opened directly with a SQLite tool, but there is no built-in reset mechanism.

## Starting the Application

### Packaged Windows release

1. Extract the ZIP file to any folder.
2. Open the extracted folder.
3. Double-click `START-Ramseyer-Finance.bat`.

Alternatively, run the executable directly:

```powershell
.\app\ramseyer-finance.exe
```

### Packaged macOS release

```bash
open "Ramseyer Finance.app"
```

### Development (any platform)

```bash
go run main.go
```

## Where Data Is Stored

### Database

The SQLite database is stored in the user config directory. The app logs the exact path during startup.

| Platform | Path |
|----------|------|
| Windows | `%AppData%\ramseyer-finance\ramseyer-finance.db` |
| macOS | `~/Library/Application Support/ramseyer-finance/ramseyer-finance.db` |
| Linux | `~/.config/ramseyer-finance/ramseyer-finance.db` |

### Backups and Exports

Manual downloads and native exports save to the user's Downloads directory.

| Platform | Path |
|----------|------|
| Windows | `%USERPROFILE%\Downloads` |
| macOS/Linux | `~/Downloads` |

Additional backup subdirectories are created automatically:

```
~/Downloads/Ramseyer Finance Backups/Auto/      (automatic backups)
~/Downloads/Ramseyer Finance Backups/Safety/    (pre-restore safety snapshots)
```

## Environment Overrides

Both overrides are optional. The application uses the default paths above unless one of these variables is set.

### Custom database location

**Windows PowerShell:**
```powershell
$env:RAMSEYER_FINANCE_DB_PATH = "C:\RamseyerData\ramseyer-finance.db"
.\ramseyer-finance.exe
```

**macOS / Linux:**
```bash
RAMSEYER_FINANCE_DB_PATH=/path/to/ramseyer-finance.db ./ramseyer-finance
```

### Custom backup root

All backup subdirectories (Auto, Safety) are created under this path instead of the Downloads folder.

**Windows PowerShell:**
```powershell
$env:RAMSEYER_FINANCE_BACKUP_DIR = "C:\RamseyerBackups"
.\ramseyer-finance.exe
```

**macOS / Linux:**
```bash
RAMSEYER_FINANCE_BACKUP_DIR=/path/to/backups ./ramseyer-finance
```

## Restore Behaviour

Restoring a backup replaces the current database entirely. To protect against accidental data loss:

- A **safety backup** of the current database is created automatically before every restore.
- Safety backups are stored in `Ramseyer Finance Backups/Safety/` with a timestamp in the filename.
- If the wrong backup file was selected, the safety backup can be used as a rollback point.

The restore page supports two file selection methods:

- **Packaged app:** native desktop file picker dialog
- **Browser or development:** standard file upload input

## Auto-Backup Behaviour

Auto-backup is configured from the **Setup** screen inside the application.

Available frequencies:

| Setting | Backup Interval | Retention |
|---------|----------------|-----------|
| Off | No automatic backups | — |
| Weekly | Every 7 days | Keeps last 12 |
| Monthly | Every 30 days | Keeps last 12 |
| Quarterly | Every 90 days | Keeps last 8 |

The scheduler:

- Checks for overdue backups immediately on startup
- Continues checking every 12 hours while the app stays open
- Stores the last-run timestamp in the database settings

Retention limits are applied per frequency. Switching from Weekly to Monthly does not delete existing weekly backup files.

## Troubleshooting

### "My data disappears when I rerun the server"

This should no longer happen. The database is stored in a stable user config directory, not beside the temporary `go run` executable. Rerunning the app uses the same database file.

### "The restore file picker works in a browser but not in the desktop app"

The packaged desktop app uses a native OS file picker instead of the webview's built-in file input. If the native picker button does not respond, verify that the app was launched from the correct package folder and that no antivirus software is blocking the shell integration.

### "Where did my export go?"

Exports are saved to the Downloads folder by default. The UI displays the full saved path after the export completes. Check that path if the file is not immediately visible.

### "The app does not start on Windows"

Ensure **Microsoft Edge WebView2 Runtime** is installed. On up-to-date Windows 10 and Windows 11 machines this is included by default. On older or enterprise-managed machines it may need to be installed separately.

### "I forgot my PIN"

There is no built-in PIN reset mechanism. The database file remains accessible with a SQLite tool if recovery is absolutely necessary, but this is not a supported workflow. Keep a record of the PIN in a secure place.
